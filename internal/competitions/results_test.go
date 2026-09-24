package competitions

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// begun returns a store with league1 round 1 awaiting results.
func begun(t *testing.T) (*Store, RoundRef, sim.GameInstant) {
	t.Helper()
	s := create(t, 42, league1, eight())
	r1 := RoundRef{league1, 1}
	k := s.Rounds(league1)[0].Kickoff
	if err := s.BeginRounds([]RoundRef{r1}, k); err != nil {
		t.Fatal(err)
	}
	return s, r1, k
}

func scoresFor(s *Store, ref RoundRef, goals func(i int) (uint16, uint16)) []Score {
	r, _ := s.Round(ref)
	out := make([]Score, len(r.Fixtures))
	for i, id := range r.Fixtures {
		h, a := goals(i)
		out[i] = Score{Fixture: id, HomeGoals: h, AwayGoals: a}
	}
	return out
}

func TestCompleteRoundsRecordsResultsAndStatusTogether(t *testing.T) {
	s, r1, k := begun(t)
	scores := scoresFor(s, r1, func(i int) (uint16, uint16) { return uint16(i), 1 })
	if err := s.CompleteRounds([]RoundRef{r1}, scores, k); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Round(r1); r.Status != RoundCompleted {
		t.Fatalf("status = %s", r.Status)
	}
	if len(s.PendingRounds()) != 0 {
		t.Fatal("completed round still pending")
	}
	results := s.Results(league1)
	if len(results) != 4 {
		t.Fatalf("%d results", len(results))
	}
	for i, r := range results {
		f, _ := s.Fixture(r.Fixture)
		if r.HomeGoals != uint16(i) || r.AwayGoals != 1 || r.RecordedAt != k || r.Home != f.Home || r.Away != f.Away || r.Round != 1 {
			t.Fatalf("result %d = %+v", i, r)
		}
		if got, ok := s.Result(r.Fixture); !ok || got != r {
			t.Fatalf("Result(%d) = %+v, %v", r.Fixture, got, ok)
		}
	}
	if _, ok := s.Result(s.Rounds(league1)[1].Fixtures[0]); ok {
		t.Fatal("unplayed fixture has a result")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	// Completing again cannot double-count.
	if err := s.CompleteRounds([]RoundRef{r1}, scores, k); err == nil {
		t.Fatal("round completed twice")
	}
}

func TestCompleteRoundsIsAllOrNothing(t *testing.T) {
	s, r1, k := begun(t)
	good := scoresFor(s, r1, func(int) (uint16, uint16) { return 2, 0 })
	r2 := RoundRef{league1, 2}
	otherFixture := s.Rounds(league1)[1].Fixtures[0]
	cases := map[string]struct {
		refs   []RoundRef
		scores []Score
		at     sim.GameInstant
	}{
		"no rounds":              {nil, good, k},
		"unknown round":          {[]RoundRef{r1, {league1, 15}}, good, k},
		"round listed twice":     {[]RoundRef{r1, r1}, good, k},
		"round not kicked off":   {[]RoundRef{r1, r2}, good, k},
		"before kickoff":         {[]RoundRef{r1}, good, k - 1},
		"missing score":          {[]RoundRef{r1}, good[1:], k},
		"extra score":            {[]RoundRef{r1}, append(append([]Score{}, good...), Score{Fixture: otherFixture}), k},
		"fixture from elsewhere": {[]RoundRef{r1}, append(append([]Score{}, good[1:]...), Score{Fixture: otherFixture}), k},
		"duplicate fixture":      {[]RoundRef{r1}, append(append([]Score{}, good[1:]...), good[1]), k},
		"absurd score":           {[]RoundRef{r1}, append([]Score{{Fixture: good[0].Fixture, HomeGoals: MaxGoals + 1}}, good[1:]...), k},
	}
	before := s.Rounds(league1)
	for name, c := range cases {
		if err := s.CompleteRounds(c.refs, c.scores, c.at); err == nil {
			t.Errorf("%s: CompleteRounds succeeded", name)
		}
		if !reflect.DeepEqual(s.Rounds(league1), before) || len(s.Results(league1)) != 0 {
			t.Fatalf("%s: failed CompleteRounds changed the store", name)
		}
	}
}

// independentTable recomputes standings without using Standings.
func independentTable(entrants []ids.TeamID, results []Result) map[ids.TeamID][4]int { // points, gd, gf, played
	out := map[ids.TeamID][4]int{}
	for _, e := range entrants {
		out[e] = [4]int{}
	}
	add := func(team ids.TeamID, gf, ga int) {
		row := out[team]
		switch {
		case gf > ga:
			row[0] += 3
		case gf == ga:
			row[0]++
		}
		row[1] += gf - ga
		row[2] += gf
		row[3]++
		out[team] = row
	}
	for _, r := range results {
		add(r.Home, int(r.HomeGoals), int(r.AwayGoals))
		add(r.Away, int(r.AwayGoals), int(r.HomeGoals))
	}
	return out
}

func TestStandingsRankingAndTieBreakers(t *testing.T) {
	s, r1, k := begun(t)
	fixtures := s.Rounds(league1)[0].Fixtures
	// Engineered so every tie-breaker matters: two wins by one goal (one
	// scoring more), and two 0-0 draws.
	goals := [][2]uint16{{2, 1}, {1, 0}, {0, 0}, {0, 0}}
	var scores []Score
	for i, id := range fixtures {
		scores = append(scores, Score{Fixture: id, HomeGoals: goals[i][0], AwayGoals: goals[i][1]})
	}
	if err := s.CompleteRounds([]RoundRef{r1}, scores, k); err != nil {
		t.Fatal(err)
	}
	table := s.Standings(league1)
	f := func(i int) Fixture { f, _ := s.Fixture(fixtures[i]); return f }
	// Order: 2-1 winner (GF 2) before 1-0 winner (GF 1); the four 0-0 teams
	// tie on points, GD and GF, so TeamID decides; then the 1-2 loser (GF 1)
	// before the 0-1 loser (GF 0).
	draws := []ids.TeamID{f(2).Home, f(2).Away, f(3).Home, f(3).Away}
	slices.Sort(draws)
	want := slices.Concat([]ids.TeamID{f(0).Home, f(1).Home}, draws, []ids.TeamID{f(0).Away, f(1).Away})
	for i, row := range table {
		if row.Rank != i+1 || row.Team != want[i] {
			t.Fatalf("rank %d: team %d (%+v), want team %d; table %+v", i+1, row.Team, row, want[i], table)
		}
	}
	indep := independentTable(eight(), s.Results(league1))
	for _, row := range table {
		want := indep[row.Team]
		if row.Points != want[0] || row.GoalDifference() != want[1] || row.GoalsFor != want[2] || row.Played != want[3] ||
			row.Won+row.Drawn+row.Lost != row.Played || row.Points != 3*row.Won+row.Drawn {
			t.Fatalf("row %+v disagrees with independent %v", row, want)
		}
	}
}

func TestStandingsBeforeAnyResults(t *testing.T) {
	s := create(t, 42, league1, eight())
	table := s.Standings(league1)
	for i, row := range table {
		if row.Team != ids.TeamID(i+1) || row.Played != 0 || row.Points != 0 {
			t.Fatalf("row %d = %+v; empty table must be in TeamID order", i, row)
		}
	}
	if s.SeasonCompleted(league1) {
		t.Fatal("fresh season reported completed")
	}
}
