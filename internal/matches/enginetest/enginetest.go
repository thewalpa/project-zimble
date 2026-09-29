// Package enginetest is the match-engine contract suite. Every engine in
// internal/matches/* runs Contract from its tests, so replacing one engine
// with another keeps the behavior callers rely on: legal participant
// minutes, consistent scores and events, valid substitutions, buffer
// ownership and a deterministic replay from the same RandomState.
//
// The suite checks the contract only. Model behavior (goal rates, trends)
// belongs in each engine's own tests.
package enginetest

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// Seed is the world seed of every stream the suite derives.
const Seed random.Seed = 42

// Config sizes the suite for an engine's speed.
type Config struct {
	// Matches is how many fixtures the invariant test plays (default 100).
	Matches int
	// Knockouts is how many league/cup pairs the knockout test plays
	// (default 200); about one in five should end level.
	Knockouts int
}

// Random returns the stream the suite uses for fixture f.
func Random(e matches.Engine, f ids.FixtureID) matches.RandomState {
	return matches.FixtureRandom(Seed, e.ID(), e.Version(), f)
}

// Starters: GK, 4 DF, 4 MF, 2 FW. Bench: GK, DF, MF, FW, DF, MF, FW.
var (
	starterRoles = []matches.Role{1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4}
	benchRoles   = []matches.Role{1, 2, 3, 4, 2, 3, 4}
)

func clamp(v int) uint8 { return uint8(min(max(v, matches.MinRating), matches.MaxRating)) }

// Ratings gives a role-appropriate profile around strength (1..100),
// varied by i.
func Ratings(role matches.Role, strength, i int) matches.Ratings {
	v := (i%5 - 2) * 5
	low := clamp(strength/3 + v)
	r := matches.Ratings{
		Goalkeeping: low, Defending: clamp(strength + v), Passing: clamp(strength - v),
		Finishing: clamp(strength + v), Pace: clamp(strength - v), Stamina: clamp(strength + 2*v),
		Dribbling: clamp(strength - v), Heading: clamp(strength + v), Strength: clamp(strength + v),
		Acceleration: clamp(strength - v), Positioning: clamp(strength + v),
	}
	switch role {
	case matches.Goalkeeper:
		r.Goalkeeping, r.Finishing, r.Dribbling = clamp(strength+v), low, low
	case matches.Defender:
		r.Finishing, r.Dribbling = clamp(strength-20+v), clamp(strength-10-v)
	case matches.Forward:
		r.Defending, r.Heading = clamp(strength-20+v), clamp(strength-10+v)
	}
	return r
}

// Team returns a full squad for team, with player IDs from firstPlayer:
// eleven starters in slot order, then seven substitutes.
func Team(team ids.TeamID, firstPlayer ids.PlayerID, strength int, m matches.Mentality) matches.TeamInput {
	t := matches.TeamInput{Team: team, Tactics: matches.Tactics{Mentality: m}}
	id := firstPlayer
	for i, role := range starterRoles {
		t.Starters = append(t.Starters, matches.PlayerInput{Player: id, Role: role, Ratings: Ratings(role, strength, i), Condition: matches.MaxCondition})
		id++
	}
	for i, role := range benchRoles {
		t.Bench = append(t.Bench, matches.PlayerInput{Player: id, Role: role, Ratings: Ratings(role, strength, i+11), Condition: matches.MaxCondition})
		id++
	}
	return t
}

// Input returns a valid match: home team 1 (players 101-118), away team 2
// (players 201-218), three substitutions from a bench of seven.
func Input(fixture ids.FixtureID, homeStrength, awayStrength int) *matches.MatchInput {
	return &matches.MatchInput{
		Match: fixture,
		Home:  Team(1, 101, homeStrength, matches.Balanced),
		Away:  Team(2, 201, awayStrength, matches.Balanced),
		Rules: matches.Rules{MaxSubstitutions: 3, MaxBench: 7},
	}
}

// Sub and SetMentality build commands.
func Sub(side matches.Side, out, in ids.PlayerID) matches.MatchCommand {
	return matches.MatchCommand{Kind: matches.CommandSubstitute, Side: side, Out: out, In: in}
}

func SetMentality(side matches.Side, m matches.Mentality) matches.MatchCommand {
	return matches.MatchCommand{Kind: matches.CommandSetMentality, Side: side, Mentality: m}
}

// TimedCommand is a command applied when the session stops at Minute
// (and, for minute 45, at half time).
type TimedCommand struct {
	Minute uint16
	Cmd    matches.MatchCommand
}

