package competitions

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/sim"
)

func TestTimingSetsRoundKickoffs(t *testing.T) {
	timing := Timing{FirstKickoff: 1000, RoundInterval: sim.Week}
	s := New()
	if err := s.CreateLeagueSeason(42, league1, eight(), timing); err != nil {
		t.Fatal(err)
	}
	rounds := s.Rounds(league1)
	if len(rounds) != 14 {
		t.Fatalf("got %d rounds", len(rounds))
	}
	for i, r := range rounds {
		want := sim.GameInstant(1000) + sim.GameInstant(i)*sim.GameInstant(sim.Week)
		if r.Ref != (RoundRef{league1, Round(i + 1)}) || r.Kickoff != want || r.Status != RoundScheduled || len(r.Fixtures) != 4 {
			t.Fatalf("round %d = %+v, want kickoff %d", i+1, r, want)
		}
		for _, id := range r.Fixtures {
			if f, _ := s.Fixture(id); f.Kickoff != want || f.Round != r.Ref.Round {
				t.Fatalf("fixture %d = %+v, want round %d kickoff %d", id, f, r.Ref.Round, want)
			}
		}
	}
}

// Timing only moves kickoffs; pairings and fixture IDs are unchanged.
func TestTimingDoesNotAffectPairings(t *testing.T) {
	a := New()
	if err := a.CreateLeagueSeason(42, league1, eight(), Timing{FirstKickoff: 5, RoundInterval: sim.Day}); err != nil {
		t.Fatal(err)
	}
	b := create(t, 42, league1, eight())
	fa, fb := a.Fixtures(league1), b.Fixtures(league1)
	for i := range fa {
		fa[i].Kickoff, fb[i].Kickoff = 0, 0
	}
	if !reflect.DeepEqual(fa, fb) {
		t.Fatal("timing changed pairings or fixture IDs")
	}
}

func TestCreateRejectsInvalidTiming(t *testing.T) {
	cases := map[string]Timing{
		"zero interval":      {FirstKickoff: 0, RoundInterval: 0},
		"negative interval":  {FirstKickoff: 0, RoundInterval: -sim.Week},
		"huge interval":      {FirstKickoff: 0, RoundInterval: 1 << 62},
		"first out of range": {FirstKickoff: sim.MaxInstant + 1, RoundInterval: sim.Week},
		"last out of range":  {FirstKickoff: sim.MaxInstant - sim.GameInstant(sim.Week), RoundInterval: sim.Week},
	}
	for name, timing := range cases {
		s := New()
		if err := s.CreateLeagueSeason(42, league1, eight(), timing); err == nil {
			t.Errorf("%s: CreateLeagueSeason succeeded", name)
		}
		if len(s.Seasons()) != 0 {
			t.Errorf("%s: failed create changed the store", name)
		}
	}
}

func TestBeginRoundsMarksAwaitingResultsOnce(t *testing.T) {
	s := create(t, 42, league1, eight())
	r1 := RoundRef{league1, 1}
	kickoff := s.Rounds(league1)[0].Kickoff
	before := s.Fixtures(league1)

	if err := s.BeginRounds([]RoundRef{r1}, kickoff); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Round(r1)
	if got.Status != RoundAwaitingResults {
		t.Fatalf("status = %s", got.Status)
	}
	if !reflect.DeepEqual(s.Fixtures(league1), before) {
		t.Fatal("BeginRounds changed fixtures")
	}
	if p := s.PendingRounds(); len(p) != 1 || p[0].Ref != r1 {
		t.Fatalf("PendingRounds = %+v", p)
	}
	if err := s.BeginRounds([]RoundRef{r1}, kickoff); err == nil {
		t.Fatal("round dispatched twice")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBeginRoundsIsAllOrNothing(t *testing.T) {
	s := create(t, 42, league1, eight())
	r1, r2 := RoundRef{league1, 1}, RoundRef{league1, 2}
	k1 := s.Rounds(league1)[0].Kickoff
	cases := map[string][]RoundRef{
		"empty":          nil,
		"unknown season": {r1, {SeasonRef{9, 1}, 1}},
		"round 0":        {r1, {league1, 0}},
		"round 15":       {r1, {league1, 15}},
		"duplicate":      {r1, r1},
		"wrong kickoff":  {r1, r2},
	}
	for name, refs := range cases {
		if err := s.BeginRounds(refs, k1); err == nil {
			t.Errorf("%s: BeginRounds succeeded", name)
		}
		if len(s.PendingRounds()) != 0 {
			t.Fatalf("%s: failed BeginRounds left a round pending", name)
		}
	}
}

func TestPendingRoundsOrderIsDeterministic(t *testing.T) {
	s := New()
	// Created out of competition order, all kicking off together.
	for _, comp := range []SeasonRef{{3, 1}, {1, 2}, {2, 1}, {1, 1}} {
		if err := s.CreateLeagueSeason(42, comp, eight(), weekly); err != nil {
			t.Fatal(err)
		}
	}
	k := sim.GameInstant(sim.Week)
	refs := []RoundRef{{SeasonRef{2, 1}, 1}, {SeasonRef{1, 2}, 1}, {SeasonRef{3, 1}, 1}, {SeasonRef{1, 1}, 1}}
	if err := s.BeginRounds(refs, k); err != nil {
		t.Fatal(err)
	}
	var got []SeasonRef
	for _, r := range s.PendingRounds() {
		got = append(got, r.Ref.Season)
	}
	want := []SeasonRef{{1, 1}, {1, 2}, {2, 1}, {3, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending order = %v, want %v", got, want)
	}
}

func TestRoundQueriesReturnCopies(t *testing.T) {
	s := create(t, 42, league1, eight())
	rounds := s.Rounds(league1)
	rounds[0].Fixtures[0] = 999
	rounds[0].Status = RoundAwaitingResults
	if again := s.Rounds(league1); again[0].Fixtures[0] == 999 || again[0].Status != RoundScheduled {
		t.Fatal("Rounds exposed internal state")
	}
	if _, ok := s.Round(RoundRef{league1, 15}); ok {
		t.Fatal("Round found round 15")
	}
}
