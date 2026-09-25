package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

var (
	ErrNoLiveMatch     = errors.New("app: no match in progress")
	ErrMatchInProgress = errors.New("app: the match is in progress")
	ErrMatchDecision   = errors.New("app: invalid match decision")
)

// LiveStop is one point where the manager stopped a live match: the minute
// reached and the decisions made there, in order.
type LiveStop struct {
	Minute   uint16
	Commands []matches.MatchCommand
}

// liveState is the manager's match in progress. It is a replay log, not a
// session: the session is rebuilt from the frozen match input, the fixture's
// random stream and these stops whenever it is needed. A world with a live
// match can therefore be saved and restored without engine checkpoints.
type liveState struct {
	fixture ids.FixtureID
	stops   []LiveStop // strictly increasing minutes
}

func cloneStops(stops []LiveStop) []LiveStop {
	out := slices.Clone(stops)
	for i := range out {
		out[i].Commands = slices.Clone(out[i].Commands)
	}
	return out
}

// PlayMatch plays the manager's match in the pending batch live, up to
// ToMinute. It always stops at half time when crossing it, so the half-time
// decision is never skipped.
type PlayMatch struct {
	ID               CommandID
	ExpectedRevision Revision
	Fixture          ids.FixtureID
	ToMinute         uint16
}

// MatchDecision is a substitution or mentality change for the manager's side
// of the live match, taking effect from the next minute.
type MatchDecision struct {
	ID               CommandID
	ExpectedRevision Revision
	Fixture          ids.FixtureID
	Command          matches.MatchCommand
}

// LiveMatch is the state of the manager's match in progress. Events are all
// match events so far, in order; Teams are both sides' selections (so the
// bench can be shown). The score is not official until ResolveRounds.
type LiveMatch struct {
	Fixture  ids.FixtureID
	Side     matches.Side // the manager's side
	Home     TeamLabel
	Away     TeamLabel
	Position matches.MatchPosition
	Status   matches.MatchStatus
	View     matches.MatchView
	Events   []matches.MatchEvent
	Teams    [2]matches.TeamInput
	Rules    matches.Rules
}

func (l LiveMatch) clone() LiveMatch {
	l.Events = slices.Clone(l.Events)
	for i := range l.Teams {
		l.Teams[i].Starters = slices.Clone(l.Teams[i].Starters)
		l.Teams[i].Bench = slices.Clone(l.Teams[i].Bench)
	}
	return l
}

// MatchStepped is the recorded result of PlayMatch and MatchDecision.
type MatchStepped struct {
	Command  CommandID
	Revision Revision
	Live     LiveMatch
}

type PlayMatchRecord struct {
	Request PlayMatch
	Result  MatchStepped
}

type DecisionRecord struct {
	Request MatchDecision
	Result  MatchStepped
}

func (r PlayMatchRecord) clone() PlayMatchRecord {
	r.Result.Live = r.Result.Live.clone()
	return r
}

func (r DecisionRecord) clone() DecisionRecord {
	r.Result.Live = r.Result.Live.clone()
	return r
}

// replayed is a live session rebuilt from its stops.
type replayed struct {
	plan    plannedMatch
	session matches.MatchSession
	step    matches.MatchStepResult // after the last stop
	events  []matches.MatchEvent    // every event so far
}

// livePlan builds the frozen input of fixture from the pending batch,
// exactly as ResolveRounds will.
func (w *World) livePlan(fixture ids.FixtureID) (plannedMatch, error) {
	ready, ok := w.pendingRounds()
	if !ok {
		return plannedMatch{}, ErrNoPendingRounds
	}
	refs, _ := canonicalRounds(readyRefs(ready))
	plan, err := w.prepareBatch(refs)
	if err != nil {
		return plannedMatch{}, err
	}
	for _, p := range plan {
		if p.fixture.ID == fixture {
			return p, nil
		}
	}
	return plannedMatch{}, fmt.Errorf("%w: fixture %d", ErrFixtureNotPending, fixture)
}

func readyRefs(ready FixtureRoundReady) (out []competitions.RoundRef) {
	for _, r := range ready.Rounds {
		out = append(out, r.Round)
	}
	return out
}

// matchRandom is the fixture's dedicated random stream.
func matchRandom(w *World, fixture ids.FixtureID) matches.RandomState {
	return matches.FixtureRandom(w.seed, w.engine.ID(), w.engine.Version(), fixture)
}