// StandardCommands exercises both command kinds at a mid-half stop and at
// half time.
var StandardCommands = []TimedCommand{
	{30, SetMentality(matches.Home, matches.Attacking)},
	{45, Sub(matches.Away, 202, 213)}, // DF for DF at half time
	{45, Sub(matches.Away, 211, 215)}, // FW for FW
	{70, Sub(matches.Home, 101, 112)}, // GK for GK
	{70, SetMentality(matches.Away, matches.Defensive)},
}

// FullMatch stops at half time, then at full time.
var FullMatch = []uint16{90, 90}

// Start starts a session for in with the suite's stream for in.Match.
func Start(t testing.TB, e matches.Engine, in *matches.MatchInput) matches.MatchSession {
	t.Helper()
	s, err := e.Start(in, Random(e, in.Match))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func advance(t testing.TB, s matches.MatchSession, req matches.AdvanceRequest, dst *matches.MatchStepResult) {
	t.Helper()
	if err := s.Advance(req, dst); err != nil {
		t.Fatalf("Advance(%+v): %v", req, err)
	}
}

// Play calls Advance once per stop, applies commands whose minute matches
// the position reached, and returns every event plus a copy of the final
// step. A stop beyond half time halts at 45 first, so plans must still
// reach full time.
func Play(t testing.TB, s matches.MatchSession, stops []uint16, cmds []TimedCommand) ([]matches.MatchEvent, matches.MatchStepResult) {
	t.Helper()
	var dst matches.MatchStepResult
	var events []matches.MatchEvent
	applied := map[int]bool{}
	for _, stop := range stops {
		advance(t, s, matches.AdvanceRequest{ToMinute: stop}, &dst)
		events = append(events, dst.Events...)
		for i, c := range cmds {
			if !applied[i] && c.Minute == dst.Position.Minute {
				if err := s.Apply(c.Cmd); err != nil {
					t.Fatalf("Apply at %d: %v", c.Minute, err)
				}
				applied[i] = true
			}
		}
	}
	if dst.Status != matches.MatchFinished {
		t.Fatalf("play ended at %+v, not full time", dst.Position)
	}
	for i := range cmds {
		if !applied[i] {
			t.Fatalf("command %d at minute %d was never applied", i, cmds[i].Minute)
		}
	}
	return events, Clone(dst)
}

// Clone deep-copies a step result.
func Clone(r matches.MatchStepResult) matches.MatchStepResult {
	r.Events = slices.Clone(r.Events)
	r.Frames = slices.Clone(r.Frames)
	r.Outcome.Goals = slices.Clone(r.Outcome.Goals)
	r.Outcome.Participants = slices.Clone(r.Outcome.Participants)
	return r
}

// Outcome plays a match to full time and returns its outcome.
func Outcome(t testing.TB, e matches.Engine, in *matches.MatchInput) matches.MatchOutcome {
	t.Helper()
	_, res := Play(t, Start(t, e, in), FullMatch, nil)
	return res.Outcome
}

// CheckOutcome verifies the invariants every completed regulation match
// satisfies: header, event log, score and participation.
func CheckOutcome(t testing.TB, e matches.Engine, in *matches.MatchInput, events []matches.MatchEvent, final matches.MatchStepResult) {
	t.Helper()
	o := final.Outcome
	if final.Status != matches.MatchFinished || final.Stop != matches.StopFullTime ||
		final.Position != (matches.MatchPosition{Period: matches.FullTime, Minute: 90}) {
		t.Fatalf("final step %+v", final.Position)
	}
	if o.Status != matches.ResultCompleted || o.Resolution != matches.ResolutionRegulation ||
		o.Match != in.Match || o.EngineID != e.ID() || o.EngineVersion != e.Version() {
		t.Fatalf("outcome header %+v", o)
	}
	checkEvents(t, events, o.Goals)
	CheckScoreAndParticipation(t, in, final)
}

func checkEvents(t testing.TB, events []matches.MatchEvent, goals []matches.Goal) {
	t.Helper()
	// Seq is 1..n with no gaps or repeats across Advance calls.
	for i, e := range events {
		if e.Seq != uint32(i+1) {
			t.Fatalf("event %d has seq %d: duplicated or missing events", i, e.Seq)
		}
		if i > 0 && e.Minute < events[i-1].Minute {
			t.Fatalf("event %d out of minute order", i)
		}
	}
	var goalEvents []matches.Goal
	var periodEnds []uint16
	for _, e := range events {
		switch e.Kind {
		case matches.EventGoal:
			goalEvents = append(goalEvents, matches.Goal{Minute: e.Minute, Side: e.Side, Scorer: e.Player})
		case matches.EventPeriodEnd:
			periodEnds = append(periodEnds, e.Minute)
		}
	}
	if !slices.Equal(goalEvents, goals) {
		t.Fatalf("goal events %v != outcome goals %v", goalEvents, goals)
	}
	if !slices.Equal(periodEnds, []uint16{45, 90}) {
		t.Fatalf("period ends at %v", periodEnds)
	}
}

// CheckScoreAndParticipation verifies that score, goals and participation
// agree in a completed outcome.
func CheckScoreAndParticipation(t testing.TB, in *matches.MatchInput, final matches.MatchStepResult) {
	t.Helper()
	o := final.Outcome
	var perSide [2]uint16
	for _, g := range o.Goals {
		perSide[g.Side.Index()]++
	}
	if perSide != o.Score || final.View.Score != o.Score {
		t.Fatalf("score %v, view %v, goals per side %v", o.Score, final.View.Score, perSide)
	}

	// 990 minutes per side, contiguous intervals, starters all present,
	// every participant from that side's squad.
	squad := map[ids.PlayerID]matches.Side{}
	for _, side := range []matches.Side{matches.Home, matches.Away} {
		tm := in.Team(side)
		for _, p := range append(slices.Clone(tm.Starters), tm.Bench...) {
			squad[p.Player] = side
		}
	}
	var minutes [2]int
	interval := map[ids.PlayerID]matches.Participation{}
	for _, p := range o.Participants {
		if squad[p.Player] != p.Side || p.OnMinute > p.OffMinute || p.OffMinute > 90 {
			t.Fatalf("bad participation %+v", p)
		}
		if _, dup := interval[p.Player]; dup {
			t.Fatalf("player %d participates twice", p.Player)
		}
		if p.Started != (p.OnMinute == 0) {
			t.Fatalf("participation %+v: Started disagrees with OnMinute", p)
		}
		interval[p.Player] = p
		minutes[p.Side.Index()] += int(p.Minutes())
	}
	if minutes != [2]int{990, 990} {
		t.Fatalf("participant minutes per side = %v, want 990 each", minutes)
	}
	for _, side := range []matches.Side{matches.Home, matches.Away} {
		for _, p := range in.Team(side).Starters {
			if !interval[p.Player].Started {
				t.Fatalf("starter %d missing from participants", p.Player)
			}
		}
	}
	for _, g := range o.Goals {
		p, ok := interval[g.Scorer]
		if !ok || p.Side != g.Side || g.Minute <= p.OnMinute || g.Minute > p.OffMinute {
			t.Fatalf("goal %+v scored by a player not on the pitch (%+v)", g, p)
		}
	}
}

// Contract runs the whole contract suite against e.
func Contract(t *testing.T, e matches.Engine, cfg Config) {
	if cfg.Matches == 0 {
		cfg.Matches = 100
	}
	if cfg.Knockouts == 0 {
		cfg.Knockouts = 200
	}
	c := contract{e: e, cfg: cfg}
	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"CompleteMatchInvariants", c.completeMatchInvariants},
		{"SameInputSeedAndCommandsGiveSameMatch", c.deterministic},
		{"AdvanceChunkingDoesNotChangeTheMatch", c.chunking},
		{"HalfTimeIsAMandatoryStop", c.halfTime},
		{"SubstitutionUpdatesParticipationAndView", c.substitution},
		{"IllegalCommandsAreRejected", c.illegalCommands},
		{"RejectedCommandsDoNotAffectTheMatch", c.rejectedCommands},
		{"FinishedSessionBehavior", c.finished},
		{"InvalidAdvanceLeavesOutputUnchanged", c.invalidAdvance},
		{"StartValidatesInput", c.startValidates},
		{"SessionDoesNotRetainInput", c.inputNotRetained},
		{"AdvanceOutputBufferOwnership", c.bufferOwnership},
		{"CommandEventsDeliveredOnce", c.commandEventsOnce},
		{"Checkpoints", c.checkpoints},
		{"Knockouts", c.knockouts},
		{"Frames", c.frames},
	} {
		t.Run(test.name, test.run)
	}
}

