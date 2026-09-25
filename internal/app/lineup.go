package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
)

var (
	ErrUnknownClub       = errors.New("app: unknown club")
	ErrNoUserClub        = errors.New("app: the career has no user club")
	ErrNotUserFixture    = errors.New("app: not a fixture of the user club")
	ErrFixtureNotPending = errors.New("app: fixture is not awaiting results")
	ErrInvalidLineup     = errors.New("app: invalid lineup")
)

// SelectedBy says who chose a side's lineup for a match. Values are durable.
type SelectedBy uint8

const (
	SelectedByAI      SelectedBy = 1 // the AI default: no lineup was submitted
	SelectedByManager SelectedBy = 2 // the lineup submitted for the fixture
)

func (s SelectedBy) Valid() bool { return s == SelectedByAI || s == SelectedByManager }

func (s SelectedBy) String() string {
	switch s {
	case SelectedByAI:
		return "AI"
	case SelectedByManager:
		return "manager"
	}
	return fmt.Sprintf("SelectedBy(%d)", uint8(s))
}

// SubmitLineup asks for the user club's lineup in a fixture of the pending
// batch. Resubmitting replaces the earlier lineup. A fixture with no
// submitted lineup is played with the AI selection.
type SubmitLineup struct {
	ID               CommandID
	ExpectedRevision Revision
	Fixture          ids.FixtureID
	Lineup           selection.Lineup
}

// LineupSubmitted is the recorded result of a SubmitLineup command.
type LineupSubmitted struct {
	Command  CommandID
	Revision Revision // world revision after the change
	Fixture  ids.FixtureID
	Team     ids.TeamID
}

// LineupRecord is a successful SubmitLineup and its result.
type LineupRecord struct {
	Request SubmitLineup
	Result  LineupSubmitted
}

func (r LineupRecord) clone() LineupRecord {
	r.Request.Lineup = r.Request.Lineup.Clone()
	return r
}

func mustEmptySelections() *selection.Store {
	s, err := selection.New(nil)
	if err != nil {
		panic(err) // an empty store is always valid
	}
	return s
}

// UserClub returns the club the human manages, if any.
func (w *World) UserClub() (ids.ClubID, bool) { return w.userClub, w.userClub != 0 }

// userTeam returns the user club's senior team, which plays its fixtures.
func (w *World) userTeam() (ids.TeamID, bool) {
	if w.userClub == 0 {
		return 0, false
	}
	return w.registry.SeniorTeam(w.userClub)
}

