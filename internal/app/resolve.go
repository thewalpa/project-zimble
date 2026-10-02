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
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/simple"
	"github.com/thewalpa/project-zimble/internal/matches/tick"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
	"github.com/thewalpa/project-zimble/internal/selection"
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
	ErrUnknownEngine    = errors.New("app: unknown match engine")
)

// Engines lists the match engines a career can be played on; the first is
// the default.
//
// A career has one football model: the engine chosen when it starts plays
// every fixture of every competition, the manager's watched match and all
// background matches alike, and the save pins it (Versions.EngineID and
// EngineVersion). Watching a match never changes the rules it is played by.
func Engines() []string { return []string{simple.EngineID, tick.EngineID} }

// MatchEngine names the career's match engine and what it can do; a client
// offers a pitch view when Capabilities.PositionalFrames is set.
type MatchEngine struct {
	ID           string
	Version      uint32
	Capabilities matches.Capabilities
}

// MatchEngine returns the engine that plays every fixture of the career.
func (w *World) MatchEngine() MatchEngine {
	return MatchEngine{ID: w.engine.ID(), Version: w.engine.Version(), Capabilities: w.engine.Capabilities()}
}

// newEngine builds the named match engine at this build's version. The empty
// name is the default engine.
func newEngine(id string) (matches.Engine, error) {
	switch id {
	case "", simple.EngineID:
		return simple.New(simple.DefaultParams())
	case tick.EngineID:
		return tick.New(tick.DefaultParams())
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownEngine, id)
}

// engineMaxBench is the largest bench the engine accepts.
func engineMaxBench(e matches.Engine) int {
	if e.ID() == tick.EngineID {
		return tick.MaxBench
	}
	return simple.MaxBench
}

// Revision returns the current world revision.
func (w *World) Revision() Revision { return w.revision }

// ResolveRounds asks for every round in the pending batch to be played and
// its official results recorded.
type ResolveRounds struct {
	ID               CommandID
	ExpectedRevision Revision
	Rounds           []competitions.RoundRef // the whole pending batch, any order
}