type contract struct {
	e   matches.Engine
	cfg Config
}

func (c contract) completeMatchInvariants(t *testing.T) {
	for fixture := ids.FixtureID(1); fixture <= ids.FixtureID(c.cfg.Matches); fixture++ {
		in := Input(fixture, 40+5*(int(fixture)%9), 80-5*(int(fixture)%9))
		cmds := StandardCommands
		if fixture%2 == 0 {
			cmds = nil
		}
		events, final := Play(t, Start(t, c.e, in), []uint16{30, 45, 70, 90}, cmds)
		CheckOutcome(t, c.e, in, events, final)
	}
}

func (c contract) deterministic(t *testing.T) {
	e1, f1 := Play(t, Start(t, c.e, Input(7, 60, 60)), []uint16{30, 45, 70, 90}, StandardCommands)
	e2, f2 := Play(t, Start(t, c.e, Input(7, 60, 60)), []uint16{30, 45, 70, 90}, StandardCommands)
	if !reflect.DeepEqual(e1, e2) || !reflect.DeepEqual(f1, f2) {
		t.Fatal("identical input, seed and commands produced different matches")
	}
	// A different fixture stream varies the match.
	differs := false
	for fixture := ids.FixtureID(8); fixture < 20 && !differs; fixture++ {
		s, err := c.e.Start(Input(7, 60, 60), Random(c.e, fixture))
		if err != nil {
			t.Fatal(err)
		}
		e3, _ := Play(t, s, FullMatch, nil)
		differs = !reflect.DeepEqual(e3, e1)
	}
	if !differs {
		t.Fatal("per-fixture random streams did not vary the match")
	}
}

