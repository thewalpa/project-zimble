package tick

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

type tally struct {
	matches, homeWins, awayWins, goals int
	scored                             [2]int
	shots, onTarget, passes, completed [2]int
	tackles, saves, offsides           [2]int
	possession                         [2]int // permille summed over matches
	shootouts, homeShootoutWins        int    // knockouts level after 90 minutes
}

// homePoints is the home side's points a match: three a win, one a draw.
func (r tally) homePoints() float64 {
	return float64(3*r.homeWins+r.matches-r.homeWins-r.awayWins) / float64(r.matches)
}

// simulate plays fixtures 1..n in parallel and folds them in fixture order.
func simulate(t *testing.T, n int, build func(ids.FixtureID) *matches.MatchInput) tally {
	t.Helper()
	e := engine(t)
	type result struct {
		score    [2]uint16
		shootout [2]uint16
		pens     bool
		stats    matches.MatchStats
		err      error
	}
	results := make([]result, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			var dst matches.MatchStepResult
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				in := build(ids.FixtureID(i + 1))
				ms, err := e.Start(in, enginetest.Random(e, in.Match))
				for half := 0; err == nil && half < 2; half++ {
					err = ms.Advance(matches.AdvanceRequest{ToMinute: matches.RegulationMinutes}, &dst)
				}
				if err == nil && dst.Status != matches.MatchFinished {
					err = fmt.Errorf("stopped at %+v", dst.Position)
				}
				if err != nil {
					results[i].err = err
					continue
				}
				o := &dst.Outcome
				results[i] = result{score: o.Score, shootout: o.Shootout, pens: o.Resolution == matches.ResolutionPenalties, stats: o.Stats}
			}
		})
	}
	wg.Wait()
	var r tally
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("fixture %d: %v", i+1, res.err)
		}
		h, a := int(res.score[0]), int(res.score[1])
		r.matches++
		r.goals += h + a
		r.scored[0] += h
		r.scored[1] += a
		for k, st := range res.stats.Teams {
			r.shots[k] += int(st.Shots)
			r.onTarget[k] += int(st.ShotsOnTarget)
			r.passes[k] += int(st.Passes)
			r.completed[k] += int(st.PassesCompleted)
			r.tackles[k] += int(st.Tackles)
			r.saves[k] += int(st.Saves)
			r.offsides[k] += int(st.Offsides)
			r.possession[k] += int(st.PossessionPermille)
		}
		switch {
		case h > a:
			r.homeWins++
		case a > h:
			r.awayWins++
		}
		if res.pens {
			r.shootouts++
			if res.shootout[0] > res.shootout[1] {
				r.homeShootoutWins++
			}
		}
	}
	return r
}

func (r tally) String() string {
	n := float64(r.matches)
	return fmt.Sprintf("%.2f goals/match, home %d away %d draws %d, shots %.1f (%.1f on target) v %.1f (%.1f), "+
		"possession %.0f%%, passes %.0f (%.0f%% completed), tackles %.1f, saves %.1f, offsides %.1f v %.1f",
		float64(r.goals)/n, r.homeWins, r.awayWins, r.matches-r.homeWins-r.awayWins,
		float64(r.shots[0])/n, float64(r.onTarget[0])/n, float64(r.shots[1])/n, float64(r.onTarget[1])/n,
		float64(r.possession[0])/n/10, float64(r.passes[0]+r.passes[1])/2/n,
		100*float64(r.completed[0]+r.completed[1])/float64(r.passes[0]+r.passes[1]),
		float64(r.tackles[0]+r.tackles[1])/2/n, float64(r.saves[0]+r.saves[1])/2/n,
		float64(r.offsides[0])/n, float64(r.offsides[1])/n)
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
	if off := even.offsides[0] + even.offsides[1]; off < n || off > 8*n {
		t.Errorf("equal teams were caught offside %d times in %d matches, want 0.5..4 a side", off, n)
	}

	mismatch := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 55, 65) })
	t.Logf("55 v 65: %v", mismatch)
	if mismatch.awayWins < n*45/100 || mismatch.shots[1] <= mismatch.shots[0] {
		t.Errorf("the stronger away team won %d/%d with %d shots to %d", mismatch.awayWins, n, mismatch.shots[1], mismatch.shots[0])
	}
	if mismatch.onTarget[1] <= mismatch.onTarget[0] || mismatch.possession[1] <= mismatch.possession[0] {
		t.Errorf("the stronger away team had %d shots on target to %d and %d permille of possession to %d",
			mismatch.onTarget[1], mismatch.onTarget[0], mismatch.possession[1], mismatch.possession[0])
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
	// Runs and a high line catch attacking sides offside more often.
	if off := func(r tally) int { return r.offsides[0] + r.offsides[1] }; off(attacking) <= off(even) || off(defensive) >= off(even) {
		t.Errorf("offsides: attacking %d, balanced %d, defensive %d", off(attacking), off(even), off(defensive))
	}
}