// MatchReport summarizes one resolved fixture. Goals and Events are match
// detail kept with the report; only the score becomes the official result.
// Selected says, per side (home, away), whether a submitted lineup or the AI
// default was played; Lineups holds the lineup each side started the match
// with, whoever chose it.
type MatchReport struct {
	Fixture  ids.FixtureID
	Round    competitions.RoundRef
	Home     TeamLabel
	Away     TeamLabel
	Selected [2]SelectedBy
	// Lineups are the selections each side (home, away) fielded: starters
	// in slot order with the roles they played, the bench and the tactics
	// they started with. Later substitutions and mentality changes are in
	// Events. Empty for a result recorded without a report.
	Lineups  [2]selection.Lineup
	Score    [2]uint16
	Shootout [2]uint16 // a knockout match level after regulation: penalties scored
	Goals    []matches.Goal
	// Events are every match event in order (Seq 1..n): goals,
	// substitutions, mentality changes and the two period ends, including
	// those of a live match before ResolveRounds.
	Events []matches.MatchEvent
	// Stats are both sides' match statistics, when the career's engine has
	// the DetailedStats capability; otherwise Available is false. Not
	// available for a result recorded without a report.
	Stats matches.MatchStats
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

// plannedMatch is a fixture with its detached, validated match input.
type plannedMatch struct {
	fixture  competitions.Fixture
	round    competitions.RoundRef
	input    matches.MatchInput
	selected [2]SelectedBy
	stamina  map[ids.PlayerID]uint8 // every selected player's, for exposure
	carried  []selection.Entry      // carried-over lineups, stored once the results are
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
//   - Steps: the shared batch pipeline (see resolveBatch), with every event
//     caused by this command, then bump the revision and record the command.
//
// Any failure before the competitions call leaves the world exactly as it
// was; that call is itself all-or-nothing, and nothing after it can fail
// (the condition and gate-receipt plans were validated against the
// unchanged medical and finance stores).
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

	batch, err := w.resolveBatch(rounds, func(competitions.RoundRef) events.Cause { return commandCause(cmd.ID) })
	if err != nil {
		return RoundsResolved{}, err
	}
	// Committed. Nothing below can fail.
	w.revision++
	res := RoundsResolved{Command: cmd.ID, Revision: w.revision, At: batch.At, Rounds: batch.Rounds, Matches: batch.Matches}
	rec := ResolveRecord{Request: ResolveRounds{ID: cmd.ID, ExpectedRevision: cmd.ExpectedRevision, Rounds: rounds}, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{resolve: &rec}
	w.publish()
	return res, nil
}

// resolveBatch plays and records the given rounds as one unit of work: the
// pipeline shared by the ResolveRounds command and Continue's auto-resolve
// (see resolveAutomatically). It never moves the clock: results are official
// at Now (the batch's kickoff), and Continue remains responsible for
// progression. causeFor names the cause of each round's events (the command,
// or the round's kickoff task).
//
//   - Steps: prepare (validate fixtures, reject overlapping teams, take each
//     side's manager lineup (submitted, from the team plan or carried
//     over, see MatchdayLineup) or else the AI selection, with current
//     condition, build detached inputs) -> simulate every fixture with its
//     own random stream -> validate every outcome -> draw the injuries
//     (see injuries.go) and plan every participant's condition loss -> record all results and complete all
//     rounds in one competitions call -> apply the condition plan, store
//     each lineup fitted from the plan or carried over for the fixture it
//     was played in, and stage the events.
//
// Any failure before the competitions call leaves the world exactly as it
// was; that call is itself all-or-nothing, and nothing after it can fail
// (the condition and gate-receipt plans were validated against the
// unchanged medical and finance stores). The caller records commands, bumps
// the revision and publishes the staged events.
func (w *World) resolveBatch(rounds []competitions.RoundRef, causeFor func(competitions.RoundRef) events.Cause) (BatchResolved, error) {
	plan, err := w.prepareBatch(rounds)
	if err != nil {
		return BatchResolved{}, err
	}
	outcomes, played, err := w.simulateBatch(plan)
	if err != nil {
		return BatchResolved{}, err
	}
	scores := make([]competitions.Score, len(plan))
	var exposures []medical.Exposure
	for i, p := range plan {
		if err := w.checkOutcome(p, outcomes[i]); err != nil {
			return BatchResolved{}, fmt.Errorf("app: fixture %d: %w", p.fixture.ID, err)
		}
		if err := checkMatchEvents(played[i], outcomes[i].Goals); err != nil {
			return BatchResolved{}, fmt.Errorf("app: fixture %d: %w", p.fixture.ID, err)
		}
		o := outcomes[i]
		scores[i] = competitions.Score{Fixture: p.fixture.ID, HomeGoals: o.Score[0], AwayGoals: o.Score[1], HomePenalties: o.Shootout[0], AwayPenalties: o.Shootout[1]}
		for _, pt := range outcomes[i].Participants {
			exposures = append(exposures, medical.Exposure{Player: pt.Player, Minutes: pt.Minutes(), Stamina: p.stamina[pt.Player]})
		}
	}
	injuries, err := w.injuryRolls(plan, outcomes)
	if err != nil {
		return BatchResolved{}, err
	}
	wear, err := w.medical.PlanExposure(exposures, injuries)
	if err != nil {
		return BatchResolved{}, fmt.Errorf("app: match exposure: %w", err)
	}
	gates, err := w.gatePostings(plan)
	if err != nil {
		return BatchResolved{}, err
	}
	receipts, err := w.finance.Plan(w.Now(), gates)
	if err != nil {
		return BatchResolved{}, fmt.Errorf("app: gate receipts: %w", err)
	}
	if err := w.competitions.CompleteRounds(rounds, scores, w.Now()); err != nil {
		return BatchResolved{}, fmt.Errorf("app: record results: %w", err)
	}

	// Committed. Nothing below can fail.
	w.applyMedical(wear)
	w.applyFinance(receipts)
	for _, p := range plan {
		for _, e := range p.carried {
			// The lineup passed lineupInput, so its shape is valid.
			if err := w.selections.Submit(e); err != nil {
				panic(fmt.Sprintf("app: unreachable: %v", err))
			}
		}
	}
	w.live = nil // a live match is now finished and official
	res := BatchResolved{At: w.Now(), Rounds: slices.Clone(rounds)}
	for i, p := range plan {
		res.Matches = append(res.Matches, MatchReport{
			Fixture: p.fixture.ID, Round: p.round,
			Home: w.teamLabel(p.fixture.Home), Away: w.teamLabel(p.fixture.Away),
			Selected: p.selected, Lineups: [2]selection.Lineup{lineupOf(p.input.Home), lineupOf(p.input.Away)},
			Score: outcomes[i].Score, Shootout: outcomes[i].Shootout, Goals: outcomes[i].Goals,
			Events: played[i], Stats: outcomes[i].Stats,
		})
	}
	for i, p := range plan {
		r, _ := w.competitions.Result(p.fixture.ID)
		appeared, scorers := outcomeFacts(outcomes[i])
		w.emit(w.Now(), causeFor(p.round), events.Event{Kind: events.KindMatchCompleted, MatchCompleted: &events.MatchCompleted{
			Fixture: r.Fixture, Competition: r.Season.Competition, Season: uint16(r.Season.Season), Round: uint8(r.Round),
			Home: r.Home, Away: r.Away, HomeGoals: r.HomeGoals, AwayGoals: r.AwayGoals,
			HomePenalties: r.HomePenalties, AwayPenalties: r.AwayPenalties,
			Appeared: appeared, Scorers: scorers,
		}})
	}
	// Gate receipts and injuries cite the round of their fixture: on
	// Continue's path, rounds of different competitions have different
	// kickoff tasks. A team plays once per batch, so a player in one match.
	roundOf := make(map[ids.FixtureID]competitions.RoundRef, len(plan))
	playedIn := map[ids.PlayerID]competitions.RoundRef{}
	for i, p := range plan {
		roundOf[p.fixture.ID] = p.round
		for _, pt := range outcomes[i].Participants {
			playedIn[pt.Player] = p.round
		}
	}
	var causes []events.Cause // in order of first entry
	byCause := map[events.Cause][]finance.Entry{}
	for _, e := range receipts.Entries() {
		c := causeFor(roundOf[e.Fixture])
		if _, ok := byCause[c]; !ok {
			causes = append(causes, c)
		}
		byCause[c] = append(byCause[c], e)
	}
	for _, c := range causes {
		w.emitLedgerEntries(w.Now(), c, byCause[c])
	}
	for _, in := range w.injuredEvents(injuries) {
		w.emit(w.Now(), causeFor(playedIn[in.Player]), events.Event{Kind: events.KindPlayerInjured, PlayerInjured: &in})
	}
	return res, nil
}

// outcomeFacts are the MatchCompleted facts of a checked outcome: every
// participant (on the pitch at some point), ascending ID, and each goal's
// scorer in match order.
func outcomeFacts(o matches.MatchOutcome) (appeared, scorers []ids.PlayerID) {
	for _, pt := range o.Participants {
		appeared = append(appeared, pt.Player)
	}
	slices.Sort(appeared)
	for _, g := range o.Goals {
		scorers = append(scorers, g.Scorer)
	}
	return appeared, scorers
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
			in, by, carried, err := w.sideSelection(p.fixture, side, rules)
			if err != nil {
				return nil, err
			}
			*p.input.Team(side), p.selected[side.Index()] = in, by
			if carried != nil {
				p.carried = append(p.carried, *carried)
			}
		}
		p.stamina = map[ids.PlayerID]uint8{}
		for _, side := range []matches.Side{matches.Home, matches.Away} {
			t := p.input.Team(side)
			for _, pl := range append(slices.Clone(t.Starters), t.Bench...) {
				p.stamina[pl.Player] = pl.Ratings.Stamina
			}
		}
		if err := p.input.Validate(engineMaxBench(w.engine)); err != nil {
			return nil, fmt.Errorf("app: fixture %d: %w", p.fixture.ID, err)
		}
	}
	return plan, nil
}

