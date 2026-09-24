package simple

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

var fullMatch = []uint16{90, 90} // stops at half time, then full time

// standardCommands exercises both command kinds at a mid-half stop and at
// half time.
var standardCommands = []timedCommand{
	{30, mentality(matches.Home, matches.Attacking)},
	{45, sub(matches.Away, 202, 213)}, // DF for DF at half time
	{45, sub(matches.Away, 211, 215)}, // FW for FW
	{70, sub(matches.Home, 101, 112)}, // GK for GK
	{70, mentality(matches.Away, matches.Defensive)},
}

// checkOutcome verifies the invariants every completed match must satisfy.
func checkOutcome(t *testing.T, in *matches.MatchInput, events []matches.MatchEvent, final matches.MatchStepResult) {
	t.Helper()
	o := final.Outcome
	if final.Status != matches.MatchFinished || final.Stop != matches.StopFullTime ||
		final.Position != (matches.MatchPosition{Period: matches.FullTime, Minute: 90}) {
		t.Fatalf("final step %+v", final)
	}
	if o.Status != matches.ResultCompleted || o.Resolution != matches.ResolutionRegulation ||
		o.Match != in.Match || o.EngineID != EngineID || o.EngineVersion != ModelVersion {
		t.Fatalf("outcome header %+v", o)
	}

	checkEvents(t, events, o.Goals)
	checkScoreAndParticipation(t, in, final)
}

// checkEvents verifies the full event log of a match.
func checkEvents(t *testing.T, events []matches.MatchEvent, goals []matches.Goal) {
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

	// Goal events, outcome goals and score agree.
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
	if !reflect.DeepEqual(periodEnds, []uint16{45, 90}) {
		t.Fatalf("period ends at %v", periodEnds)
	}
}

