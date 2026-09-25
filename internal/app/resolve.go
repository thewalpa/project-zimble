package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/simple"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
)

type (
	// CommandID identifies a command so a retry is answered, not re-applied.
	CommandID uint64
	// Revision increments on every committed world change.
	Revision uint64
)

var (
	ErrInvalidCommand   = errors.New("app: invalid command")
	ErrCommandIDReused  = errors.New("app: command ID reused with different content")
	ErrStaleRevision    = errors.New("app: stale expected revision")
	ErrNoPendingRounds  = errors.New("app: no rounds await results")
	ErrBatchMismatch    = errors.New("app: rounds differ from the pending batch")
	ErrOverlappingTeams = errors.New("app: a team appears in more than one fixture of the batch")
)

// Revision returns the current world revision.
func (w *World) Revision() Revision { return w.revision }

// ResolveRounds asks for every round in the pending batch to be played and
// its official results recorded.
type ResolveRounds struct {
	ID               CommandID
	ExpectedRevision Revision
	Rounds           []competitions.RoundRef // the whole pending batch, any order
}

// MatchReport summarizes one resolved fixture. Goals are match detail
// returned to the caller; only the score becomes the official result.
// Selected says, per side (home, away), whether a submitted lineup or the AI
// default was played.
type MatchReport struct {
	Fixture  ids.FixtureID
	Round    competitions.RoundRef
	Home     TeamLabel
	Away     TeamLabel
	Selected [2]SelectedBy
	Score    [2]uint16
	Goals    []matches.Goal
}

// RoundsResolved is the recorded result of a ResolveRounds command.
type RoundsResolved struct {
	Command  CommandID
	Revision Revision        // world revision after the change
	At       sim.GameInstant // results are official at this instant
	Rounds   []competitions.RoundRef
	Matches  []MatchReport // fixture ID order
}

// ResolveRecord is a successful ResolveRounds (Rounds in canonical order)
// and its complete result, including detailed match outcomes, so a retry is
// answered exactly as before.
type ResolveRecord struct {
	Request ResolveRounds
	Result  RoundsResolved
}

func (r ResolveRecord) clone() ResolveRecord {
	r.Request.Rounds = slices.Clone(r.Request.Rounds)
	r.Result = cloneResolved(r.Result)
	return r
}

// commandRecord is one successful command; exactly one field is set.
type commandRecord struct {
	resolve *ResolveRecord
	lineup  *LineupRecord
}

// plannedMatch is a fixture with its detached, validated match input.
type plannedMatch struct {
	fixture  competitions.Fixture
	round    competitions.RoundRef
	input    matches.MatchInput
	selected [2]SelectedBy
	stamina  map[ids.PlayerID]uint8 // every selected player's, for exposure
}

