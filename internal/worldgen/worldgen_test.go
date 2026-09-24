package worldgen

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/players"
)

// goldenSeed42 pins the output of Generate(content.Default(), 42). If this
// test fails, either fix the regression or, when the change is intended, bump
// worldgen.Version (or content.Version / random.Version) and update the value.
const goldenSeed42 = "8bc01a116a8c9d787dd1c2791dc304ab74d20c8af96d082245b7d8997723f0c1"

func generate(t *testing.T, seed uint64) Snapshot {
	t.Helper()
	s, err := Generate(content.Default(), random.Seed(seed))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, b := generate(t, 42), generate(t, 42)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed produced different snapshots")
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("same snapshot produced different fingerprints")
	}
}

func TestGenerateMatchesGoldenFingerprint(t *testing.T) {
	if got := generate(t, 42).Fingerprint(); got != goldenSeed42 {
		t.Fatalf("seed 42 fingerprint = %s, want %s\n"+
			"generated content changed: fix the regression or bump a version and update goldenSeed42", got, goldenSeed42)
	}
}

func TestDifferentSeedsProduceDifferentWorlds(t *testing.T) {
	if generate(t, 42).Fingerprint() == generate(t, 43).Fingerprint() {
		t.Fatal("seeds 42 and 43 produced the same world")
	}
}

func TestGenerateShapeAndRanges(t *testing.T) {
	defs := content.Default()
	for _, seed := range []uint64{0, 1, 42, 1 << 63} {
		s := generate(t, seed)
		if len(s.Clubs) != 8 || len(s.Teams) != 8 || len(s.Players) != 160 {
			t.Fatalf("seed %d: clubs=%d teams=%d players=%d", seed, len(s.Clubs), len(s.Teams), len(s.Players))
		}
		if len(s.Profiles) != 160 || len(s.Assignments) != 160 {
			t.Fatalf("seed %d: profiles=%d assignments=%d", seed, len(s.Profiles), len(s.Assignments))
		}
		for i, c := range s.Clubs {
			if c.ID != ids.ClubID(i+1) || s.Teams[i].ID != ids.TeamID(i+1) || s.Teams[i].Club != c.ID {
				t.Fatalf("seed %d: club/team %d not allocated sequentially", seed, i)
			}
		}
		for i, p := range s.Profiles {
			if s.Players[i].ID != p.Player || s.Assignments[i].Player != p.Player || p.Player != ids.PlayerID(i+1) {
				t.Fatalf("seed %d: player row %d IDs disagree", seed, i)
			}
			base, _ := defs.Profile(p.Position)
			for a, r := range p.Attributes {
				if rg := base.Ranges[a]; r < rg.Min || r > rg.Max {
					t.Fatalf("seed %d: player %d %s=%d outside %d..%d",
						seed, p.Player, players.Attribute(a), r, rg.Min, rg.Max)
				}
			}
		}
	}
}

func TestGenerateRejectsInvalidDefinitions(t *testing.T) {
	defs := content.Default()
	defs.ClubCount = 0
	if _, err := Generate(defs, 42); err == nil {
		t.Fatal("Generate accepted invalid definitions")
	}
}
