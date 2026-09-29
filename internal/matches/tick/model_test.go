package tick

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

type tally struct {
	matches, homeWins, awayWins, goals int
	shots, passes, completed           [2]int
}

func simulate(t *testing.T, n int, build func(ids.FixtureID) *matches.MatchInput) tally {
	t.Helper()
	var r tally
	for f := ids.FixtureID(1); f <= ids.FixtureID(n); f++ {
		s := start(t, build(f))
		o := playOut(t, s, false).Outcome
		h, a := int(o.Score[0]), int(o.Score[1])
		r.matches++
		r.goals += h + a
		for i := range 2 {
			r.shots[i] += s.stats.shots[i]
			r.passes[i] += s.stats.passes[i]
			r.completed[i] += s.stats.completed[i]
		}
		switch {
		case h > a:
			r.homeWins++
		case a > h:
			r.awayWins++
		}
	}
	return r
}

func (r tally) String() string {
	n := float64(r.matches)
	return fmt.Sprintf("%.2f goals/match, home %d away %d draws %d, shots %.1f v %.1f, passes %.0f (%.0f%% completed)",
		float64(r.goals)/n, r.homeWins, r.awayWins, r.matches-r.homeWins-r.awayWins,
		float64(r.shots[0])/n, float64(r.shots[1])/n, float64(r.passes[0]+r.passes[1])/2/n,
		100*float64(r.completed[0]+r.completed[1])/float64(r.passes[0]+r.passes[1]))
}

// Seeded batches show the intended trends. Bounds are loose on purpose:
// this guards against broken signs and gross miscalibration, not tuning.
func TestModelTrends(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 800 matches")
	}
	const n = 200
	even := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 60, 60) })
	t.Logf("equal teams: %v", even)
	if avg := float64(even.goals) / n; avg < 2.0 || avg > 3.5 {
		t.Errorf("equal teams average %.2f goals, want 2.0..3.5", avg)
	}
	if even.homeWins <= even.awayWins {
		t.Errorf("no home advantage: %d home wins, %d away wins", even.homeWins, even.awayWins)
	}
	if done := even.completed[0] + even.completed[1]; done*100 < (even.passes[0]+even.passes[1])*60 {
		t.Errorf("only %d of %d passes completed", done, even.passes[0]+even.passes[1])
	}

	mismatch := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 55, 65) })
	t.Logf("55 v 65: %v", mismatch)
	if mismatch.awayWins < n*45/100 || mismatch.shots[1] <= mismatch.shots[0] {
		t.Errorf("the stronger away team won %d/%d with %d shots to %d", mismatch.awayWins, n, mismatch.shots[1], mismatch.shots[0])
	}

	withMentality := func(m matches.Mentality) func(ids.FixtureID) *matches.MatchInput {
		return func(f ids.FixtureID) *matches.MatchInput {
			in := input(f, 60, 60)
			in.Home.Tactics.Mentality, in.Away.Tactics.Mentality = m, m
			return in
		}
	}
	attacking := simulate(t, n, withMentality(matches.Attacking))
	defensive := simulate(t, n, withMentality(matches.Defensive))
	t.Logf("attacking: %v", attacking)
	t.Logf("defensive: %v", defensive)
	if attacking.goals <= even.goals || defensive.goals >= even.goals {
		t.Errorf("mentality has no effect: attacking %d, balanced %d, defensive %d goals", attacking.goals, even.goals, defensive.goals)
	}
}

// Tired legs lose matches: a side starting at low condition does worse.
func TestConditionMatters(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 200 matches")
	}
	const n = 200
	tired := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput {
		in := input(f, 60, 60)
		for i := range in.Home.Starters {
			in.Home.Starters[i].Condition = 30
		}
		return in
	})
	t.Logf("tired home side: %v", tired)
	if tired.homeWins >= tired.awayWins {
		t.Errorf("a tired home side won %d, lost %d", tired.homeWins, tired.awayWins)
	}
}