// checkScoreAndParticipation verifies score, goals and participation agree.
func checkScoreAndParticipation(t *testing.T, in *matches.MatchInput, final matches.MatchStepResult) {
	t.Helper()
	o := final.Outcome
	var perSide [2]uint16
	for _, g := range o.Goals {
		perSide[g.Side.Index()]++
	}
	if perSide != o.Score || final.View.Score != o.Score {
		t.Fatalf("score %v, view %v, goals per side %v", o.Score, final.View.Score, perSide)
	}

	// Participation: 990 minutes per side, contiguous intervals, starters
	// all present, every participant from that side's squad.
	squad := map[ids.PlayerID]matches.Side{}
	for _, side := range []matches.Side{matches.Home, matches.Away} {
		tm := in.Team(side)
		for _, p := range append(append([]matches.PlayerInput(nil), tm.Starters...), tm.Bench...) {
			squad[p.Player] = side
		}
	}
	var minutes [2]int
	started := map[ids.PlayerID]bool{}
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
		if p.Started {
			started[p.Player] = true
		}
	}
	if minutes != [2]int{990, 990} {
		t.Fatalf("participant minutes per side = %v, want 990 each", minutes)
	}
	for _, side := range []matches.Side{matches.Home, matches.Away} {
		for _, p := range in.Team(side).Starters {
			if !started[p.Player] {
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

func TestCompleteMatchInvariantsAcrossFixtures(t *testing.T) {
	for fixture := ids.FixtureID(1); fixture <= 300; fixture++ {
		in := input(fixture, 8+int(fixture)%9, 16-int(fixture)%9)
		cmds := standardCommands
		if fixture%2 == 0 {
			cmds = nil
		}
		events, final := run(t, start(t, in), []uint16{30, 45, 70, 90}, cmds)
		checkOutcome(t, in, events, final)
	}
}

func TestSameInputSeedAndCommandsGiveSameMatch(t *testing.T) {
	in := input(7, 12, 12)
	e1, f1 := run(t, start(t, in), []uint16{30, 45, 70, 90}, standardCommands)
	e2, f2 := run(t, start(t, input(7, 12, 12)), []uint16{30, 45, 70, 90}, standardCommands)
	if !reflect.DeepEqual(e1, e2) || !reflect.DeepEqual(f1, f2) {
		t.Fatal("identical input, seed and commands produced different matches")
	}
	// A different fixture uses a different stream.
	differs := false
	for fixture := ids.FixtureID(8); fixture < 20 && !differs; fixture++ {
		in := input(7, 12, 12)
		s, err := engine(t).Start(in, rs(fixture))
		if err != nil {
			t.Fatal(err)
		}
		e3, _ := run(t, s, fullMatch, nil)
		differs = !reflect.DeepEqual(e3, e1)
	}
	if !differs {
		t.Fatal("per-fixture random streams did not vary the match")
	}
}

func TestAdvanceChunkingDoesNotChangeTheMatch(t *testing.T) {
	everyMinute := make([]uint16, 0, 90)
	for m := uint16(1); m <= 90; m++ {
		everyMinute = append(everyMinute, m)
	}
	plans := map[string][]uint16{
		"command stops only": {30, 45, 70, 90},
		"every minute":       everyMinute,
		"irregular":          {0, 5, 30, 30, 31, 45, 45, 46, 60, 70, 88, 90},
		"overshooting":       {30, 90, 70, 90}, // the first 90 halts at half time
	}
	var wantEvents []matches.MatchEvent
	var want matches.MatchStepResult
	for _, name := range []string{"command stops only", "every minute", "irregular", "overshooting"} {
		events, final := run(t, start(t, input(3, 13, 11)), plans[name], standardCommands)
		// The last step of a chunked run only holds its own events.
		final.Events = nil
		if wantEvents == nil {
			wantEvents, want = events, final
			continue
		}
		if !reflect.DeepEqual(events, wantEvents) || !reflect.DeepEqual(final, want) {
			t.Fatalf("plan %q produced a different match", name)
		}
	}
}

func TestHalfTimeIsAMandatoryStop(t *testing.T) {
	s := start(t, input(1, 12, 12))
	var dst matches.MatchStepResult
	advance(t, s, 90, &dst)
	if dst.Position != (matches.MatchPosition{Period: matches.HalfTime, Minute: 45}) ||
		dst.Status != matches.MatchDecisionRequired || dst.Stop != matches.StopHalfTime {
		t.Fatalf("Advance(90) from kickoff stopped at %+v %d %d", dst.Position, dst.Status, dst.Stop)
	}
	if last := dst.Events[len(dst.Events)-1]; last.Kind != matches.EventPeriodEnd || last.Period != matches.FirstHalf {
		t.Fatalf("last first-half event %+v", last)
	}
	// Advancing to the current minute at half time resumes nothing.
	advance(t, s, 45, &dst)
	if dst.Status != matches.MatchDecisionRequired || len(dst.Events) != 0 {
		t.Fatalf("Advance(45) at half time: %+v", dst)
	}
	advance(t, s, 60, &dst)
	if dst.Position != (matches.MatchPosition{Period: matches.SecondHalf, Minute: 60}) || dst.Status != matches.MatchRunning {
		t.Fatalf("second half position %+v", dst.Position)
	}
	if dst.Outcome.Status != matches.ResultPending || len(dst.Outcome.Participants) != 0 || dst.Outcome.Score != [2]uint16{} {
		t.Fatalf("unfinished session exposed a result: %+v", dst.Outcome)
	}
}

func TestSubstitutionUpdatesParticipationAndView(t *testing.T) {
	in := input(4, 12, 12)
	s := start(t, in)
	var dst matches.MatchStepResult
	advance(t, s, 60, &dst)
	advance(t, s, 60, &dst)
	if err := s.Apply(sub(matches.Home, 110, 115)); err != nil { // FW 110 off, FW 115 on
		t.Fatal(err)
	}
	advance(t, s, 61, &dst)
	if e := dst.Events[0]; e.Kind != matches.EventSubstitution || e.Minute != 60 || e.Player != 115 || e.Other != 110 {
		t.Fatalf("first event after sub = %+v", e)
	}
	if dst.View.OnPitch[0][9] != 115 || dst.View.SubstitutionsUsed[0] != 1 {
		t.Fatalf("view after sub %+v", dst.View)
	}
	advance(t, s, 90, &dst)
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
	checkScoreAndParticipation(t, in, dst)
}

func TestIllegalCommandsLeaveSessionUnchanged(t *testing.T) {
	s := start(t, input(5, 12, 12))
	reject := func(name string, cmd matches.MatchCommand, want error) {
		t.Helper()
		before := capture(s)
		if err := s.Apply(cmd); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
		if !reflect.DeepEqual(capture(s), before) {
			t.Errorf("%s: rejected command changed the session", name)
		}
	}
	invalid := matches.ErrInvalidCommand

	reject("sub before kickoff", sub(matches.Home, 110, 115), invalid)
	var dst matches.MatchStepResult
	advance(t, s, 20, &dst)
	for _, c := range []struct {
		name string
		cmd  matches.MatchCommand
	}{
		{"invalid side", sub(0, 110, 115)},
		{"unknown kind", matches.MatchCommand{Kind: 9, Side: matches.Home}},
		{"out on bench", sub(matches.Home, 113, 115)},
		{"in already playing", sub(matches.Home, 110, 111)},
		{"unknown player", sub(matches.Home, 110, 999)},
		{"opponent's player", sub(matches.Home, 110, 215)},
		{"outfield for keeper", sub(matches.Home, 101, 115)},
		{"keeper for outfield", sub(matches.Home, 110, 112)},
		{"same mentality", mentality(matches.Home, matches.Balanced)},
		{"invalid mentality", mentality(matches.Home, 7)},
	} {
		reject(c.name, c.cmd, invalid)
	}

	if err := s.Apply(sub(matches.Home, 110, 115)); err != nil {
		t.Fatal(err)
	}
	// With substitutions still available, only the specific rule applies.
	reject("player subbed off returns", sub(matches.Home, 111, 110), invalid)
	reject("substitute brought on twice", sub(matches.Home, 111, 115), invalid)
	reject("subbed-off player taken off again", sub(matches.Home, 110, 116), invalid)
	for _, cmd := range []matches.MatchCommand{sub(matches.Home, 102, 113), sub(matches.Home, 106, 114)} {
		if err := s.Apply(cmd); err != nil {
			t.Fatal(err)
		}
	}
	reject("fourth substitution", sub(matches.Home, 103, 116), invalid)
	if err := s.Apply(sub(matches.Away, 210, 215)); err != nil {
		t.Fatalf("away substitutions are independent: %v", err)
	}
}

// A session that rejected commands plays out exactly like one that never
// received them.
func TestRejectedCommandsDoNotAffectTheMatch(t *testing.T) {
	clean := start(t, input(6, 12, 12))
	noisy := start(t, input(6, 12, 12))
	var a, b matches.MatchStepResult
	for _, stop := range []uint16{20, 45, 90, 90} {
		advance(t, clean, stop, &a)
		advance(t, noisy, stop, &b)
		_ = noisy.Apply(sub(matches.Home, 101, 115))
		_ = noisy.Apply(mentality(matches.Away, matches.Balanced))
		if !reflect.DeepEqual(cloneStep(a), cloneStep(b)) {
			t.Fatalf("rejected commands changed the match at %d", stop)
		}
	}
}

func TestFinishedSessionBehavior(t *testing.T) {
	s := start(t, input(9, 12, 12))
	_, final := run(t, s, fullMatch, nil)

	var dst matches.MatchStepResult
	advance(t, s, 90, &dst)
	if dst.Status != matches.MatchFinished || len(dst.Events) != 0 {
		t.Fatalf("Advance after full time: status %d, %d events", dst.Status, len(dst.Events))
	}
	final.Events = nil
	dst.Events = nil
	if !reflect.DeepEqual(cloneStep(dst), final) {
		t.Fatal("Advance after full time returned a different final state")
	}
	before := capture(s)
	if err := s.Apply(mentality(matches.Home, matches.Attacking)); !errors.Is(err, matches.ErrMatchFinished) {
		t.Fatalf("Apply after full time: %v", err)
	}
	if err := s.Advance(matches.AdvanceRequest{ToMinute: 89}, &dst); !errors.Is(err, matches.ErrInvalidRequest) {
		t.Fatalf("Advance(89) after full time: %v", err)
	}
	if !reflect.DeepEqual(capture(s), before) {
		t.Fatal("finished session changed")
	}
}

func TestInvalidAdvanceLeavesSessionAndOutputUnchanged(t *testing.T) {
	s := start(t, input(10, 12, 12))
	var dst matches.MatchStepResult
	advance(t, s, 30, &dst)
	saved, before := cloneStep(dst), capture(s)
	for _, to := range []uint16{29, 91, 1000} {
		if err := s.Advance(matches.AdvanceRequest{ToMinute: to}, &dst); !errors.Is(err, matches.ErrInvalidRequest) {
			t.Fatalf("Advance(%d): %v", to, err)
		}
	}
	if err := s.Advance(matches.AdvanceRequest{ToMinute: 40}, nil); !errors.Is(err, matches.ErrInvalidRequest) {
		t.Fatalf("nil dst: %v", err)
	}
	if !reflect.DeepEqual(cloneStep(dst), saved) || !reflect.DeepEqual(capture(s), before) {
		t.Fatal("rejected Advance changed the session or the output")
	}
}

func TestStartValidatesInput(t *testing.T) {
	cases := map[string]func(*matches.MatchInput){
		"zero match":        func(in *matches.MatchInput) { in.Match = 0 },
		"same teams":        func(in *matches.MatchInput) { in.Away.Team = in.Home.Team },
		"zero team":         func(in *matches.MatchInput) { in.Home.Team = 0 },
		"ten starters":      func(in *matches.MatchInput) { in.Home.Starters = in.Home.Starters[:10] },
		"no keeper":         func(in *matches.MatchInput) { in.Home.Starters[0].Role = matches.Defender },
		"two keepers":       func(in *matches.MatchInput) { in.Home.Starters[1].Role = matches.Goalkeeper },
		"bench over rules":  func(in *matches.MatchInput) { in.Rules.MaxBench = 5 },
		"bench over engine": func(in *matches.MatchInput) { in.Rules.MaxBench = 8 },
		"subs over bench":   func(in *matches.MatchInput) { in.Rules = matches.Rules{MaxSubstitutions: 8, MaxBench: 7} },
		"duplicate in team": func(in *matches.MatchInput) { in.Home.Bench[0].Player = in.Home.Starters[3].Player },
		"duplicate across":  func(in *matches.MatchInput) { in.Away.Starters[4].Player = in.Home.Starters[4].Player },
		"zero player":       func(in *matches.MatchInput) { in.Away.Bench[2].Player = 0 },
		"rating 0":          func(in *matches.MatchInput) { in.Home.Starters[5].Ratings.Pace = 0 },
		"rating 21":         func(in *matches.MatchInput) { in.Away.Bench[6].Ratings.Stamina = 21 },
		"invalid role":      func(in *matches.MatchInput) { in.Home.Starters[5].Role = 9 },
		"invalid mentality": func(in *matches.MatchInput) { in.Away.Tactics.Mentality = 0 },
	}
	for name, mutate := range cases {
		in := input(1, 12, 12)
		mutate(in)
		if _, err := engine(t).Start(in, rs(1)); !errors.Is(err, matches.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := engine(t).Start(nil, rs(1)); !errors.Is(err, matches.ErrInvalidInput) {
		t.Errorf("nil input: %v", err)
	}
	for name, mutate := range map[string]func(*matches.RandomState){
		"algorithm":  func(r *matches.RandomState) { r.Algorithm = 2 },
		"version":    func(r *matches.RandomState) { r.Version++ },
		"extra word": func(r *matches.RandomState) { r.Words[3] = 1 },
	} {
		r := rs(1)
		mutate(&r)
		if _, err := engine(t).Start(input(1, 12, 12), r); !errors.Is(err, matches.ErrInvalidInput) {
			t.Errorf("random state %s: err = %v", name, err)
		}
	}
}

func TestParamsValidation(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Params){
		"zero version":        func(p *Params) { p.Version = 0 },
		"chance above 1":      func(p *Params) { p.MaxChancePPM = ppm + 1 },
		"min above max":       func(p *Params) { p.MinConversionPPM = p.MaxConversionPPM + 1 },
		"zero offset":         func(p *Params) { p.ConversionOffset = 0 },
		"fatigue cap 100%":    func(p *Params) { p.FatigueCapPer10k = per10k },
		"zero attack weights": func(p *Params) { p.AttackWeights = Weights{} },
		"zero mentality":      func(p *Params) { p.MentalityOwnPermille[matches.Attacking] = 0 },
		"zero forward shots":  func(p *Params) { p.ShotShare[matches.Forward] = 0 },
		"negative keeper":     func(p *Params) { p.DefenseShare[matches.Goalkeeper] = -1 },
	}
	for name, mutate := range cases {
		p := DefaultParams()
		mutate(&p)
		if _, err := New(p); err == nil {
			t.Errorf("%s: New accepted invalid params", name)
		}
	}
}

func TestCapabilitiesAreAdvertised(t *testing.T) {
	e := engine(t)
	want := matches.Capabilities{Substitutions: true, Mentality: true}
	if e.Capabilities() != want || e.ID() != EngineID || e.Version() != ModelVersion {
		t.Fatalf("engine %s v%d capabilities %+v", e.ID(), e.Version(), e.Capabilities())
	}
	s := start(t, input(1, 12, 12))
	if _, err := s.Checkpoint(); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Checkpoint: %v", err)
	}
	if _, err := e.Restore(matches.MatchCheckpoint{EngineID: EngineID}); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Restore: %v", err)
	}
}
