package competitions

import (
	"reflect"
	"testing"
)

// playedStore has round 1 completed and round 2 awaiting results.
func playedStore(t *testing.T) *Store {
	t.Helper()
	s, r1, k := begun(t)
	if err := s.CompleteRounds([]RoundRef{r1}, scoresFor(s, r1, func(i int) (uint16, uint16) { return uint16(i), 2 }), k); err != nil {
		t.Fatal(err)
	}
	r2 := RoundRef{league1, 2}
	if err := s.BeginRounds([]RoundRef{r2}, s.Rounds(league1)[1].Kickoff); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := playedStore(t)
	snap := s.Snapshot()
	r, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), snap) {
		t.Fatal("Restore(Snapshot()) is not equivalent")
	}
	// Derived views are rebuilt identically.
	if !reflect.DeepEqual(r.Standings(league1), s.Standings(league1)) || !reflect.DeepEqual(r.PendingRounds(), s.PendingRounds()) {
		t.Fatal("derived views differ after restore")
	}
	for _, f := range s.Fixtures(league1) {
		a, _ := s.Result(f.ID)
		b, _ := r.Result(f.ID)
		if got, _ := r.Fixture(f.ID); got != f || a != b {
			t.Fatalf("fixture %d differs after restore", f.ID)
		}
	}
}

func TestSnapshotsDoNotShareMemory(t *testing.T) {
	s := playedStore(t)
	snap := s.Snapshot()
	want := s.Snapshot()
	snap.Seasons[0].Fixtures[0].Home = 99
	snap.Seasons[0].Entrants[0] = 99
	snap.Seasons[0].Results[0].HomeGoals = 50
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("mutating a snapshot changed the store")
	}
	r, err := Restore(want)
	if err != nil {
		t.Fatal(err)
	}
	want.Seasons[0].Fixtures[0].Away = 77
	want.Seasons[0].Rounds[0].Status = RoundScheduled
	if got := r.Snapshot(); got.Seasons[0].Fixtures[0].Away == 77 || got.Seasons[0].Rounds[0].Status != RoundCompleted {
		t.Fatal("restored store shares memory with its snapshot")
	}
}

func TestRestoredAllocatorContinues(t *testing.T) {
	s := playedStore(t)
	r, err := Restore(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	next := SeasonRef{Competition: 1, Season: 2}
	if err := r.CreateLeagueSeason(42, next, eight(), weekly); err != nil {
		t.Fatal(err)
	}
	if first := r.Fixtures(next)[0].ID; first != 57 {
		t.Fatalf("first new fixture ID = %d, want 57 (after 56 saved)", first)
	}
}

func TestRestoreRejectsInvalidSnapshots(t *testing.T) {
	cases := map[string]func(*Snapshot){
		"allocator below IDs":      func(s *Snapshot) { s.LastFixture = 55 },
		"duplicate season":         func(s *Snapshot) { s.Seasons = append(s.Seasons, s.Seasons[0]) },
		"zero season":              func(s *Snapshot) { s.Seasons[0].Ref.Season = 0 },
		"entrants unsorted":        func(s *Snapshot) { e := s.Seasons[0].Entrants; e[0], e[1] = e[1], e[0] },
		"duplicate entrant":        func(s *Snapshot) { s.Seasons[0].Entrants[1] = s.Seasons[0].Entrants[0] },
		"missing round":            func(s *Snapshot) { s.Seasons[0].Rounds = s.Seasons[0].Rounds[1:] },
		"invalid status":           func(s *Snapshot) { s.Seasons[0].Rounds[5].Status = 9 },
		"kickoffs not increasing":  func(s *Snapshot) { s.Seasons[0].Rounds[3].Kickoff = s.Seasons[0].Rounds[2].Kickoff },
		"fixture kickoff differs":  func(s *Snapshot) { s.Seasons[0].Fixtures[9].Kickoff++ },
		"fixture wrong season":     func(s *Snapshot) { s.Seasons[0].Fixtures[3].Season.Season = 2 },
		"fixture unknown team":     func(s *Snapshot) { s.Seasons[0].Fixtures[3].Home = 99 },
		"fixture self-match":       func(s *Snapshot) { s.Seasons[0].Fixtures[3].Away = s.Seasons[0].Fixtures[3].Home },
		"fixtures out of order":    func(s *Snapshot) { f := s.Seasons[0].Fixtures; f[0], f[1] = f[1], f[0] },
		"missing fixture":          func(s *Snapshot) { s.Seasons[0].Fixtures = s.Seasons[0].Fixtures[:55] },
		"result unknown fixture":   func(s *Snapshot) { s.Seasons[0].Results[0].Fixture = 999 },
		"duplicate result":         func(s *Snapshot) { s.Seasons[0].Results[1].Fixture = s.Seasons[0].Results[0].Fixture },
		"result before kickoff":    func(s *Snapshot) { s.Seasons[0].Results[0].RecordedAt-- },
		"absurd score":             func(s *Snapshot) { s.Seasons[0].Results[0].AwayGoals = MaxGoals + 1 },
		"result for pending round": func(s *Snapshot) { s.Seasons[0].Rounds[0].Status = RoundAwaitingResults },
		"completed without results": func(s *Snapshot) {
			s.Seasons[0].Results = s.Seasons[0].Results[:3]
		},
	}
	for name, mutate := range cases {
		snap := playedStore(t).Snapshot()
		mutate(&snap)
		if _, err := Restore(snap); err == nil {
			t.Errorf("%s: Restore succeeded", name)
		}
	}
}
