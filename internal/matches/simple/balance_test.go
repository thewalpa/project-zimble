package simple

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

type tally struct{ matches, homeWins, awayWins, goals int }

func simulate(t *testing.T, n int, build func(ids.FixtureID) *matches.MatchInput) tally {
	t.Helper()
	var r tally
	var dst matches.MatchStepResult
	e := engine(t)
	for f := ids.FixtureID(1); f <= ids.FixtureID(n); f++ {
		in := build(f)
		s, err := e.Start(in, rs(f))
		if err != nil {
			t.Fatal(err)
		}
		advance(t, s, 90, &dst)
		advance(t, s, 90, &dst)
		h, a := int(dst.Outcome.Score[0]), int(dst.Outcome.Score[1])
		r.matches++
		r.goals += h + a
		switch {
		case h > a:
			r.homeWins++
		case a > h:
			r.awayWins++
		}
	}
	return r
}

// Seeded batches show the intended trends. Bounds are loose on purpose:
// this guards against broken signs and gross miscalibration, not tuning.
func TestModelTrends(t *testing.T) {
	const n = 2000
	even := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 12, 12) })
	avg := float64(even.goals) / n
	t.Logf("equal teams: %.2f goals/match, home %d, away %d, draws %d", avg, even.homeWins, even.awayWins, n-even.homeWins-even.awayWins)
	if avg < 2.0 || avg > 3.5 {
		t.Errorf("equal teams average %.2f goals, want 2.0..3.5", avg)
	}
	if even.homeWins <= even.awayWins {
		t.Errorf("no home advantage: %d home wins, %d away wins", even.homeWins, even.awayWins)
	}

	mismatch := simulate(t, n, func(f ids.FixtureID) *matches.MatchInput { return input(f, 9, 15) })
	t.Logf("weak home v strong away: home %d, away %d", mismatch.homeWins, mismatch.awayWins)
	if mismatch.awayWins < n*55/100 {
		t.Errorf("stronger away team won %d/%d, want at least 55%%", mismatch.awayWins, n)
	}

	withMentality := func(m matches.Mentality) func(ids.FixtureID) *matches.MatchInput {
		return func(f ids.FixtureID) *matches.MatchInput {
			in := input(f, 12, 12)
			in.Home.Tactics.Mentality, in.Away.Tactics.Mentality = m, m
			return in
		}
	}
	attacking := simulate(t, n, withMentality(matches.Attacking))
	defensive := simulate(t, n, withMentality(matches.Defensive))
	t.Logf("goals: attacking %d, defensive %d", attacking.goals, defensive.goals)
	if attacking.goals <= even.goals || defensive.goals >= even.goals {
		t.Errorf("mentality has no effect: attacking %d, balanced %d, defensive %d goals", attacking.goals, even.goals, defensive.goals)
	}
}

// Fresh legs matter: replacing tired low-stamina players changes chances.
func TestFatigueReducesEffectiveRatings(t *testing.T) {
	s := start(t, input(1, 12, 12))
	p := &s.teams[0].players[9]
	s.minute = 1
	fresh := s.effective(p, p.r.Finishing)
	s.minute = 90
	tired := s.effective(p, p.r.Finishing)
	if tired >= fresh {
		t.Fatalf("effective finishing fresh %d, after 89 minutes %d", fresh, tired)
	}
	sub := *p
	sub.on = 60
	if s.effective(&sub, sub.r.Finishing) <= tired {
		t.Fatal("a substitute is as tired as a starter")
	}
}

// Condition at kickoff matters: the same team plays worse when tired, and a
// fresh substitute of equal ability outperforms a tired starter.
func TestConditionReducesPerformance(t *testing.T) {
	const n = 2000
	tired := func(condition uint16) func(ids.FixtureID) *matches.MatchInput {
		return func(f ids.FixtureID) *matches.MatchInput {
			in := input(f, 12, 12)
			for i := range in.Home.Starters {
				in.Home.Starters[i].Condition = condition
			}
			return in
		}
	}
	fresh := simulate(t, n, tired(matches.MaxCondition))
	weary := simulate(t, n, tired(4_000))
	t.Logf("home wins: fresh %d, at condition 4000 %d", fresh.homeWins, weary.homeWins)
	if weary.homeWins >= fresh.homeWins || weary.awayWins <= fresh.awayWins {
		t.Errorf("condition has no effect: home wins %d -> %d, away wins %d -> %d",
			fresh.homeWins, weary.homeWins, fresh.awayWins, weary.awayWins)
	}

	in := input(1, 12, 12)
	in.Home.Starters[9].Condition = 5_000
	s := start(t, in)
	s.minute = 1
	low, full := &s.teams[0].players[9], &s.teams[0].players[10]
	if s.effective(low, 12) >= s.effective(full, 12) {
		t.Fatal("a tired player's effective rating is not lower")
	}
}

func TestConditionOutOfRangeIsRejected(t *testing.T) {
	for _, c := range []uint16{0, matches.MaxCondition + 1} {
		in := input(1, 12, 12)
		in.Away.Bench[2].Condition = c
		e, err := New(DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Start(in, rs(1)); err == nil {
			t.Errorf("condition %d accepted", c)
		}
	}
	p := DefaultParams()
	p.ConditionFloorPer10k = 0
	if _, err := New(p); err == nil {
		t.Error("zero condition floor accepted")
	}
}