// matchRules returns a competition's match rules. Cup and play-off matches
// are knockout matches, which need an engine that can take penalty shootouts.
func (w *World) matchRules(comp ids.CompetitionID) (matches.Rules, error) {
	if li, ok := w.leagueIndex(comp); ok {
		d := w.leagues[li].def
		return matches.Rules{MaxSubstitutions: d.MaxSubstitutions, MaxBench: d.MaxBench}, nil
	}
	if ci, ok := w.cupIndex(comp); ok {
		if !w.engine.Capabilities().Penalties {
			return matches.Rules{}, fmt.Errorf("app: the %s engine cannot decide cup matches: no penalties", w.engine.ID())
		}
		d := w.cups[ci]
		return matches.Rules{MaxSubstitutions: d.MaxSubstitutions, MaxBench: d.MaxBench, Knockout: true}, nil
	}
	if l, ok := w.playoffLink(comp); ok {
		if !w.engine.Capabilities().Penalties {
			return matches.Rules{}, fmt.Errorf("app: the %s engine cannot decide play-off matches: no penalties", w.engine.ID())
		}
		li, ok := w.leagueIndex(l.Lower) // the link's lower division supplies the match rules
		if !ok {
			return matches.Rules{}, fmt.Errorf("app: play-off competition %d: link's lower league %d has no definition", comp, l.Lower)
		}
		d := w.leagues[li].def
		return matches.Rules{MaxSubstitutions: d.MaxSubstitutions, MaxBench: d.MaxBench, Knockout: true}, nil
	}
	return matches.Rules{}, fmt.Errorf("app: competition %d has no league, cup or play-off definition", comp)
}

