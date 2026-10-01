package app

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
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
	SelectedByManager SelectedBy = 2 // the manager's lineup: submitted for the fixture, from the team plan or carried over
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

// LineupSource says where the lineup of a pending fixture comes from.
// Values are durable.
type LineupSource uint8

const (
	LineupFromSubmission LineupSource = 1 // submitted for this fixture
	LineupCarriedOver    LineupSource = 2 // the lineup the user club last played, carried over
	LineupSuggested      LineupSource = 3 // the AI selection: the manager has never picked one
	LineupFromPlan       LineupSource = 4 // the manager's saved team plan
)

func (s LineupSource) Valid() bool { return s >= LineupFromSubmission && s <= LineupFromPlan }

func (s LineupSource) String() string {
	switch s {
	case LineupFromSubmission:
		return "submitted"
	case LineupCarriedOver:
		return "carried over"
	case LineupSuggested:
		return "suggested"
	case LineupFromPlan:
		return "team plan"
	}
	return fmt.Sprintf("LineupSource(%d)", uint8(s))
}

// MatchdayLineup is the lineup the user club plays in a pending fixture
// unless the manager submits another.
type MatchdayLineup struct {
	Fixture ids.FixtureID
	Lineup  selection.Lineup
	Source  LineupSource
	// Carried over only: the fixture the lineup was last played in.
	From ids.FixtureID
	// Carried over or from the plan: the players of the lineup it started
	// from who are left out of this one, in lineup order. A player is left
	// out when he is no longer in the squad or is injured (his starting
	// place is refilled by the AI) or the bench is longer than this
	// competition allows.
	Dropped []ids.PlayerID
}

func (m MatchdayLineup) clone() MatchdayLineup {
	m.Lineup = m.Lineup.Clone()
	m.Dropped = slices.Clone(m.Dropped)
	return m
}

// SubmitLineup asks for the user club's lineup in a fixture of the pending
// batch. Resubmitting replaces the earlier lineup. A fixture with no
// submitted lineup is played with the team plan, else the lineup the user
// club last played, carried over (see MatchdayLineup), or else the AI
// selection.
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
	s, err := selection.New(selection.Snapshot{})
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
	return w.selectTeam(team, rules)
}

// ProbableLineup returns the lineup the AI would field for a senior team of
// a current league if it played today: the selection ResolveRounds makes for
// a club nobody manages, from the squad's current condition and injuries,
// under the league's match rules. It is not a promise: the squad changes by
// kickoff. The user club's team is rejected, since its lineup comes from the
// manager (see MatchdayLineup and TeamPlan). Read-only.
func (w *World) ProbableLineup(team ids.TeamID) (selection.Lineup, error) {
	if mine, ok := w.userTeam(); ok && mine == team {
		return selection.Lineup{}, fmt.Errorf("%w: team %d is the user club's; see TeamPlan", ErrInvalidCommand, team)
	}
	rules, err := w.leagueRules(team)
	if err != nil {
		return selection.Lineup{}, err
	}
	return w.selectTeam(team, rules)
}

// lineupOf returns the lineup a team input fields.
func lineupOf(in matches.TeamInput) selection.Lineup {
	l := selection.Lineup{Tactics: in.Tactics}
	for _, p := range in.Starters {
		l.Starters = append(l.Starters, selection.Slot{Player: p.Player, Role: p.Role})
	}
	for _, p := range in.Bench {
		l.Bench = append(l.Bench, p.Player)
	}
	return l
}

// MatchdayLineup returns the lineup the user club plays in a pending
// fixture if the manager submits nothing more, and where it comes from:
//
//   - the lineup submitted for the fixture;
//   - else the saved team plan (see TeamPlan);
//   - else the lineup the user club played in its latest earlier match
//     that had one (by kickoff, then fixture ID), carried over with the
//     same tactics;
//   - else, or when the plan or carried lineup cannot be refilled, the
//     AI's selection (SuggestLineup).
//
// A plan or carried lineup is fitted to the match: players who left the
// squad or are injured are dropped and their starting places refilled by
// ai.RefillLineup, and the bench is cut to the competition's limit.
//
// ResolveRounds and PlayMatch field exactly this lineup. Read-only.
func (w *World) MatchdayLineup(fixture ids.FixtureID) (MatchdayLineup, error) {
	team, rules, err := w.pendingUserFixture(fixture)
	if err != nil {
		return MatchdayLineup{}, err
	}
	f, _ := w.competitions.Fixture(fixture)
	m, ok, err := w.managerLineup(f, team, rules)
	if err != nil || ok {
		return m, err
	}
	l, err := w.SuggestLineup(fixture)
	if err != nil {
		return MatchdayLineup{}, err
	}
	return MatchdayLineup{Fixture: fixture, Lineup: l, Source: LineupSuggested}, nil
}