// Goals depend on the gap between the sides, not on their level: equal
// sides score alike at 40, 60 and 80, and a 20-point mismatch shows in who
// scores far more than in how many goals there are.
func TestGoalsFollowTheGapNotTheLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 800 matches")
	}
	const n = 200
	avg := func(r tally) float64 { return float64(r.goals) / float64(r.matches) }
	equal := map[int]tally{}
	for _, level := range []int{40, 60, 80} {
		r := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, level, level) })
		t.Logf("%d v %d: %v", level, level, r)
		if a := avg(r); a < 2.0 || a > 3.4 {
			t.Errorf("%d v %d average %.2f goals, want 2.0..3.4", level, level, a)
		}
		equal[level] = r
	}
	if lo, hi := avg(equal[40]), avg(equal[80]); hi-lo > 0.6 || lo-hi > 0.6 {
		t.Errorf("40 v 40 averages %.2f goals and 80 v 80 %.2f: goals follow the level", lo, hi)
	}
	gap := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 70, 50) })
	t.Logf("70 v 50: %v", gap)
	if a := avg(gap); a > avg(equal[60])+1.5 {
		t.Errorf("70 v 50 averages %.2f goals, 60 v 60 %.2f: a mismatch inflates goals", a, avg(equal[60]))
	}
	if gap.homeWins < n*3/4 || gap.scored[0] < 4*gap.scored[1] {
		t.Errorf("the stronger side won %d/%d and outscored the weaker %d to %d", gap.homeWins, n, gap.scored[0], gap.scored[1])
	}
}

// Mentality is a trade-off against a balanced side: attacking scores and
// concedes more for a modest change in results, and defensive scores and
// concedes less and draws more. The win-rate bound is several standard
// errors wide.
func TestMentalityTradeOff(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 1,800 matches")
	}
	const n = 600
	homeWith := func(m matches.Mentality) func(ids.FixtureID) *matches.MatchInput {
		return func(f ids.FixtureID) *matches.MatchInput {
			in := input(f, 60, 60)
			in.Home.Tactics.Mentality = m
			return in
		}
	}
	balanced := simulate(t, n, homeWith(matches.Balanced))
	attacking := simulate(t, n, homeWith(matches.Attacking))
	defensive := simulate(t, n, homeWith(matches.Defensive))
	t.Logf("balanced v balanced: %v", balanced)
	t.Logf("attacking v balanced: %v", attacking)
	t.Logf("defensive v balanced: %v", defensive)
	if attacking.scored[0] <= balanced.scored[0] || attacking.scored[1] <= balanced.scored[1] {
		t.Errorf("attacking scored %d and conceded %d, balanced %d and %d: want more at both ends",
			attacking.scored[0], attacking.scored[1], balanced.scored[0], balanced.scored[1])
	}
	if defensive.scored[0] >= balanced.scored[0] || defensive.scored[1] >= balanced.scored[1] {
		t.Errorf("defensive scored %d and conceded %d, balanced %d and %d: want fewer at both ends",
			defensive.scored[0], defensive.scored[1], balanced.scored[0], balanced.scored[1])
	}
	draws := func(r tally) int { return r.matches - r.homeWins - r.awayWins }
	if draws(defensive) <= draws(balanced) {
		t.Errorf("defensive drew %d, balanced %d", draws(defensive), draws(balanced))
	}
	if d := attacking.homeWins - balanced.homeWins; d*100 > n*12 || d*100 < -n*12 {
		t.Errorf("attacking changed home wins by %d of %d, want a modest change", d, n)
	}
	// Attacking is not free: v6's +0.24 points a match would fail.
	if d := attacking.homePoints() - balanced.homePoints(); d > 0.15 {
		t.Errorf("attacking is worth %+.2f points a match against balanced, want at most +0.15", d)
	}
}