func (c contract) chunking(t *testing.T) {
	everyMinute := make([]uint16, 0, 90)
	for m := uint16(1); m <= 90; m++ {
		everyMinute = append(everyMinute, m)
	}
	plans := [][]uint16{
		{30, 45, 70, 90},
		everyMinute,
		{0, 5, 30, 30, 31, 45, 45, 46, 60, 70, 88, 90},
		{30, 90, 70, 90}, // the first 90 halts at half time
	}
	var wantEvents []matches.MatchEvent
	var want matches.MatchStepResult
	for i, plan := range plans {
		events, final := Play(t, Start(t, c.e, Input(3, 65, 55)), plan, StandardCommands)
		// The last step of a chunked run only holds its own events.
		final.Events = nil
		if i == 0 {
			wantEvents, want = events, final
			continue
		}
		if !reflect.DeepEqual(events, wantEvents) || !reflect.DeepEqual(final, want) {
			t.Fatalf("plan %v produced a different match", plan)
		}
	}
}

func (c contract) halfTime(t *testing.T) {
	s := Start(t, c.e, Input(1, 60, 60))
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	if dst.Position != (matches.MatchPosition{Period: matches.HalfTime, Minute: 45}) ||
		dst.Status != matches.MatchDecisionRequired || dst.Stop != matches.StopHalfTime {
		t.Fatalf("Advance(90) from kickoff stopped at %+v %d %d", dst.Position, dst.Status, dst.Stop)
	}
	if last := dst.Events[len(dst.Events)-1]; last.Kind != matches.EventPeriodEnd || last.Period != matches.FirstHalf {
		t.Fatalf("last first-half event %+v", last)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 45}, &dst)
	if dst.Status != matches.MatchDecisionRequired || len(dst.Events) != 0 {
		t.Fatalf("Advance(45) at half time: %+v", dst)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 60}, &dst)
	if dst.Position != (matches.MatchPosition{Period: matches.SecondHalf, Minute: 60}) || dst.Status != matches.MatchRunning {
		t.Fatalf("second half position %+v", dst.Position)
	}
	if dst.Outcome.Status != matches.ResultPending || len(dst.Outcome.Participants) != 0 || dst.Outcome.Score != [2]uint16{} {
		t.Fatalf("unfinished session exposed a result: %+v", dst.Outcome)
	}
}

func (c contract) substitution(t *testing.T) {
	in := Input(4, 60, 60)
	s := Start(t, c.e, in)
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 60}, &dst) // stops at half time
	advance(t, s, matches.AdvanceRequest{ToMinute: 60}, &dst)
	if err := s.Apply(Sub(matches.Home, 110, 115)); err != nil { // FW 110 off, FW 115 on
		t.Fatal(err)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 61}, &dst)
	if e := dst.Events[0]; e.Kind != matches.EventSubstitution || e.Minute != 60 || e.Player != 115 || e.Other != 110 {
		t.Fatalf("first event after sub = %+v", e)
	}
	if dst.View.OnPitch[0][9] != 115 || dst.View.SubstitutionsUsed[0] != 1 {
		t.Fatalf("view after sub %+v", dst.View)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	var off, on matches.Participation
	for _, p := range dst.Outcome.Participants {
		switch p.Player {
		case 110:
			off = p
		case 115:
			on = p
		}
	}
	if off != (matches.Participation{Player: 110, Side: matches.Home, Started: true, OnMinute: 0, OffMinute: 60}) ||
		on != (matches.Participation{Player: 115, Side: matches.Home, Started: false, OnMinute: 60, OffMinute: 90}) {
		t.Fatalf("participation off=%+v on=%+v", off, on)
	}
	CheckScoreAndParticipation(t, in, dst)
}