// managerLineup returns team's lineup for fixture f if the manager has one:
// submitted for f, else the saved plan, else carried over from the latest
// earlier fixture with a stored lineup. ok is false when the AI selects
// instead. Read-only.
func (w *World) managerLineup(f competitions.Fixture, team ids.TeamID, rules matches.Rules) (MatchdayLineup, bool, error) {
	if l, ok := w.selections.Lineup(f.ID, team); ok {
		return MatchdayLineup{Fixture: f.ID, Lineup: l, Source: LineupFromSubmission}, true, nil
	}
	m := MatchdayLineup{Fixture: f.ID, Source: LineupFromPlan}
	base, ok := w.selections.Plan(team)
	if !ok {
		last, ok := w.latestLineup(team, f.Kickoff, f.ID)
		if !ok {
			return MatchdayLineup{}, false, nil
		}
		base, m.Source, m.From = last.Lineup, LineupCarriedOver, last.Fixture
	}
	l, dropped, ok, err := w.fitLineup(team, base, rules)
	if err != nil || !ok {
		return MatchdayLineup{}, false, err
	}
	m.Lineup, m.Dropped = l, dropped
	return m, true, nil
}

// latestLineup returns team's stored lineup for its latest fixture kicking
// off no later than before (by kickoff, then fixture ID), other than
// exclude. Read-only.
func (w *World) latestLineup(team ids.TeamID, before sim.GameInstant, exclude ids.FixtureID) (selection.Entry, bool) {
	var last selection.Entry
	var lastKickoff sim.GameInstant
	for _, e := range w.selections.Entries() {
		prev, ok := w.competitions.Fixture(e.Fixture)
		if e.Team != team || !ok || prev.Kickoff > before || e.Fixture == exclude {
			continue
		}
		if last.Fixture == 0 || prev.Kickoff > lastKickoff || (prev.Kickoff == lastKickoff && e.Fixture > last.Fixture) {
			last, lastKickoff = e, prev.Kickoff
		}
	}
	return last, last.Fixture != 0
}

// fitLineup fits a saved lineup to a match of team under rules: players
// not in its available squad are dropped, their starting places refilled by
// ai.RefillLineup, and the bench is cut to the rules' limit. It returns the
// fitted lineup, with the same tactics, and the dropped players in the
// saved lineup's order. ok is false when nobody can fill a vacancy.
// Read-only.
func (w *World) fitLineup(team ids.TeamID, saved selection.Lineup, rules matches.Rules) (selection.Lineup, []ids.PlayerID, bool, error) {
	available := w.availableSquad(team)
	inSquad := func(p ids.PlayerID) bool { return slices.Contains(available, p) }
	var slots []ai.Slot
	for _, s := range saved.Starters {
		if !inSquad(s.Player) {
			s.Player = 0
		}
		slots = append(slots, ai.Slot{Player: s.Player, Role: s.Role})
	}
	var bench []ids.PlayerID
	for _, p := range saved.Bench {
		if inSquad(p) {
			bench = append(bench, p)
		}
	}
	candidates, err := w.knownCandidates(team, available)
	if err != nil {
		return selection.Lineup{}, nil, false, err
	}
	slots, bench, err = ai.RefillLineup(team, slots, bench, candidates, rules)
	if errors.Is(err, ai.ErrNoLegalLineup) {
		return selection.Lineup{}, nil, false, nil
	}
	if err != nil {
		return selection.Lineup{}, nil, false, err
	}
	l := selection.Lineup{Bench: bench, Tactics: saved.Tactics}
	for _, s := range slots {
		l.Starters = append(l.Starters, selection.Slot{Player: s.Player, Role: s.Role})
	}
	var dropped []ids.PlayerID
	kept := l.Players()
	for _, p := range saved.Players() {
		if !slices.Contains(kept, p) {
			dropped = append(dropped, p)
		}
	}
	return l, dropped, true, nil
}

