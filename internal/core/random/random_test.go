package random

import "testing"

func TestStreamMatchesSplitMix64Reference(t *testing.T) {
	// Reference outputs of SplitMix64 seeded with 0.
	want := []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f}
	s := NewStream(0)
	for i, w := range want {
		if got := s.Uint64(); got != w {
			t.Fatalf("output %d = %#x, want %#x", i, got, w)
		}
	}
}

func TestDeriveIsStableAndSeparatesContexts(t *testing.T) {
	a := Derive(42, "worldgen/player", 1, 7).Uint64()
	if b := Derive(42, "worldgen/player", 1, 7).Uint64(); a != b {
		t.Fatalf("same inputs gave %#x and %#x", a, b)
	}
	others := map[string]*Stream{
		"seed":   Derive(43, "worldgen/player", 1, 7),
		"domain": Derive(42, "worldgen/club", 1, 7),
		"key":    Derive(42, "worldgen/player", 1, 8),
		"arity":  Derive(42, "worldgen/player", 1),
	}
	for name, s := range others {
		if s.Uint64() == a {
			t.Errorf("changing %s did not change the stream", name)
		}
	}
}

func TestIntRangeStaysInBoundsAndCoversRange(t *testing.T) {
	s := NewStream(1)
	seen := map[int]int{}
	for range 10_000 {
		v := s.IntRange(3, 9)
		if v < 3 || v > 9 {
			t.Fatalf("IntRange(3, 9) = %d", v)
		}
		seen[v]++
	}
	if len(seen) != 7 {
		t.Fatalf("saw %d distinct values, want 7", len(seen))
	}
}

func TestPermIsAPermutation(t *testing.T) {
	p := NewStream(5).Perm(12)
	seen := make([]bool, 12)
	for _, v := range p {
		if v < 0 || v >= 12 || seen[v] {
			t.Fatalf("invalid permutation %v", p)
		}
		seen[v] = true
	}
}

func TestStateContinuesSequence(t *testing.T) {
	a := Derive(42, "x", 1)
	a.Uint64()
	b := NewStream(a.State())
	for range 5 {
		if a.Uint64() != b.Uint64() {
			t.Fatal("NewStream(State()) diverged")
		}
	}
}