func (c contract) illegalCommands(t *testing.T) {
	s := Start(t, c.e, Input(5, 60, 60))
	reject := func(name string, cmd matches.MatchCommand) {
		t.Helper()
		if err := s.Apply(cmd); !errors.Is(err, matches.ErrInvalidCommand) {
			t.Errorf("%s: err = %v, want %v", name, err, matches.ErrInvalidCommand)
		}
	}
	reject("sub before kickoff", Sub(matches.Home, 110, 115))
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 20}, &dst)
	for _, r := range []struct {
		name string
		cmd  matches.MatchCommand
	}{
		{"invalid side", Sub(0, 110, 115)},
		{"unknown kind", matches.MatchCommand{Kind: 9, Side: matches.Home}},
		{"out on bench", Sub(matches.Home, 113, 115)},
		{"in already playing", Sub(matches.Home, 110, 111)},
		{"unknown player", Sub(matches.Home, 110, 999)},
		{"opponent's player", Sub(matches.Home, 110, 215)},
		{"outfield for keeper", Sub(matches.Home, 101, 115)},
		{"keeper for outfield", Sub(matches.Home, 110, 112)},
		{"same mentality", SetMentality(matches.Home, matches.Balanced)},
		{"invalid mentality", SetMentality(matches.Home, 7)},
	} {
		reject(r.name, r.cmd)
	}
	if err := s.Apply(Sub(matches.Home, 110, 115)); err != nil {
		t.Fatal(err)
	}
	reject("player subbed off returns", Sub(matches.Home, 111, 110))
	reject("substitute brought on twice", Sub(matches.Home, 111, 115))
	reject("subbed-off player taken off again", Sub(matches.Home, 110, 116))
	for _, cmd := range []matches.MatchCommand{Sub(matches.Home, 102, 113), Sub(matches.Home, 106, 114)} {
		if err := s.Apply(cmd); err != nil {
			t.Fatal(err)
		}
	}
	reject("fourth substitution", Sub(matches.Home, 103, 116))
	if err := s.Apply(Sub(matches.Away, 210, 215)); err != nil {
		t.Fatalf("away substitutions are independent: %v", err)
	}
}

// A session that rejected commands plays out exactly like one that never
// received them.
func (c contract) rejectedCommands(t *testing.T) {
	clean := Start(t, c.e, Input(6, 60, 60))
	noisy := Start(t, c.e, Input(6, 60, 60))
	var a, b matches.MatchStepResult
	for _, stop := range []uint16{20, 45, 90, 90} {
		advance(t, clean, matches.AdvanceRequest{ToMinute: stop}, &a)
		advance(t, noisy, matches.AdvanceRequest{ToMinute: stop}, &b)
		_ = noisy.Apply(Sub(matches.Home, 101, 115))
		_ = noisy.Apply(SetMentality(matches.Away, matches.Balanced))
		if !reflect.DeepEqual(Clone(a), Clone(b)) {
			t.Fatalf("rejected commands changed the match at %d", stop)
		}
	}
}

func (c contract) finished(t *testing.T) {
	s := Start(t, c.e, Input(9, 60, 60))
	_, final := Play(t, s, FullMatch, nil)
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	if dst.Status != matches.MatchFinished || len(dst.Events) != 0 {
		t.Fatalf("Advance after full time: status %d, %d events", dst.Status, len(dst.Events))
	}
	final.Events, dst.Events = nil, nil
	if !reflect.DeepEqual(Clone(dst), final) {
		t.Fatal("Advance after full time returned a different final state")
	}
	if err := s.Apply(SetMentality(matches.Home, matches.Attacking)); !errors.Is(err, matches.ErrMatchFinished) {
		t.Fatalf("Apply after full time: %v", err)
	}
	if err := s.Advance(matches.AdvanceRequest{ToMinute: 89}, &dst); !errors.Is(err, matches.ErrInvalidRequest) {
		t.Fatalf("Advance(89) after full time: %v", err)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	dst.Events = nil
	if !reflect.DeepEqual(Clone(dst), final) {
		t.Fatal("rejected calls changed the finished session")
	}
}

func (c contract) invalidAdvance(t *testing.T) {
	s := Start(t, c.e, Input(10, 60, 60))
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 30}, &dst)
	saved := Clone(dst)
	for _, to := range []uint16{29, 91, 1000} {
		if err := s.Advance(matches.AdvanceRequest{ToMinute: to}, &dst); !errors.Is(err, matches.ErrInvalidRequest) {
			t.Fatalf("Advance(%d): %v", to, err)
		}
	}
	if err := s.Advance(matches.AdvanceRequest{ToMinute: 40}, nil); !errors.Is(err, matches.ErrInvalidRequest) {
		t.Fatalf("nil dst: %v", err)
	}
	if !reflect.DeepEqual(Clone(dst), saved) {
		t.Fatal("rejected Advance changed the output")
	}
	// The session continues exactly like one that never saw the calls.
	_, got := Play(t, s, FullMatch, nil)
	clean := Start(t, c.e, Input(10, 60, 60))
	advance(t, clean, matches.AdvanceRequest{ToMinute: 30}, &dst)
	_, want := Play(t, clean, FullMatch, nil)
	if !reflect.DeepEqual(got.Outcome, want.Outcome) {
		t.Fatal("rejected Advance calls changed the match")
	}
}