// TeamPlan is the user club's team plan as a lineup editor shows it. The
// plan is the lineup the club fields in every fixture without a submitted
// lineup, fitted to each match (see MatchdayLineup).
type TeamPlan struct {
	// Saved is false when the manager has saved no plan. Lineup is then a
	// starting point to edit: the lineup the club last fielded or had
	// submitted (by kickoff, then fixture ID), fitted to the squad as if
	// carried over, else the AI's selection, both under the rules of the
	// club's current league.
	Saved  bool
	Lineup selection.Lineup
	// The players of Lineup who could not be named in a match today, in
	// lineup order: no longer in the squad, or injured and not selectable.
	// They stay in a saved plan; each match leaves them out while they
	// cannot play and refills their places.
	Unavailable []ids.PlayerID
	// Every player of the squad in ascending ID order, with whether he
	// could be named today. Any of them may be named in the plan.
	Squad []LineupEligibility
}

func (p TeamPlan) clone() TeamPlan {
	p.Lineup = p.Lineup.Clone()
	p.Unavailable = slices.Clone(p.Unavailable)
	p.Squad = slices.Clone(p.Squad)
	return p
}

// TeamPlan returns the user club's team plan, matchday or not. Read-only.
func (w *World) TeamPlan() (TeamPlan, error) {
	team, ok := w.userTeam()
	if !ok {
		return TeamPlan{}, ErrNoUserClub
	}
	p := TeamPlan{Squad: w.eligibility(team)}
	if l, ok := w.selections.Plan(team); ok {
		p.Saved, p.Lineup = true, l
	} else {
		l, err := w.planStart(team)
		if err != nil {
			return TeamPlan{}, err
		}
		p.Lineup = l
	}
	for _, id := range p.Lineup.Players() {
		i, ok := slices.BinarySearchFunc(p.Squad, id, func(e LineupEligibility, id ids.PlayerID) int { return cmp.Compare(e.Player, id) })
		if !ok || !p.Squad[i].Eligibility.Selectable() {
			p.Unavailable = append(p.Unavailable, id)
		}
	}
	return p, nil
}

// planStart returns the starting point of an unsaved team plan (see
// TeamPlan.Saved). Read-only.
func (w *World) planStart(team ids.TeamID) (selection.Lineup, error) {
	rules, err := w.leagueRules(team)
	if err != nil {
		return selection.Lineup{}, err
	}
	if last, ok := w.latestLineup(team, math.MaxInt64, 0); ok {
		l, _, ok, err := w.fitLineup(team, last.Lineup, rules)
		if err != nil || ok {
			return l, err
		}
	}
	return w.selectTeam(team, rules)
}

// leagueRules returns the match rules of the league team plays in this
// season.
func (w *World) leagueRules(team ids.TeamID) (matches.Rules, error) {
	for _, l := range w.leagues {
		if entrants, ok := w.competitions.Entrants(l.season); ok && slices.Contains(entrants, team) {
			return w.matchRules(l.def.ID)
		}
	}
	return matches.Rules{}, fmt.Errorf("app: team %d plays in no current league", team)
}

// SetTeamPlan saves the user club's team plan, replacing any earlier one.
// It names no fixture and can be saved on matchday or between matches.
type SetTeamPlan struct {
	ID               CommandID
	ExpectedRevision Revision
	Lineup           selection.Lineup
}

// TeamPlanSaved is the recorded result of a SetTeamPlan command.
type TeamPlanSaved struct {
	Command  CommandID
	Revision Revision // world revision after the change
	Team     ids.TeamID
}

// TeamPlanRecord is a successful SetTeamPlan and its result.
type TeamPlanRecord struct {
	Request SetTeamPlan
	Result  TeamPlanSaved
}

func (r TeamPlanRecord) clone() TeamPlanRecord {
	r.Request.Lineup = r.Request.Lineup.Clone()
	return r
}