// Players never move faster than a sprint, and nobody is teleported during
// play: only the line-up for the second half's kickoff moves players
// between instants.
func TestPlayersMoveAtHumanSpeed(t *testing.T) {
	p := DefaultParams()
	for f := ids.FixtureID(1); f <= 5; f++ {
		frames := playOut(t, start(t, input(f, 40, 80)), true).Frames
		for i := 1; i < len(frames); i++ {
			if frames[i].Millis == matches.HalfTimeMinute*60_000+tickMillis {
				continue
			}
			for side := range frames[i].Players {
				for slot, now := range frames[i].Players[side] {
					before := frames[i-1].Players[side][slot]
					dx, dy := int64(now.X-before.X), int64(now.Y-before.Y)
					if dx*dx+dy*dy > p.MaxSprint*p.MaxSprint {
						t.Fatalf("fixture %d, %d ms: slot %d of side %d moved (%d, %d) in one instant", f, frames[i].Millis, slot, side, dx, dy)
					}
				}
			}
		}
	}
}

func TestParamsValidation(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Params){
		"zero version":       func(p *Params) { p.Version = 0 },
		"probability over 1": func(p *Params) { p.ControlPPM = ppm + 1 },
		"no sprint":          func(p *Params) { p.MinSprint = 0 },
		"sprint range":       func(p *Params) { p.MaxSprint = p.MinSprint - 1 },
		"no deceleration":    func(p *Params) { p.GroundDecel = 0 },
		"control range":      func(p *Params) { p.MinControlPPM = p.MaxControlPPM + 1 },
		"short reach":        func(p *Params) { p.KeeperReach = p.ControlRadius - 1 },
		"pass range":         func(p *Params) { p.MaxPass = p.MinPass },
		"no pass noise":      func(p *Params) { p.PassNoise = 0 },
		"line off pitch":     func(p *Params) { p.AttackDepth[matches.Forward] = matches.PitchLength },
		"zero mentality":     func(p *Params) { p.MentalityShotPermille[matches.Attacking] = 0 },
		"too many pressers":  func(p *Params) { p.Pressers[matches.Attacking] = 11 },
		"certain penalties":  func(p *Params) { p.MaxShootoutPPM = ppm },
		"restart never ends": func(p *Params) { p.RestartTimeoutTicks = 0 },
	}
	for name, mutate := range cases {
		p := DefaultParams()
		mutate(&p)
		if _, err := New(p); err == nil {
			t.Errorf("%s: New accepted invalid params", name)
		}
	}
}

// goldenHash pins ModelVersion's output: outcomes and every frame of a few
// matches, with commands. Bump ModelVersion when it changes on purpose.
const goldenHash = "4ace18e230a7d6b5"

func TestGolden(t *testing.T) {
	h := fnv.New64a()
	w := func(vs ...any) {
		for _, v := range vs {
			if err := binary.Write(h, binary.LittleEndian, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	e := engine(t)
	for f := ids.FixtureID(1); f <= 3; f++ {
		in := input(f, 50+5*int(f), 60)
		in.Rules.Knockout = f == 3
		s := enginetest.Start(t, e, in)
		var dst matches.MatchStepResult
		applied := 0
		for _, stop := range []uint16{30, 45, 70, 90} {
			if err := s.Advance(matches.AdvanceRequest{ToMinute: stop, Frames: true}, &dst); err != nil {
				t.Fatal(err)
			}
			for _, fr := range dst.Frames {
				w(fr.Millis, fr.Ball, fr.Carrier, fr.Players)
			}
			for _, c := range enginetest.StandardCommands[applied:] {
				if c.Minute != dst.Position.Minute {
					break
				}
				if err := s.Apply(c.Cmd); err != nil {
					t.Fatal(err)
				}
				applied++
			}
		}
		o := dst.Outcome
		w(o.Score, o.Shootout, o.Resolution)
		for _, g := range o.Goals {
			w(g.Minute, g.Side, g.Scorer)
		}
		for _, p := range o.Participants {
			w(p.Player, p.OnMinute, p.OffMinute)
		}
	}
	if got := fmt.Sprintf("%016x", h.Sum64()); got != goldenHash {
		t.Fatalf("model v%d output hash %s, golden %s: bump ModelVersion if the change is intended", ModelVersion, got, goldenHash)
	}
}