func (c contract) startValidates(t *testing.T) {
	cases := map[string]func(*matches.MatchInput){
		"zero match":        func(in *matches.MatchInput) { in.Match = 0 },
		"same teams":        func(in *matches.MatchInput) { in.Away.Team = in.Home.Team },
		"zero team":         func(in *matches.MatchInput) { in.Home.Team = 0 },
		"ten starters":      func(in *matches.MatchInput) { in.Home.Starters = in.Home.Starters[:10] },
		"no keeper":         func(in *matches.MatchInput) { in.Home.Starters[0].Role = matches.Defender },
		"two keepers":       func(in *matches.MatchInput) { in.Home.Starters[1].Role = matches.Goalkeeper },
		"bench over rules":  func(in *matches.MatchInput) { in.Rules.MaxBench = 5 },
		"subs over bench":   func(in *matches.MatchInput) { in.Rules = matches.Rules{MaxSubstitutions: 8, MaxBench: 7} },
		"duplicate in team": func(in *matches.MatchInput) { in.Home.Bench[0].Player = in.Home.Starters[3].Player },
		"duplicate across":  func(in *matches.MatchInput) { in.Away.Starters[4].Player = in.Home.Starters[4].Player },
		"zero player":       func(in *matches.MatchInput) { in.Away.Bench[2].Player = 0 },
		"rating 0":          func(in *matches.MatchInput) { in.Home.Starters[5].Ratings.Pace = 0 },
		"rating 101":        func(in *matches.MatchInput) { in.Away.Bench[6].Ratings.Stamina = 101 },
		"dribbling 0":       func(in *matches.MatchInput) { in.Home.Starters[9].Ratings.Dribbling = 0 },
		"positioning 101":   func(in *matches.MatchInput) { in.Away.Starters[2].Ratings.Positioning = 101 },
		"condition 0":       func(in *matches.MatchInput) { in.Home.Starters[2].Condition = 0 },
		"condition 101":     func(in *matches.MatchInput) { in.Away.Starters[2].Condition = 101 },
		"invalid role":      func(in *matches.MatchInput) { in.Home.Starters[5].Role = 9 },
		"invalid mentality": func(in *matches.MatchInput) { in.Away.Tactics.Mentality = 0 },
	}
	for name, mutate := range cases {
		in := Input(1, 60, 60)
		mutate(in)
		if _, err := c.e.Start(in, Random(c.e, 1)); !errors.Is(err, matches.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := c.e.Start(nil, Random(c.e, 1)); !errors.Is(err, matches.ErrInvalidInput) {
		t.Errorf("nil input: %v", err)
	}
	for name, mutate := range map[string]func(*matches.RandomState){
		"algorithm":  func(r *matches.RandomState) { r.Algorithm = 2 },
		"version":    func(r *matches.RandomState) { r.Version++ },
		"extra word": func(r *matches.RandomState) { r.Words[3] = 1 },
	} {
		r := Random(c.e, 1)
		mutate(&r)
		if _, err := c.e.Start(Input(1, 60, 60), r); !errors.Is(err, matches.ErrInvalidInput) {
			t.Errorf("random state %s: err = %v", name, err)
		}
	}
}

// Start copies its input: mutating the caller's input afterwards changes
// nothing, and Start itself does not modify the input.
func (c contract) inputNotRetained(t *testing.T) {
	in := Input(11, 60, 60)
	pristine := Input(11, 60, 60)
	s := Start(t, c.e, in)
	if !reflect.DeepEqual(in, pristine) {
		t.Fatal("Start modified its input")
	}
	for i := range in.Home.Starters {
		in.Home.Starters[i].Ratings = Ratings(matches.Midfielder, 1, 0)
		in.Away.Starters[i].Player += 1000
	}
	in.Home.Bench[0].Role = matches.Forward
	in.Away.Tactics.Mentality = matches.Attacking
	in.Home.Starters = nil

	stops := []uint16{30, 45, 70, 90}
	got, gotFinal := Play(t, s, stops, StandardCommands)
	want, wantFinal := Play(t, Start(t, c.e, pristine), stops, StandardCommands)
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotFinal, wantFinal) {
		t.Fatal("mutating the input after Start changed the match")
	}
}

// Advance reuses dst's buffers, replaces rather than accumulates their
// contents, and never aliases session memory.
func (c contract) bufferOwnership(t *testing.T) {
	dst := matches.MatchStepResult{
		Events:  make([]matches.MatchEvent, 0, 64),
		Outcome: matches.MatchOutcome{Goals: make([]matches.Goal, 0, 32), Participants: make([]matches.Participation, 0, 32)},
	}
	eventsBuf, goalsBuf, partsBuf := &dst.Events[:1][0], &dst.Outcome.Goals[:1][0], &dst.Outcome.Participants[:1][0]

	s := Start(t, c.e, Input(12, 60, 60))
	advance(t, s, matches.AdvanceRequest{ToMinute: 45}, &dst)
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	final := Clone(dst)
	if len(dst.Events) > 0 && &dst.Events[0] != eventsBuf {
		t.Fatal("Events buffer was reallocated despite sufficient capacity")
	}
	if &dst.Outcome.Participants[0] != partsBuf || (len(dst.Outcome.Goals) > 0 && &dst.Outcome.Goals[0] != goalsBuf) {
		t.Fatal("outcome buffers were reallocated despite sufficient capacity")
	}

	// Scribbling over dst must not reach the session.
	for i := range dst.Outcome.Goals {
		dst.Outcome.Goals[i] = matches.Goal{}
	}
	for i := range dst.Outcome.Participants {
		dst.Outcome.Participants[i].OffMinute = 0
	}
	dst.View.OnPitch[0][0] = 0
	advance(t, s, matches.AdvanceRequest{ToMinute: 90}, &dst)
	final.Events = nil
	again := Clone(dst)
	again.Events = nil
	if !reflect.DeepEqual(again, final) {
		t.Fatal("modifying dst changed the session's outcome")
	}

	// Reusing a dst that holds a completed outcome for a new, unfinished
	// session replaces everything: no stale result, events or participants.
	s2 := Start(t, c.e, Input(13, 60, 60))
	advance(t, s2, matches.AdvanceRequest{ToMinute: 10}, &dst)
	if dst.Outcome.Status != matches.ResultPending || len(dst.Outcome.Goals) != 0 || len(dst.Outcome.Participants) != 0 ||
		dst.Outcome.Match != 13 || dst.Outcome.Score != [2]uint16{} {
		t.Fatalf("stale outcome after reuse: %+v", dst.Outcome)
	}
	for _, e := range dst.Events {
		if e.Minute > 10 {
			t.Fatalf("stale event after reuse: %+v", e)
		}
	}
}

// Pending command events are delivered exactly once, even through no-op
// Advance calls.
func (c contract) commandEventsOnce(t *testing.T) {
	s := Start(t, c.e, Input(14, 60, 60))
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 45}, &dst)
	if err := s.Apply(Sub(matches.Home, 110, 115)); err != nil {
		t.Fatal(err)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 45}, &dst) // no minutes played
	if len(dst.Events) != 1 || dst.Events[0].Kind != matches.EventSubstitution || dst.Events[0].Minute != 45 {
		t.Fatalf("no-op Advance events = %+v", dst.Events)
	}
	advance(t, s, matches.AdvanceRequest{ToMinute: 45}, &dst)
	if len(dst.Events) != 0 {
		t.Fatalf("command event delivered twice: %+v", dst.Events)
	}
}