// replay starts the fixture's session and re-applies every stop: one
// Advance to the stop's minute (which must reach it exactly, as PlayMatch
// recorded it), then its commands, then a no-op Advance that delivers their
// events. It never touches world state.
func (w *World) replay(p plannedMatch, stops []LiveStop) (*replayed, error) {
	rs := matchRandom(w, p.fixture.ID)
	session, err := w.engine.Start(&p.input, rs)
	if err != nil {
		return nil, fmt.Errorf("app: start fixture %d: %w", p.fixture.ID, err)
	}
	r := &replayed{plan: p, session: session}
	advance := func(minute uint16) error {
		if err := session.Advance(matches.AdvanceRequest{ToMinute: minute}, &r.step); err != nil {
			return err
		}
		r.events = append(r.events, r.step.Events...)
		return nil
	}
	for i, st := range stops {
		if st.Minute == 0 || (i > 0 && st.Minute <= stops[i-1].Minute) {
			return nil, fmt.Errorf("app: live stop minutes %d not increasing", st.Minute)
		}
		if err := advance(st.Minute); err != nil {
			return nil, err
		}
		if r.step.Position.Minute != st.Minute {
			return nil, fmt.Errorf("app: live stop at minute %d reached %d", st.Minute, r.step.Position.Minute)
		}
		for _, c := range st.Commands {
			if err := session.Apply(c); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrMatchDecision, err)
			}
		}
		if len(st.Commands) > 0 {
			if err := advance(st.Minute); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

// view builds the live view of a replayed match for the manager's team.
func (w *World) view(r *replayed) LiveMatch {
	team, _ := w.userTeam()
	f := r.plan.fixture
	side := matches.Home
	if f.Away == team {
		side = matches.Away
	}
	l := LiveMatch{
		Fixture: f.ID, Side: side, Home: w.teamLabel(f.Home), Away: w.teamLabel(f.Away),
		Position: r.step.Position, Status: r.step.Status, View: r.step.View,
		Events: slices.Clone(r.events), Rules: r.plan.input.Rules,
		Teams: [2]matches.TeamInput{r.plan.input.Home, r.plan.input.Away},
	}
	return l.clone()
}

// LiveMatch returns the manager's match in progress, if any. Read-only.
func (w *World) LiveMatch() (LiveMatch, bool) {
	if w.live == nil {
		return LiveMatch{}, false
	}
	p, err := w.livePlan(w.live.fixture)
	if err != nil {
		return LiveMatch{}, false
	}
	r, err := w.replay(p, w.live.stops)
	if err != nil {
		return LiveMatch{}, false
	}
	return w.view(r), true
}

// PlayMatch starts or continues the manager's match live. The fixture must
// be the user club's in the pending batch, and ToMinute after the current
// minute and at most full time. The recorded stop is the minute actually
// reached (45 when crossing half time). Retries follow the other commands:
// a recorded ID with the same content returns the recorded result.
func (w *World) PlayMatch(cmd PlayMatch) (MatchStepped, error) {
	if !validCommandID(cmd.ID) {
		return MatchStepped{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		if rec.play == nil || rec.play.Request != cmd {
			return MatchStepped{}, fmt.Errorf("%w: command %d", ErrCommandIDReused, cmd.ID)
		}
		return rec.play.clone().Result, nil
	}
	if cmd.ExpectedRevision != w.revision {
		return MatchStepped{}, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, cmd.ExpectedRevision, w.revision)
	}
	if _, _, err := w.pendingUserFixture(cmd.Fixture); err != nil {
		return MatchStepped{}, err
	}
	var stops []LiveStop
	if w.live != nil {
		if w.live.fixture != cmd.Fixture {
			return MatchStepped{}, fmt.Errorf("%w: fixture %d", ErrMatchInProgress, w.live.fixture)
		}
		stops = cloneStops(w.live.stops)
	}
	p, err := w.livePlan(cmd.Fixture)
	if err != nil {
		return MatchStepped{}, err
	}
	r, err := w.replay(p, stops)
	if err != nil {
		return MatchStepped{}, err
	}
	if cmd.ToMinute <= r.step.Position.Minute || cmd.ToMinute > matches.RegulationMinutes {
		return MatchStepped{}, fmt.Errorf("%w: play to minute %d from %d", ErrInvalidCommand, cmd.ToMinute, r.step.Position.Minute)
	}
	if err := r.session.Advance(matches.AdvanceRequest{ToMinute: cmd.ToMinute}, &r.step); err != nil {
		return MatchStepped{}, fmt.Errorf("app: play fixture %d: %w", cmd.Fixture, err)
	}
	r.events = append(r.events, r.step.Events...)
	stops = append(stops, LiveStop{Minute: r.step.Position.Minute})

	// Committed. Nothing below can fail.
	w.live = &liveState{fixture: cmd.Fixture, stops: stops}
	w.revision++
	res := MatchStepped{Command: cmd.ID, Revision: w.revision, Live: w.view(r)}
	rec := PlayMatchRecord{Request: cmd, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{play: &rec}
	return res, nil
}

// MatchDecision applies a substitution or mentality change for the
// manager's side at the live match's current minute. The engine validates
// it (substitutions left, players on the pitch or bench, goalkeepers
// like-for-like); a rejected decision changes nothing.
func (w *World) MatchDecision(cmd MatchDecision) (MatchStepped, error) {
	if !validCommandID(cmd.ID) {
		return MatchStepped{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		if rec.decision == nil || rec.decision.Request != cmd {
			return MatchStepped{}, fmt.Errorf("%w: command %d", ErrCommandIDReused, cmd.ID)
		}
		return rec.decision.clone().Result, nil
	}
	if cmd.ExpectedRevision != w.revision {
		return MatchStepped{}, fmt.Errorf("%w: expected %d, world is at %d", ErrStaleRevision, cmd.ExpectedRevision, w.revision)
	}
	if w.live == nil || w.live.fixture != cmd.Fixture {
		return MatchStepped{}, fmt.Errorf("%w: fixture %d", ErrNoLiveMatch, cmd.Fixture)
	}
	p, err := w.livePlan(cmd.Fixture)
	if err != nil {
		return MatchStepped{}, err
	}
	stops := cloneStops(w.live.stops)
	r, err := w.replay(p, stops)
	if err != nil {
		return MatchStepped{}, err
	}
	if side := w.view(r).Side; cmd.Command.Side != side {
		return MatchStepped{}, fmt.Errorf("%w: side %s, you manage the %s side", ErrMatchDecision, cmd.Command.Side, side)
	}
	if err := r.session.Apply(cmd.Command); err != nil {
		return MatchStepped{}, fmt.Errorf("%w: %v", ErrMatchDecision, err)
	}
	minute := r.step.Position.Minute
	if err := r.session.Advance(matches.AdvanceRequest{ToMinute: minute}, &r.step); err != nil {
		return MatchStepped{}, fmt.Errorf("app: fixture %d: %w", cmd.Fixture, err)
	}
	r.events = append(r.events, r.step.Events...)
	last := &stops[len(stops)-1]
	last.Commands = append(last.Commands, cmd.Command)

	// Committed. Nothing below can fail.
	w.live.stops = stops
	w.revision++
	res := MatchStepped{Command: cmd.ID, Revision: w.revision, Live: w.view(r)}
	rec := DecisionRecord{Request: cmd, Result: res}.clone()
	w.commands[cmd.ID] = commandRecord{decision: &rec}
	return res, nil
}

// validateLive checks a match in progress: it is the user club's fixture in
// the pending batch, and its stops replay exactly, with every decision for
// the manager's side.
func (w *World) validateLive() []error {
	if w.live == nil {
		return nil
	}
	fail := func(format string, args ...any) []error {
		return []error{fmt.Errorf("app: live match: "+format, args...)}
	}
	if _, _, err := w.pendingUserFixture(w.live.fixture); err != nil {
		return fail("%v", err)
	}
	if len(w.live.stops) == 0 {
		return fail("no stops")
	}
	p, err := w.livePlan(w.live.fixture)
	if err != nil {
		return fail("%v", err)
	}
	r, err := w.replay(p, w.live.stops)
	if err != nil {
		return fail("%v", err)
	}
	side := w.view(r).Side
	for _, st := range w.live.stops {
		for _, c := range st.Commands {
			if c.Side != side {
				return fail("decision %+v for the other side", c)
			}
		}
	}
	return nil
}
