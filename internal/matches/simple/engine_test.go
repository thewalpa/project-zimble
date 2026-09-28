package simple

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

func TestIllegalCommandsLeaveSessionUnchanged(t *testing.T) {
	s := start(t, input(5, 60, 60))
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

func TestFinishedSessionBehavior(t *testing.T) {
	s := start(t, input(9, 60, 60))
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
	s := start(t, input(10, 60, 60))
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

func TestParamsValidation(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Params){
		"zero version":        func(p *Params) { p.Version = 0 },
		"chance above 1":      func(p *Params) { p.MaxChancePPM = ppm + 1 },
		"min above max":       func(p *Params) { p.MinConversionPPM = p.MaxConversionPPM + 1 },
		"zero offset":         func(p *Params) { p.ConversionOffset = 0 },
		"fatigue cap 100%":    func(p *Params) { p.FatigueCapPer100k = per100k },
		"zero attack weights": func(p *Params) { p.AttackWeights = Weights{} },
		"zero mentality":      func(p *Params) { p.MentalityOwnPermille[matches.Attacking] = 0 },
		"zero forward shots":  func(p *Params) { p.ShotShare[matches.Forward] = 0 },
		"negative keeper":     func(p *Params) { p.DefenseShare[matches.Goalkeeper] = -1 },
		"certain penalties":   func(p *Params) { p.MaxShootoutPPM = ppm },
		"no penalties":        func(p *Params) { p.MinShootoutPPM = 0 },
		"penalty range":       func(p *Params) { p.MinShootoutPPM = p.MaxShootoutPPM + 1 },
		"zero penalty base":   func(p *Params) { p.ShootoutConversionPPM = 0 },
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
	want := matches.Capabilities{Substitutions: true, Mentality: true, Penalties: true}
	if e.Capabilities() != want || e.ID() != EngineID || e.Version() != ModelVersion {
		t.Fatalf("engine %s v%d capabilities %+v", e.ID(), e.Version(), e.Capabilities())
	}
	s := start(t, input(1, 60, 60))
	if _, err := s.Checkpoint(); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Checkpoint: %v", err)
	}
	if _, err := e.Restore(matches.MatchCheckpoint{EngineID: EngineID}); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("Restore: %v", err)
	}
}

func playOut(t *testing.T, in *matches.MatchInput) matches.MatchOutcome {
	t.Helper()
	return enginetest.Outcome(t, engine(t), in)
}

// Better penalty takers against a weaker goalkeeper win more shootouts.
func TestShootoutsFavourBetterTakers(t *testing.T) {
	wins, level := 0, 0
	for f := ids.FixtureID(1); f <= 3000 && level < 400; f++ {
		in := input(f, 60, 60)
		in.Rules.Knockout = true
		for i := range in.Home.Starters {
			in.Home.Starters[i].Ratings.Finishing = 95
		}
		in.Away.Starters[0].Ratings.Goalkeeping = 30
		o := playOut(t, in)
		if o.Resolution != matches.ResolutionPenalties {
			continue
		}
		level++
		if o.Shootout[0] > o.Shootout[1] {
			wins++
		}
	}
	if level < 100 || wins*100 < level*65 {
		t.Fatalf("the better side won %d of %d shootouts", wins, level)
	}
}

func TestWinner(t *testing.T) {
	for _, c := range []struct {
		score, pens [2]uint16
		side        matches.Side
		ok          bool
	}{
		{[2]uint16{2, 1}, [2]uint16{}, matches.Home, true},
		{[2]uint16{0, 3}, [2]uint16{}, matches.Away, true},
		{[2]uint16{1, 1}, [2]uint16{4, 5}, matches.Away, true},
		{[2]uint16{1, 1}, [2]uint16{}, 0, false},
	} {
		side, ok := matches.MatchOutcome{Score: c.score, Shootout: c.pens}.Winner()
		if side != c.side || ok != c.ok {
			t.Errorf("Winner(%v, %v) = %v, %v", c.score, c.pens, side, ok)
		}
	}
}