func (c contract) checkpoints(t *testing.T) {
	if c.e.Capabilities().Checkpoints {
		t.Skip("checkpoint round trips are not in the suite yet")
	}
	s := Start(t, c.e, Input(1, 60, 60))
	if _, err := s.Checkpoint(); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Checkpoint: %v", err)
	}
	if _, err := c.e.Restore(matches.MatchCheckpoint{EngineID: c.e.ID()}); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Restore: %v", err)
	}
}

// A knockout match always has a winner. Its 90 minutes are exactly those of
// the same match without the knockout rule; a level match adds a
// well-formed shootout.
func (c contract) knockouts(t *testing.T) {
	if !c.e.Capabilities().Penalties {
		t.Skip("engine has no penalties")
	}
	shootouts := 0
	for f := ids.FixtureID(1); f <= ids.FixtureID(c.cfg.Knockouts); f++ {
		league := Input(f, 60, 60)
		cup := Input(f, 60, 60)
		cup.Rules.Knockout = true
		a, b := Outcome(t, c.e, league), Outcome(t, c.e, cup)
		if a.Resolution != matches.ResolutionRegulation || a.Shootout != [2]uint16{} {
			t.Fatalf("fixture %d: a league match went to penalties: %+v", f, a)
		}
		if a.Score != b.Score || !slices.Equal(a.Goals, b.Goals) || !slices.Equal(a.Participants, b.Participants) {
			t.Fatalf("fixture %d: the knockout rule changed regulation play", f)
		}
		if _, ok := b.Winner(); !ok {
			t.Fatalf("fixture %d: a knockout match ended level: %+v", f, b)
		}
		if b.Score[0] != b.Score[1] {
			if b.Resolution != matches.ResolutionRegulation || b.Shootout != [2]uint16{} {
				t.Fatalf("fixture %d: a decided match has a shootout: %+v", f, b)
			}
			continue
		}
		shootouts++
		p := b.Shootout
		hi, lo := max(p[0], p[1]), min(p[0], p[1])
		// Best of five: a lead of up to three over five kicks each; beyond
		// five, sudden death wins by exactly one.
		if b.Resolution != matches.ResolutionPenalties || hi == lo || (hi > 5 && hi-lo != 1) || hi-lo > 3 {
			t.Fatalf("fixture %d: shootout %v, resolution %d", f, p, b.Resolution)
		}
	}
	if shootouts*10 < c.cfg.Knockouts {
		t.Fatalf("only %d of %d matches ended level", shootouts, c.cfg.Knockouts)
	}
}