// Defensive is not a trap for the underdog: a deep block at least holds its
// own against a stronger side (v8: +0.05 points a match at 12,000 matches a
// row). At 1,000 matches a row the bound is about 2.5 standard errors below
// that, so it catches a backfire, not a small drift.
func TestDefensiveServesTheUnderdog(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 2,000 matches")
	}
	const n = 1000
	underdog := func(m matches.Mentality) func(ids.FixtureID) *matches.MatchInput {
		return func(f ids.FixtureID) *matches.MatchInput {
			in := input(f, 55, 65)
			in.Home.Tactics.Mentality = m
			return in
		}
	}
	balanced := simulate(t, n, underdog(matches.Balanced))
	defensive := simulate(t, n, underdog(matches.Defensive))
	t.Logf("55 v 65, home balanced: %.3f points a match; home defensive: %.3f", balanced.homePoints(), defensive.homePoints())
	if d := defensive.homePoints() - balanced.homePoints(); d < -0.10 {
		t.Errorf("defensive costs the underdog %.2f points a match, want at most 0.10", -d)
	}
}

// Shootouts stay close: the better side's edge in Finishing and Goalkeeping
// shows, but a shootout remains near a coin toss (v7: 56% at 65 v 55).
func TestShootoutsStayClose(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 1,000 matches")
	}
	r := simulate(t, 1000, func(f ids.FixtureID) *matches.MatchInput {
		in := input(f, 65, 55)
		in.Rules.Knockout = true
		return in
	})
	t.Logf("65 v 55 knockouts: the stronger side won %d of %d shootouts", r.homeShootoutWins, r.shootouts)
	if r.shootouts < 200 || r.homeShootoutWins*100 > r.shootouts*65 {
		t.Errorf("the stronger side won %d of %d shootouts, want at most 65%% of at least 200", r.homeShootoutWins, r.shootouts)
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
		"negative skill":     func(p *Params) { p.ShootoutSkillPPM = -1 },
		"restart never ends": func(p *Params) { p.RestartTimeoutTicks = 0 },
		"marking radii":      func(p *Params) { p.TightMarkRadius = p.MarkRadius },
		"no drift interval":  func(p *Params) { p.DriftTicks = 0 },
		"no reference":       func(p *Params) { p.ContestReference = 0 },
		"contest scale":      func(p *Params) { p.ContestPermille = -1 },
		"home advantage":     func(p *Params) { p.HomeAdvantage = -1 },
		"run rate over 1":    func(p *Params) { p.RunPPM[matches.Attacking] = ppm + 1 },
		"endless run":        func(p *Params) { p.RunTicks = 0 },
		"offside blindness":  func(p *Params) { p.OffsideVision = matches.PitchLength },
		"line gives way":     func(p *Params) { p.LineHold = -1 },
		"through ball reach": func(p *Params) { p.ThroughBallLead = p.MaxPass + 1 },
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
const goldenHash = "a3c844127275aa2d"

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

// The first slot of a line is on its side's left flank looking upfield, as
// the contract says: at each kickoff the slots of a line run along Y in
// increasing order for a side attacking towards X = PitchLength and in
// decreasing order for the other.
func TestLineSlotsRunLeftToRight(t *testing.T) {
	in := input(1, 60, 60)
	frames := playOut(t, start(t, in), true).Frames
	check := func(f matches.Frame, homeUp bool) {
		t.Helper()
		for side, team := range []matches.TeamInput{in.Home, in.Away} {
			up := homeUp == (side == 0)
			for slot := 1; slot < matches.StartersPerTeam; slot++ {
				if team.Starters[slot].Role != team.Starters[slot-1].Role {
					continue
				}
				a, b := f.Players[side][slot-1].Y, f.Players[side][slot].Y
				if (up && a >= b) || (!up && a <= b) {
					t.Fatalf("side %d slots %d, %d at %d ms: Y %d then %d, attacking up: %t", side, slot-1, slot, f.Millis, a, b, up)
				}
			}
		}
	}
	check(frames[0], true)
	for _, f := range frames {
		if f.Millis > matches.HalfTimeMinute*60_000 {
			check(f, false)
			return
		}
	}
	t.Fatal("no second-half frame")
}