// ResolveRounds plays and records the whole pending batch as one unit of
// work. It never moves the clock: results are official at Now (the batch's
// kickoff), and Continue remains responsible for progression.
//
//   - A command ID that already succeeded with the same content returns the
//     recorded result without changing anything; different content with a
//     used ID is ErrCommandIDReused. Failed attempts are not recorded, so
//     the same ID may be retried.
//   - ExpectedRevision must equal Revision, and Rounds must equal the
//     pending batch exactly.
//   - Steps: prepare (validate fixtures, reject overlapping teams, take each
//     side's submitted lineup or else the AI selection, with current
//     condition, build detached inputs) -> simulate every fixture with its
//     own random stream -> validate every outcome -> plan every
//     participant's condition loss -> record all results and complete all
//     rounds in one competitions call -> apply the condition plan, bump the
//     revision and record the command.
//
// Any failure before the competitions call leaves the world exactly as it
// was; that call is itself all-or-nothing, and nothing after it can fail
// (the condition plan was validated against the unchanged medical store).
func (w *World) ResolveRounds(cmd ResolveRounds) (RoundsResolved, error) {
	if !validCommandID(cmd.ID) {
		return RoundsResolved{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	rounds, err := canonicalRounds(cmd.Rounds)
	if err != nil {
		return RoundsResolved{}, err
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		r := rec.resolve
		if r == nil || r.Request.ExpectedRevision != cmd.ExpectedRevision || !slices.Equal(r.Request.Rounds, rounds) {
			return RoundsResolved{}, fmt.Errorf("%w: command %d", ErrCommandIDReused, cmd.ID)
		}
		return cloneResolved(r.Result), nil
	}
	if cmd.ExpectedRevision != w.revision {
		return RoundsResolved{}, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, cmd.ExpectedRevision, w.revision)
	}
	var pending []competitions.RoundRef
	for _, r := range w.competitions.PendingRounds() {
		pending = append(pending, r.Ref)
	}
	if len(pending) == 0 {
		return RoundsResolved{}, ErrNoPendingRounds
	}
	pending, _ = canonicalRounds(pending)
	if !slices.Equal(rounds, pending) {
		return RoundsResolved{}, fmt.Errorf("%w: got %v, pending %v", ErrBatchMismatch, rounds, pending)
	}

	plan, err := w.prepareBatch(rounds)
	if err != nil {
		return RoundsResolved{}, err
	}
	outcomes, err := w.simulateBatch(plan)
	if err != nil {
		return RoundsResolved{}, err
	}
	scores := make([]competitions.Score, len(plan))
	var exposures []medical.Exposure
	for i, p := range plan {
		if err := w.checkOutcome(p, outcomes[i]); err != nil {
			return RoundsResolved{}, fmt.Errorf("app: fixture %d: %w", p.fixture.ID, err)
		}
		scores[i] = competitions.Score{Fixture: p.fixture.ID, HomeGoals: outcomes[i].Score[0], AwayGoals: outcomes[i].Score[1]}
		for _, pt := range outcomes[i].Participants {
			exposures = append(exposures, medical.Exposure{Player: pt.Player, Minutes: pt.Minutes(), Stamina: p.stamina[pt.Player]})
		}
	}
	wear, err := w.medical.PlanExposure(exposures)
	if err != nil {
		return RoundsResolved{}, fmt.Errorf("app: match exposure: %w", err)
	}
	if err := w.competitions.CompleteRounds(rounds, scores, w.Now()); err != nil {
		return RoundsResolved{}, fmt.Errorf("app: record results: %w", err)
	}

	// Committed. Nothing below can fail.
	w.applyMedical(wear)
	w.revision++
	res := RoundsResolved{Command: cmd.ID, Revision: w.revision, At: w.Now(), Rounds: rounds}
	for i, p := range plan {
		res.Matches = append(res.Matches, MatchReport{
			Fixture: p.fixture.ID, Round: p.round,
			Home: w.teamLabel(p.fixture.Home), Away: w.teamLabel(p.fixture.Away),
			Selected: p.selected, Score: outcomes[i].Score, Goals: outcomes[i].Goals,
		})
	}
	rec := ResolveRecord{Request: ResolveRounds{ID: cmd.ID, ExpectedRevision: cmd.ExpectedRevision, Rounds: rounds}, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{resolve: &rec}
	return res, nil
}

func validCommandID(id CommandID) bool { return id != 0 }

// canonicalRounds returns refs sorted by (competition, season, round) and
// rejects duplicates and zero values.
func canonicalRounds(refs []competitions.RoundRef) ([]competitions.RoundRef, error) {
	out := slices.Clone(refs)
	slices.SortFunc(out, func(a, b competitions.RoundRef) int {
		return cmp.Or(
			cmp.Compare(a.Season.Competition, b.Season.Competition),
			cmp.Compare(a.Season.Season, b.Season.Season),
			cmp.Compare(a.Round, b.Round),
		)
	})
	for i, r := range out {
		if !r.Season.Valid() || r.Round == 0 || (i > 0 && out[i-1] == r) {
			return nil, fmt.Errorf("%w: round %+v invalid or duplicated", ErrInvalidCommand, r)
		}
	}
	return out, nil
}

// prepareBatch validates the batch and builds every match input before any
// match runs. It only reads module state.
func (w *World) prepareBatch(rounds []competitions.RoundRef) ([]plannedMatch, error) {
	var plan []plannedMatch
	for _, ref := range rounds {
		info, ok := w.competitions.Round(ref)
		if !ok || info.Status != competitions.RoundAwaitingResults {
			return nil, fmt.Errorf("app: %s is not awaiting results", ref)
		}
		for _, id := range info.Fixtures {
			f, ok := w.competitions.Fixture(id)
			if !ok || f.Season != ref.Season || f.Round != ref.Round {
				return nil, fmt.Errorf("app: fixture %d does not belong to %s", id, ref)
			}
			if _, done := w.competitions.Result(id); done {
				return nil, fmt.Errorf("app: fixture %d already has a result", id)
			}
			plan = append(plan, plannedMatch{fixture: f, round: ref})
		}
	}
	slices.SortFunc(plan, func(a, b plannedMatch) int { return cmp.Compare(a.fixture.ID, b.fixture.ID) })

	playing := map[ids.TeamID]ids.FixtureID{}
	for _, p := range plan {
		for _, team := range []ids.TeamID{p.fixture.Home, p.fixture.Away} {
			if other, dup := playing[team]; dup {
				return nil, fmt.Errorf("%w: team %d in fixtures %d and %d", ErrOverlappingTeams, team, other, p.fixture.ID)
			}
			playing[team] = p.fixture.ID
		}
	}

	for i := range plan {
		p := &plan[i]
		rules, err := w.matchRules(p.fixture.Season.Competition)
		if err != nil {
			return nil, err
		}
		p.input = matches.MatchInput{Match: p.fixture.ID, Rules: rules}
		for _, side := range []matches.Side{matches.Home, matches.Away} {
			in, by, err := w.sideSelection(p.fixture, side, rules)
			if err != nil {
				return nil, err
			}
			*p.input.Team(side), p.selected[side.Index()] = in, by
		}
		p.stamina = map[ids.PlayerID]uint8{}
		for _, side := range []matches.Side{matches.Home, matches.Away} {
			t := p.input.Team(side)
			for _, pl := range append(slices.Clone(t.Starters), t.Bench...) {
				p.stamina[pl.Player] = pl.Ratings.Stamina
			}
		}
		if err := p.input.Validate(simple.MaxBench); err != nil {
			return nil, fmt.Errorf("app: fixture %d: %w", p.fixture.ID, err)
		}
	}
	return plan, nil
}

func (w *World) matchRules(comp ids.CompetitionID) (matches.Rules, error) {
	for _, l := range w.leagues {
		if l.def.ID == comp {
			return matches.Rules{MaxSubstitutions: l.def.MaxSubstitutions, MaxBench: l.def.MaxBench}, nil
		}
	}
	return matches.Rules{}, fmt.Errorf("app: competition %d has no league definition", comp)
}

// sideSelection returns the lineup a side plays: the one submitted for the
// fixture, revalidated against the current squad, or else the AI selection.
func (w *World) sideSelection(f competitions.Fixture, side matches.Side, rules matches.Rules) (matches.TeamInput, SelectedBy, error) {
	team := f.Home
	if side == matches.Away {
		team = f.Away
	}
	if l, ok := w.selections.Lineup(f.ID, team); ok {
		in, err := w.lineupInput(team, l, rules)
		if err != nil {
			return matches.TeamInput{}, 0, fmt.Errorf("app: fixture %d: %w", f.ID, err)
		}
		return in, SelectedByManager, nil
	}
	in, err := w.selectTeam(team, rules)
	return in, SelectedByAI, err
}

// selectTeam builds AI candidates from the team's current squad and picks a
// lineup. The candidates are detached copies of module data.
func (w *World) selectTeam(team ids.TeamID, rules matches.Rules) (matches.TeamInput, error) {
	t, ok := w.registry.Team(team)
	if !ok || t.Kind != registry.TeamSenior {
		return matches.TeamInput{}, fmt.Errorf("app: team %d is not a registered senior team", team)
	}
	var candidates []ai.Candidate
	for _, id := range w.employment.Squad(team) {
		c, err := w.candidate(id)
		if err != nil {
			return matches.TeamInput{}, err
		}
		candidates = append(candidates, c)
	}
	return ai.SelectTeam(team, candidates, rules)
}

// candidate is a detached copy of a player's profile and current condition.
func (w *World) candidate(id ids.PlayerID) (ai.Candidate, error) {
	p, ok := w.players.Profile(id)
	if !ok {
		return ai.Candidate{}, fmt.Errorf("app: player %d has no profile", id)
	}
	condition, ok := w.medical.Condition(id)
	if !ok {
		return ai.Candidate{}, fmt.Errorf("app: player %d has no condition record", id)
	}
	a := p.Attributes
	return ai.Candidate{
		Player:  p.Player,
		Natural: roleOf(p.Position),
		Ratings: matches.Ratings{
			Goalkeeping: uint8(a[players.Goalkeeping]), Defending: uint8(a[players.Defending]),
			Passing: uint8(a[players.Passing]), Finishing: uint8(a[players.Finishing]),
			Pace: uint8(a[players.Pace]), Stamina: uint8(a[players.Stamina]),
		},
		Condition: condition,
	}, nil
}

func roleOf(p players.Position) matches.Role {
	switch p {
	case players.Goalkeeper:
		return matches.Goalkeeper
	case players.Defender:
		return matches.Defender
	case players.Midfielder:
		return matches.Midfielder
	case players.Forward:
		return matches.Forward
	}
	return 0 // rejected by ai.SelectTeam
}

// simulateBatch runs every planned match to full time in fixture ID order,
// each with its own random stream. It never touches world state; outcomes
// are copied out of the reused step buffer.
func (w *World) simulateBatch(plan []plannedMatch) ([]matches.MatchOutcome, error) {
	outcomes := make([]matches.MatchOutcome, len(plan))
	var dst matches.MatchStepResult
	for i := range plan {
		p := &plan[i]
		rs := matches.FixtureRandom(w.seed, w.engine.ID(), w.engine.Version(), p.fixture.ID)
		session, err := w.engine.Start(&p.input, rs)
		if err != nil {
			return nil, fmt.Errorf("app: start fixture %d: %w", p.fixture.ID, err)
		}
		finished := false
		// Regulation needs two calls: to half time, then to full time. The
		// bound guards against an engine that never finishes.
		for range 4 {
			if err := session.Advance(matches.AdvanceRequest{ToMinute: matches.RegulationMinutes}, &dst); err != nil {
				return nil, fmt.Errorf("app: simulate fixture %d: %w", p.fixture.ID, err)
			}
			if dst.Status == matches.MatchFinished {
				finished = true
				break
			}
		}
		if !finished {
			return nil, fmt.Errorf("app: fixture %d did not reach full time", p.fixture.ID)
		}
		o := dst.Outcome
		o.Goals, o.Participants = slices.Clone(o.Goals), slices.Clone(o.Participants)
		outcomes[i] = o
	}
	return outcomes, nil
}

// checkOutcome verifies an outcome against its input before anything is
// recorded.
func (w *World) checkOutcome(p plannedMatch, o matches.MatchOutcome) error {
	switch {
	case o.Status != matches.ResultCompleted:
		return fmt.Errorf("result status %d is not completed", o.Status)
	case o.Resolution != matches.ResolutionRegulation:
		return fmt.Errorf("resolution %d is not regulation", o.Resolution)
	case o.Match != p.fixture.ID:
		return fmt.Errorf("outcome is for match %d", o.Match)
	case o.EngineID != w.engine.ID() || o.EngineVersion != w.engine.Version():
		return fmt.Errorf("outcome from engine %s v%d", o.EngineID, o.EngineVersion)
	case o.Score[0] > competitions.MaxGoals || o.Score[1] > competitions.MaxGoals:
		return fmt.Errorf("score %v exceeds %d", o.Score, competitions.MaxGoals)
	}

	selected := map[ids.PlayerID]matches.Side{}
	for _, side := range []matches.Side{matches.Home, matches.Away} {
		t := p.input.Team(side)
		for _, pl := range t.Starters {
			selected[pl.Player] = side
		}
		for _, pl := range t.Bench {
			selected[pl.Player] = side
		}
	}
	var minutes [2]int
	on := map[ids.PlayerID]matches.Participation{}
	for _, pt := range o.Participants {
		if side, ok := selected[pt.Player]; !ok || side != pt.Side {
			return fmt.Errorf("participant %d was not selected for the %s side", pt.Player, pt.Side)
		}
		if _, dup := on[pt.Player]; dup || pt.OnMinute > pt.OffMinute || pt.OffMinute > matches.RegulationMinutes {
			return fmt.Errorf("participation %+v is invalid", pt)
		}
		on[pt.Player] = pt
		minutes[pt.Side.Index()] += int(pt.Minutes())
	}
	if want := matches.StartersPerTeam * matches.RegulationMinutes; minutes != [2]int{want, want} {
		return fmt.Errorf("participant minutes %v, want %d per side", minutes, want)
	}
	var goals [2]uint16
	for _, g := range o.Goals {
		pt, ok := on[g.Scorer]
		if !g.Side.Valid() || !ok || pt.Side != g.Side || g.Minute <= pt.OnMinute || g.Minute > pt.OffMinute {
			return fmt.Errorf("goal %+v not scored by a participant on the pitch", g)
		}
		goals[g.Side.Index()]++
	}
	if goals != o.Score {
		return fmt.Errorf("goals %v disagree with score %v", goals, o.Score)
	}
	return nil
}

func cloneResolved(r RoundsResolved) RoundsResolved {
	r.Rounds = slices.Clone(r.Rounds)
	r.Matches = slices.Clone(r.Matches)
	for i := range r.Matches {
		r.Matches[i].Goals = slices.Clone(r.Matches[i].Goals)
	}
	return r
}