// SetTeamPlan saves the user club's team plan: the lineup it fields in
// every fixture without a submitted lineup, fitted to each match (see
// MatchdayLineup).
//
//   - Retries follow ResolveRounds: a recorded ID with the same content
//     returns the recorded result, different content is ErrCommandIDReused,
//     and failed attempts are not recorded.
//   - ExpectedRevision must equal Revision.
//   - The career must have a user club, and its match must not be live: a
//     live match's lineup changes through MatchDecision.
//   - The lineup must have a valid shape (selection.Lineup.Validate) and
//     name only players of the user club's squad. Injured players may be
//     named, and the bench is not held to a competition's limit: each
//     match leaves out whoever cannot play and cuts the bench to fit.
//
// It changes no submitted or played lineup. On success the revision
// increments. On error nothing changes.
func (w *World) SetTeamPlan(cmd SetTeamPlan) (TeamPlanSaved, error) {
	if !validCommandID(cmd.ID) {
		return TeamPlanSaved{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		if rec.teamPlan == nil || !sameTeamPlan(rec.teamPlan.Request, cmd) {
			return TeamPlanSaved{}, fmt.Errorf("%w: command %d", ErrCommandIDReused, cmd.ID)
		}
		return rec.teamPlan.Result, nil
	}
	if cmd.ExpectedRevision != w.revision {
		return TeamPlanSaved{}, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, cmd.ExpectedRevision, w.revision)
	}
	team, ok := w.userTeam()
	if !ok {
		return TeamPlanSaved{}, ErrNoUserClub
	}
	if w.live != nil {
		return TeamPlanSaved{}, fmt.Errorf("%w: fixture %d is live; use MatchDecision", ErrMatchInProgress, w.live.fixture)
	}
	if err := cmd.Lineup.Validate(); err != nil {
		return TeamPlanSaved{}, fmt.Errorf("%w for team %d: %v", ErrInvalidLineup, team, err)
	}
	squad := w.employment.Squad(team)
	for _, id := range cmd.Lineup.Players() {
		if _, ok := slices.BinarySearch(squad, id); !ok {
			return TeamPlanSaved{}, fmt.Errorf("%w for team %d: player %d is not in the squad", ErrInvalidLineup, team, id)
		}
	}
	if err := w.selections.SetPlan(selection.Plan{Team: team, Lineup: cmd.Lineup}); err != nil {
		return TeamPlanSaved{}, fmt.Errorf("%w: %v", ErrInvalidLineup, err)
	}

	// Committed. Nothing below can fail.
	w.revision++
	res := TeamPlanSaved{Command: cmd.ID, Revision: w.revision, Team: team}
	rec := TeamPlanRecord{Request: cmd, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{teamPlan: &rec}
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindTeamPlanSaved, TeamPlanSaved: &events.TeamPlanSaved{Team: team}})
	w.publish()
	return res, nil
}

func sameTeamPlan(a, b SetTeamPlan) bool {
	return a.ID == b.ID && a.ExpectedRevision == b.ExpectedRevision && a.Lineup.Equal(b.Lineup)
}

// restoreTeamPlan validates a recorded SetTeamPlan and adds it to the
// command log. The user team must still have a plan (possibly a later
// one).
func (w *World) restoreTeamPlan(c TeamPlanRecord, revision Revision) error {
	req, res := c.Request, c.Result
	if err := w.checkRecordID(req.ID, res.Command); err != nil {
		return err
	}
	if err := req.Lineup.Validate(); err != nil {
		return err
	}
	team, ok := w.userTeam()
	if !ok || res.Team != team {
		return fmt.Errorf("team plan for team %d is not the user team's", res.Team)
	}
	if _, ok := w.selections.Plan(team); !ok {
		return fmt.Errorf("no team plan stored for team %d", team)
	}
	if res.Revision <= req.ExpectedRevision || res.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", res.Revision, req.ExpectedRevision, revision)
	}
	rec := c.clone()
	w.commands[req.ID] = commandRecord{teamPlan: &rec}
	return nil
}

// Eligibility says whether a squad player may be named in the user club's
// lineup. Values are durable.
type Eligibility uint8

const (
	EligibleFit       Eligibility = 1 // fit to play
	EligibleInjured   Eligibility = 2 // injured, but the fit players cannot field a legal lineup, so the whole squad may play
	IneligibleInjured Eligibility = 3 // injured, and the fit players can field a lineup without him
)