// sideSelection returns the lineup a side plays: the manager's (submitted
// for the fixture, from the team plan or carried over; see MatchdayLineup),
// revalidated against the current squad, or else the AI selection. A lineup
// fitted from the plan or carried over is also returned as the entry to
// store for the fixture once it is played, so a later match can carry it on
// and the report's SelectedByManager always has a stored lineup behind it.
func (w *World) sideSelection(f competitions.Fixture, side matches.Side, rules matches.Rules) (matches.TeamInput, SelectedBy, *selection.Entry, error) {
	team := f.Home
	if side == matches.Away {
		team = f.Away
	}
	m, ok, err := w.managerLineup(f, team, rules)
	if err != nil {
		return matches.TeamInput{}, 0, nil, fmt.Errorf("app: fixture %d: %w", f.ID, err)
	}
	if ok {
		in, err := w.lineupInput(team, m.Lineup, rules)
		if err != nil {
			return matches.TeamInput{}, 0, nil, fmt.Errorf("app: fixture %d: %w", f.ID, err)
		}
		var carried *selection.Entry
		if m.Source != LineupFromSubmission {
			carried = &selection.Entry{Fixture: f.ID, Team: team, Lineup: m.Lineup}
		}
		return in, SelectedByManager, carried, nil
	}
	l, err := w.selectTeam(team, rules)
	if err != nil {
		return matches.TeamInput{}, 0, nil, err
	}
	in, err := w.lineupInput(team, l, rules)
	if err != nil {
		return matches.TeamInput{}, 0, nil, fmt.Errorf("app: fixture %d: AI lineup: %w", f.ID, err)
	}
	return in, SelectedByAI, nil, nil
}

// selectTeam is the AI's lineup for a senior team: chosen from its
// available squad (see availableSquad) by what its club knows about those
// players (see knownCandidates). It is a decision only; lineupInput builds
// the match input from it. Read-only.
func (w *World) selectTeam(team ids.TeamID, rules matches.Rules) (selection.Lineup, error) {
	t, ok := w.registry.Team(team)
	if !ok || t.Kind != registry.TeamSenior {
		return selection.Lineup{}, fmt.Errorf("app: team %d is not a registered senior team", team)
	}
	candidates, err := w.knownCandidates(team, w.availableSquad(team))
	if err != nil {
		return selection.Lineup{}, err
	}
	sel, err := ai.SelectTeam(team, candidates, rules)
	if err != nil {
		return selection.Lineup{}, err
	}
	return aiLineup(sel), nil
}

// aiLineup is an AI selection as a lineup.
func aiLineup(sel ai.Lineup) selection.Lineup {
	l := selection.Lineup{Bench: slices.Clone(sel.Bench), Tactics: sel.Tactics}
	for _, s := range sel.Starters {
		l.Starters = append(l.Starters, selection.Slot{Player: s.Player, Role: s.Role})
	}
	return l
}

// knownCandidates is what team's club knows about the given players (see
// ObservePlayers) as AI selection candidates, whoever manages the club. It
// steers lineup decisions only (the AI's, suggestions and refilled
// vacancies); match input never comes from it (see matchPlayer), so a
// club's knowledge decides who plays, never how well. Read-only.
func (w *World) knownCandidates(team ids.TeamID, squad []ids.PlayerID) ([]ai.Candidate, error) {
	t, ok := w.registry.Team(team)
	if !ok {
		return nil, fmt.Errorf("app: team %d is not registered", team)
	}
	known, err := w.ObservePlayers(t.Club, squad)
	if err != nil {
		return nil, fmt.Errorf("app: team %d: %w", team, err)
	}
	out := make([]ai.Candidate, 0, len(known.Players))
	for _, p := range known.Players {
		out = append(out, ai.Candidate{Player: p.Player, Natural: roleOf(p.Position), Ratings: matchRatings(p.Attributes), Condition: p.Condition})
	}
	return out, nil
}