// An engine without positional frames rejects a request for them and is
// unchanged. One with them emits one frame per instant, on the pitch, in
// clock order, and asking for them never changes the match.
func (c contract) frames(t *testing.T) {
	s := Start(t, c.e, Input(15, 60, 60))
	var dst matches.MatchStepResult
	advance(t, s, matches.AdvanceRequest{ToMinute: 10}, &dst)
	if !c.e.Capabilities().PositionalFrames {
		saved := Clone(dst)
		if err := s.Advance(matches.AdvanceRequest{ToMinute: 20, Frames: true}, &dst); !errors.Is(err, matches.ErrUnsupported) {
			t.Fatalf("Advance with frames: %v", err)
		}
		if !reflect.DeepEqual(Clone(dst), saved) {
			t.Fatal("rejected frame request changed the output")
		}
		return
	}

	withFrames := Start(t, c.e, Input(15, 60, 60))
	var events []matches.MatchEvent
	var last uint32
	count := 0
	for _, stop := range []uint16{1, 45, 46, 90} {
		advance(t, withFrames, matches.AdvanceRequest{ToMinute: stop, Frames: true}, &dst)
		events = append(events, dst.Events...)
		for _, f := range dst.Frames {
			if count > 0 && f.Millis <= last {
				t.Fatalf("frame at %d ms follows %d ms", f.Millis, last)
			}
			last = f.Millis
			count++
			onPitch := func(p matches.PitchPoint) bool {
				return p.X >= 0 && p.X <= matches.PitchLength && p.Y >= 0 && p.Y <= matches.PitchWidth
			}
			if !onPitch(f.Ball) {
				t.Fatalf("ball off the pitch at %d ms: %+v", f.Millis, f.Ball)
			}
			carried := f.Carrier == 0
			for side := range f.Players {
				for slot, p := range f.Players[side] {
					if !onPitch(p) {
						t.Fatalf("player in slot %d off the pitch at %d ms: %+v", slot, f.Millis, p)
					}
					if dst.View.OnPitch[side][slot] == f.Carrier {
						carried = true
					}
				}
			}
			if !carried {
				t.Fatalf("carrier %d is not on the pitch at %d ms", f.Carrier, f.Millis)
			}
		}
	}
	if last != matches.RegulationMinutes*60_000 || count == 0 {
		t.Fatalf("%d frames ending at %d ms, want the last at full time", count, last)
	}
	plainEvents, plain := Play(t, Start(t, c.e, Input(15, 60, 60)), []uint16{1, 45, 46, 90}, nil)
	if !reflect.DeepEqual(events, plainEvents) || !reflect.DeepEqual(dst.Outcome, plain.Outcome) {
		t.Fatal("asking for frames changed the match")
	}
}