func (e Eligibility) Valid() bool { return e >= EligibleFit && e <= IneligibleInjured }

// Selectable reports whether a player with this eligibility may be named.
func (e Eligibility) Selectable() bool { return e == EligibleFit || e == EligibleInjured }

func (e Eligibility) String() string {
	switch e {
	case EligibleFit:
		return "fit"
	case EligibleInjured:
		return "injured, selectable: the fit players cannot field a team"
	case IneligibleInjured:
		return "injured"
	}
	return fmt.Sprintf("Eligibility(%d)", uint8(e))
}

// LineupEligibility is one squad player's eligibility for a lineup.
type LineupEligibility struct {
	Player      ids.PlayerID
	Eligibility Eligibility
	DaysOut     uint16 // injured: the recovery days he still misses; zero when fit
}

// SquadEligibility returns, for a pending fixture of the user club, every
// player of its squad in ascending ID order and whether he may be named in
// its lineup. SubmitLineup applies the same rule: a lineup naming only
// selectable players is accepted as long as nothing changes in between.
// Read-only.
func (w *World) SquadEligibility(fixture ids.FixtureID) ([]LineupEligibility, error) {
	team, _, err := w.pendingUserFixture(fixture)
	if err != nil {
		return nil, err
	}
	return w.eligibility(team), nil
}

// eligibility returns every player of team's squad in ascending ID order
// with his eligibility (see availableSquad). Read-only.
func (w *World) eligibility(team ids.TeamID) []LineupEligibility {
	squad := w.employment.Squad(team)
	available := w.availableSquad(team)
	emergency := len(available) == len(squad)
	out := make([]LineupEligibility, 0, len(squad))
	for _, id := range squad {
		e := LineupEligibility{Player: id, Eligibility: EligibleFit}
		if days, injured := w.medical.DaysOut(id); injured {
			e.DaysOut = days
			e.Eligibility = IneligibleInjured
			if emergency {
				e.Eligibility = EligibleInjured
			}
		}
		out = append(out, e)
	}
	return out
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
//     competition's MaxBench, and only selectable players of the user club's
//     squad (see SquadEligibility: injured players are not, unless the fit
//     ones cannot field a lineup).
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
// competition's rules, and builds its detached match input from the
// authoritative profiles and current condition (see matchPlayer), whoever
// chose it. Starters play their slot's role; bench players their natural
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
	squad := w.eligibility(team)
	player := func(id ids.PlayerID) (matches.PlayerInput, error) {
		i, ok := slices.BinarySearchFunc(squad, id, func(e LineupEligibility, id ids.PlayerID) int { return cmp.Compare(e.Player, id) })
		if !ok {
			return matches.PlayerInput{}, fmt.Errorf("player %d is not in the squad", id)
		}
		if e := squad[i]; !e.Eligibility.Selectable() {
			return matches.PlayerInput{}, fmt.Errorf("player %d is %s for %d more days", id, e.Eligibility, e.DaysOut)
		}
		return w.matchPlayer(id)
	}
	in := matches.TeamInput{Team: team, Tactics: l.Tactics}
	for _, s := range l.Starters {
		p, err := player(s.Player)
		if err != nil {
			return fail("%v", err)
		}
		p.Role = s.Role
		in.Starters = append(in.Starters, p)
	}
	for _, id := range l.Bench {
		p, err := player(id)
		if err != nil {
			return fail("%v", err)
		}
		in.Bench = append(in.Bench, p)
	}
	return in, nil
}

// validateSelections checks submitted lineups and team plans against the
// other modules: the user club exists; each lineup is the user team's, for
// a fixture it plays that has kicked off (lineups are only accepted for the
// pending batch), with a bench within the league's rules; a lineup still
// awaiting its match selects only current squad players; and a team plan is
// the user team's and names registered players (who may have left since).
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
	for _, p := range w.selections.Plans() {
		if !hasUser || p.Team != team {
			fail("team plan of team %d, not the user team", p.Team)
			continue
		}
		for _, id := range p.Lineup.Players() {
			if _, ok := w.registry.Player(id); !ok {
				fail("team plan names unregistered player %d", id)
			}
		}
	}
	return errs
}