// matchPlayer is a player's authoritative match input in his natural role:
// his profile's attributes and his current condition. Detached.
func (w *World) matchPlayer(id ids.PlayerID) (matches.PlayerInput, error) {
	p, ok := w.players.Profile(id)
	if !ok {
		return matches.PlayerInput{}, fmt.Errorf("app: player %d has no profile", id)
	}
	condition, ok := w.medical.Condition(id)
	if !ok {
		return matches.PlayerInput{}, fmt.Errorf("app: player %d has no condition record", id)
	}
	return matches.PlayerInput{Player: p.Player, Role: roleOf(p.Position), Ratings: matchRatings(p.Attributes), Condition: condition}, nil
}

// matchRatings is the match contract's view of a player's attributes.
func matchRatings(a players.Attributes) matches.Ratings {
	return matches.Ratings{
		Goalkeeping: uint8(a[players.Goalkeeping]), Defending: uint8(a[players.Defending]),
		Passing: uint8(a[players.Passing]), Finishing: uint8(a[players.Finishing]),
		Pace: uint8(a[players.Pace]), Stamina: uint8(a[players.Stamina]),
		Dribbling: uint8(a[players.Dribbling]), Heading: uint8(a[players.Heading]),
		Strength: uint8(a[players.Strength]), Acceleration: uint8(a[players.Acceleration]),
		Positioning: uint8(a[players.Positioning]),
	}
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
// each with its own random stream. The manager's live match, if any, is
// replayed from its stops and continued from there. It never touches world state; outcomes
// are copied out of the reused step buffer.
func (w *World) simulateBatch(plan []plannedMatch) ([]matches.MatchOutcome, [][]matches.MatchEvent, error) {
	outcomes := make([]matches.MatchOutcome, len(plan))
	played := make([][]matches.MatchEvent, len(plan))
	var dst matches.MatchStepResult
	for i := range plan {
		p := &plan[i]
		var session matches.MatchSession
		if w.live != nil && w.live.fixture == p.fixture.ID {
			// The manager's live match continues from its recorded stops.
			r, err := w.replay(*p, w.live.stops)
			if err != nil {
				return nil, nil, fmt.Errorf("app: resume fixture %d: %w", p.fixture.ID, err)
			}
			session, played[i] = r.session, r.events
		} else {
			var err error
			if session, err = w.engine.Start(&p.input, matchRandom(w, p.fixture.ID)); err != nil {
				return nil, nil, fmt.Errorf("app: start fixture %d: %w", p.fixture.ID, err)
			}
		}
		finished := false
		// Regulation needs two calls: to half time, then to full time. The
		// bound guards against an engine that never finishes.
		for range 4 {
			if err := session.Advance(matches.AdvanceRequest{ToMinute: matches.RegulationMinutes}, &dst); err != nil {
				return nil, nil, fmt.Errorf("app: simulate fixture %d: %w", p.fixture.ID, err)
			}
			played[i] = append(played[i], dst.Events...)
			if dst.Status == matches.MatchFinished {
				finished = true
				break
			}
		}
		if !finished {
			return nil, nil, fmt.Errorf("app: fixture %d did not reach full time", p.fixture.ID)
		}
		o := dst.Outcome
		o.Goals, o.Participants = slices.Clone(o.Goals), slices.Clone(o.Participants)
		outcomes[i] = o
	}
	return outcomes, played, nil
}

// validShape reports whether roles are a possible formation: valid roles
// with exactly one goalkeeper.
func validShape(roles [matches.StartersPerTeam]matches.Role) bool {
	keepers := 0
	for _, r := range roles {
		if !r.Valid() {
			return false
		}
		if r == matches.Goalkeeper {
			keepers++
		}
	}
	return keepers == 1
}

// checkMatchEvents verifies a completed match's events against its goals, as
// the engine contract promises: Seq 1..n in minute order within regulation,
// known kinds with a valid side where one is needed, goal events equal to
// the goals, and the two period ends at 45 and 90.
func checkMatchEvents(evs []matches.MatchEvent, goals []matches.Goal) error {
	var fromEvents []matches.Goal
	var periodEnds []uint16
	for i, e := range evs {
		if e.Seq != uint32(i+1) || e.Minute > matches.RegulationMinutes || (i > 0 && e.Minute < evs[i-1].Minute) {
			return fmt.Errorf("event %d (seq %d, minute %d) out of order", i, e.Seq, e.Minute)
		}
		switch e.Kind {
		case matches.EventGoal, matches.EventSubstitution, matches.EventMentalityChange, matches.EventFormationChange:
			if !e.Side.Valid() {
				return fmt.Errorf("event %d has side %d", e.Seq, e.Side)
			}
			if e.Kind == matches.EventFormationChange && !validShape(e.Roles) {
				return fmt.Errorf("event %d has roles %v", e.Seq, e.Roles)
			}
			if e.Kind == matches.EventGoal {
				fromEvents = append(fromEvents, matches.Goal{Minute: e.Minute, Side: e.Side, Scorer: e.Player})
			}
		case matches.EventPeriodEnd:
			periodEnds = append(periodEnds, e.Minute)
		default:
			return fmt.Errorf("event %d has kind %d", e.Seq, e.Kind)
		}
	}
	if !slices.Equal(fromEvents, goals) {
		return fmt.Errorf("goal events %v disagree with goals %v", fromEvents, goals)
	}
	if !slices.Equal(periodEnds, []uint16{matches.HalfTimeMinute, matches.RegulationMinutes}) {
		return fmt.Errorf("periods end at %v", periodEnds)
	}
	return nil
}

// checkOutcome verifies an outcome against its input before anything is
// recorded.
func (w *World) checkOutcome(p plannedMatch, o matches.MatchOutcome) error {
	switch {
	case o.Status != matches.ResultCompleted:
		return fmt.Errorf("result status %d is not completed", o.Status)
	case o.Resolution != matches.ResolutionRegulation && o.Resolution != matches.ResolutionPenalties:
		return fmt.Errorf("resolution %d is neither regulation nor penalties", o.Resolution)
	case (o.Resolution == matches.ResolutionPenalties) != (p.input.Rules.Knockout && o.Score[0] == o.Score[1]):
		return fmt.Errorf("resolution %d for score %v (knockout %t)", o.Resolution, o.Score, p.input.Rules.Knockout)
	case o.Resolution == matches.ResolutionRegulation && o.Shootout != [2]uint16{}:
		return fmt.Errorf("shootout %v without penalties", o.Shootout)
	case o.Resolution == matches.ResolutionPenalties && (o.Shootout[0] == o.Shootout[1] || o.Shootout[0] > competitions.MaxGoals || o.Shootout[1] > competitions.MaxGoals):
		return fmt.Errorf("shootout %v decides nothing", o.Shootout)
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

func cloneLineups(l [2]selection.Lineup) [2]selection.Lineup {
	return [2]selection.Lineup{l[0].Clone(), l[1].Clone()}
}

func cloneResolved(r RoundsResolved) RoundsResolved {
	r.Rounds = slices.Clone(r.Rounds)
	r.Matches = slices.Clone(r.Matches)
	for i := range r.Matches {
		r.Matches[i].Goals = slices.Clone(r.Matches[i].Goals)
		r.Matches[i].Events = slices.Clone(r.Matches[i].Events)
		r.Matches[i].Lineups = cloneLineups(r.Matches[i].Lineups)
	}
	return r
}

// MatchReport returns the match report for a completed fixture, or false if
// none is recorded. Read-only.
func (w *World) MatchReport(fixture ids.FixtureID) (MatchReport, bool) {
	for _, rec := range w.commands {
		if rec.resolve == nil {
			continue
		}
		for _, m := range rec.resolve.Result.Matches {
			if m.Fixture == fixture {
				m.Goals, m.Events, m.Lineups = slices.Clone(m.Goals), slices.Clone(m.Events), cloneLineups(m.Lineups)
				return m, true
			}
		}
	}
	res, ok := w.competitions.Result(fixture)
	if !ok {
		return MatchReport{}, false
	}
	return MatchReport{
		Fixture:  fixture,
		Round:    competitions.RoundRef{Season: res.Season, Round: res.Round},
		Home:     w.teamLabel(res.Home),
		Away:     w.teamLabel(res.Away),
		Score:    [2]uint16{res.HomeGoals, res.AwayGoals},
		Shootout: [2]uint16{res.HomePenalties, res.AwayPenalties},
	}, true
}