// userFixtures returns the pending batch's fixtures that the user club
// plays, in ascending ID order.
func (w *World) userFixtures(rounds []competitions.RoundInfo) []ids.FixtureID {
	team, ok := w.userTeam()
	if !ok {
		return nil
	}
	var out []ids.FixtureID
	for _, r := range rounds {
		for _, id := range r.Fixtures {
			if f, ok := w.competitions.Fixture(id); ok && (f.Home == team || f.Away == team) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// pendingUserFixture checks that the user club plays fixture in the pending
// batch and returns its team and the competition's match rules.
func (w *World) pendingUserFixture(fixture ids.FixtureID) (ids.TeamID, matches.Rules, error) {
	team, ok := w.userTeam()
	if !ok {
		return 0, matches.Rules{}, ErrNoUserClub
	}
	f, ok := w.competitions.Fixture(fixture)
	if !ok || (f.Home != team && f.Away != team) {
		return 0, matches.Rules{}, fmt.Errorf("%w: fixture %d, team %d", ErrNotUserFixture, fixture, team)
	}
	info, _ := w.competitions.Round(competitions.RoundRef{Season: f.Season, Round: f.Round})
	if _, done := w.competitions.Result(fixture); done || info.Status != competitions.RoundAwaitingResults {
		return 0, matches.Rules{}, fmt.Errorf("%w: fixture %d (%s)", ErrFixtureNotPending, fixture, info.Status)
	}
	rules, err := w.matchRules(f.Season.Competition)
	return team, rules, err
}

// SuggestLineup returns the AI's selection for the user club in a pending
// fixture: the lineup it plays if none is submitted. Clients can edit it and
// submit the result. Read-only.
func (w *World) SuggestLineup(fixture ids.FixtureID) (selection.Lineup, error) {
	team, rules, err := w.pendingUserFixture(fixture)
	if err != nil {
		return selection.Lineup{}, err
	}
	in, err := w.selectTeam(team, rules)
	if err != nil {
		return selection.Lineup{}, err
	}
	l := selection.Lineup{Tactics: in.Tactics}
	for _, p := range in.Starters {
		l.Starters = append(l.Starters, selection.Slot{Player: p.Player, Role: p.Role})
	}
	for _, p := range in.Bench {
		l.Bench = append(l.Bench, p.Player)
	}
	return l, nil
}

// SubmittedLineup returns the lineup the user club submitted for fixture, if
// any. Read-only.
func (w *World) SubmittedLineup(fixture ids.FixtureID) (selection.Lineup, bool) {
	team, ok := w.userTeam()
	if !ok {
		return selection.Lineup{}, false
	}
	return w.selections.Lineup(fixture, team)
}

// SubmitLineup records the user club's lineup for a fixture of the pending
// batch; ResolveRounds plays it instead of the AI selection.
//
//   - Retries follow ResolveRounds: a recorded ID with the same content
//     returns the recorded result, different content is ErrCommandIDReused,
//     and failed attempts are not recorded.
//   - ExpectedRevision must equal Revision.
//   - The fixture must be the user club's and await results; the lineup
//     must have a valid shape (selection.Lineup.Validate), a bench within the
//     competition's MaxBench, and only players of the user club's squad.
//
// On success the lineup replaces any earlier one for the fixture and the
// revision increments. On error nothing changes.
func (w *World) SubmitLineup(cmd SubmitLineup) (LineupSubmitted, error) {
	if !validCommandID(cmd.ID) {
		return LineupSubmitted{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		if rec.lineup == nil || !sameSubmission(rec.lineup.Request, cmd) {
			return LineupSubmitted{}, fmt.Errorf("%w: command %d", ErrCommandIDReused, cmd.ID)
		}
		return rec.lineup.Result, nil
	}
	if cmd.ExpectedRevision != w.revision {
		return LineupSubmitted{}, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, cmd.ExpectedRevision, w.revision)
	}
	team, rules, err := w.pendingUserFixture(cmd.Fixture)
	if err != nil {
		return LineupSubmitted{}, err
	}
	if w.live != nil && w.live.fixture == cmd.Fixture {
		return LineupSubmitted{}, fmt.Errorf("%w: fixture %d has kicked off live; use MatchDecision", ErrMatchInProgress, cmd.Fixture)
	}
	if _, err := w.lineupInput(team, cmd.Lineup, rules); err != nil {
		return LineupSubmitted{}, err
	}
	if err := w.selections.Submit(selection.Entry{Fixture: cmd.Fixture, Team: team, Lineup: cmd.Lineup}); err != nil {
		return LineupSubmitted{}, fmt.Errorf("%w: %v", ErrInvalidLineup, err)
	}

	// Committed. Nothing below can fail.
	w.revision++
	res := LineupSubmitted{Command: cmd.ID, Revision: w.revision, Fixture: cmd.Fixture, Team: team}
	rec := LineupRecord{Request: cmd, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{lineup: &rec}
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindLineupSubmitted, LineupSubmitted: &events.LineupSubmitted{Fixture: cmd.Fixture, Team: team}})
	w.publish()
	return res, nil
}

func sameSubmission(a, b SubmitLineup) bool {
	return a.ID == b.ID && a.ExpectedRevision == b.ExpectedRevision && a.Fixture == b.Fixture && a.Lineup.Equal(b.Lineup)
}

// lineupInput validates a lineup against the team's current squad and the
// competition's rules, and builds its detached match input with current
// condition. Starters play their slot's role; bench players their natural
// role.
func (w *World) lineupInput(team ids.TeamID, l selection.Lineup, rules matches.Rules) (matches.TeamInput, error) {
	fail := func(format string, args ...any) (matches.TeamInput, error) {
		return matches.TeamInput{}, fmt.Errorf("%w for team %d: "+format, append([]any{ErrInvalidLineup, team}, args...)...)
	}
	if err := l.Validate(); err != nil {
		return fail("%v", err)
	}
	if len(l.Bench) > int(rules.MaxBench) {
		return fail("bench of %d exceeds %d", len(l.Bench), rules.MaxBench)
	}
	player := func(id ids.PlayerID) (ai.Candidate, error) {
		if a, ok := w.employment.Assignment(id); !ok || a.Team != team {
			return ai.Candidate{}, fmt.Errorf("player %d is not in the squad", id)
		}
		return w.candidate(id)
	}
	in := matches.TeamInput{Team: team, Tactics: l.Tactics}
	for _, s := range l.Starters {
		c, err := player(s.Player)
		if err != nil {
			return fail("%v", err)
		}
		in.Starters = append(in.Starters, matches.PlayerInput{Player: c.Player, Role: s.Role, Ratings: c.Ratings, Condition: c.Condition})
	}
	for _, id := range l.Bench {
		c, err := player(id)
		if err != nil {
			return fail("%v", err)
		}
		in.Bench = append(in.Bench, matches.PlayerInput{Player: c.Player, Role: c.Natural, Ratings: c.Ratings, Condition: c.Condition})
	}
	return in, nil
}

// validateSelections checks submitted lineups against the other modules:
// the user club exists; each lineup is the user team's, for a fixture it
// plays that has kicked off (lineups are only accepted for the pending
// batch), with a bench within the league's rules; and a lineup still
// awaiting its match selects only current squad players.
func (w *World) validateSelections() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }
	if w.userClub != 0 {
		if _, ok := w.registry.Club(w.userClub); !ok {
			fail("user club %d is not registered", w.userClub)
		}
	}
	team, hasUser := w.userTeam()
	for _, e := range w.selections.Entries() {
		f, ok := w.competitions.Fixture(e.Fixture)
		switch {
		case !hasUser || e.Team != team:
			fail("lineup for fixture %d belongs to team %d, not the user team", e.Fixture, e.Team)
			continue
		case !ok || (f.Home != e.Team && f.Away != e.Team):
			fail("lineup for fixture %d, which team %d does not play", e.Fixture, e.Team)
			continue
		}
		info, _ := w.competitions.Round(competitions.RoundRef{Season: f.Season, Round: f.Round})
		if info.Status == competitions.RoundScheduled {
			fail("lineup for fixture %d, which has not kicked off", e.Fixture)
		}
		rules, err := w.matchRules(f.Season.Competition)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(e.Lineup.Bench) > int(rules.MaxBench) {
			fail("lineup for fixture %d has a bench of %d, max %d", e.Fixture, len(e.Lineup.Bench), rules.MaxBench)
		}
		if _, done := w.competitions.Result(e.Fixture); !done {
			if _, err := w.lineupInput(e.Team, e.Lineup, rules); err != nil {
				fail("fixture %d: %v", e.Fixture, err)
			}
		}
	}
	return errs
}
