package matches

import (
	"errors"
	"testing"
)

func TestFixtureRandomIsExplicitPerFixture(t *testing.T) {
	a := FixtureRandom(42, "simple", 1, 7)
	if a != FixtureRandom(42, "simple", 1, 7) {
		t.Fatal("same inputs gave different random states")
	}
	if a.Algorithm != AlgorithmSplitMix64 || a.Words[1] != 0 || a.Words[2] != 0 || a.Words[3] != 0 {
		t.Fatalf("unexpected random state shape %+v", a)
	}
	for name, other := range map[string]RandomState{
		"seed":    FixtureRandom(43, "simple", 1, 7),
		"engine":  FixtureRandom(42, "detailed", 1, 7),
		"version": FixtureRandom(42, "simple", 2, 7),
		"fixture": FixtureRandom(42, "simple", 1, 8),
	} {
		if other.Words == a.Words {
			t.Errorf("changing %s did not change the stream", name)
		}
	}
}

func TestSideHelpers(t *testing.T) {
	if Home.Index() != 0 || Away.Index() != 1 || Home.Opponent() != Away || Away.Opponent() != Home {
		t.Fatal("side helpers")
	}
	if Side(0).Valid() || Side(3).Valid() {
		t.Fatal("invalid sides reported valid")
	}
}

func TestMatchStatsValidate(t *testing.T) {
	good := MatchStats{Available: true, Teams: [2]TeamStats{
		{Shots: 12, ShotsOnTarget: 5, Passes: 400, PassesCompleted: 320, Tackles: 18, Saves: 2, PossessionPermille: 540},
		{Shots: 8, ShotsOnTarget: 3, Passes: 350, PassesCompleted: 270, Tackles: 20, Saves: 4, PossessionPermille: 460},
	}}
	oneSided := good
	oneSided.Teams[0].PossessionPermille, oneSided.Teams[1].PossessionPermille = 0, 1000
	for name, s := range map[string]MatchStats{"complete": good, "unavailable": {}, "before kickoff": {Available: true}, "one-sided": oneSided} {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]func(*MatchStats){
		"unavailable with numbers":   func(s *MatchStats) { s.Available = false },
		"more on target than shots":  func(s *MatchStats) { s.Teams[0].ShotsOnTarget = 13 },
		"more completed than passes": func(s *MatchStats) { s.Teams[1].PassesCompleted = 351 },
		"more saves than on target":  func(s *MatchStats) { s.Teams[1].Saves = 6 },
		"possession short of 1000":   func(s *MatchStats) { s.Teams[0].PossessionPermille = 539 },
	}
	for name, mutate := range bad {
		s := good
		mutate(&s)
		if err := s.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: Validate returned %v", name, err)
		}
	}
}
