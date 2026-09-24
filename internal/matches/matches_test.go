package matches

import "testing"

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
