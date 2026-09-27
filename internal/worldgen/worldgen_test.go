package worldgen

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/players"
)

// goldenSeed42 pins the output of Generate(content.Default(), 42). If this
// test fails, either fix the regression or, when the change is intended, bump
// worldgen.Version (or content.Version / random.Version) and update the value.
// Last changed by worldgen v3 and content v4 (birth dates; names, attributes
// and contracts are unchanged since v2).
const goldenSeed42 = "5682a824c61455c1042ab93fbda2f81eea50e990f2744ede9dad01fcf5792bae"

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

// Every player gets contract terms within the economy's ranges; wages follow
// the formula within the variation band.
func TestContractTermsFollowTheEconomy(t *testing.T) {
	defs := content.Default()
	s := generate(t, 42)
	econ := defs.Economy
	if len(s.Contracts) != len(s.Players) {
		t.Fatalf("%d contracts for %d players", len(s.Contracts), len(s.Players))
	}
	years := map[int]int{}
	for i, c := range s.Contracts {
		p := s.Profiles[i]
		lo, hi := econ.Wage(p.Overall(), -econ.WageVariationPct), econ.Wage(p.Overall(), econ.WageVariationPct)
		if c.Player != p.Player || c.Years < econ.ContractYears[0] || c.Years > econ.ContractYears[1] || c.WeeklyWage < lo || c.WeeklyWage > hi {
			t.Fatalf("player %d terms %+v (overall %d, wage band %s..%s)", c.Player, c, p.Overall(), lo, hi)
		}
		years[c.Years]++
	}
	if len(years) != econ.ContractYears[1]-econ.ContractYears[0]+1 {
		t.Fatalf("contract lengths %v do not cover the range", years)
	}
}

// Every generated player is defs.Ages old at the career start, whatever the
// epoch, and the whole range occurs.
func TestGeneratedAges(t *testing.T) {
	defs := content.Default()
	for _, epoch := range []sim.CivilTime{{Year: 2025, Month: 7, Day: 1}, {Year: 2028, Month: 2, Day: 29}, {Year: 1999, Month: 12, Day: 31, Hour: 23, Minute: 59}} {
		cal, err := sim.NewCalendar(epoch)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int]bool{}
		for _, seed := range []uint64{1, 42, 99} {
			for _, p := range generate(t, seed).Players {
				age, err := cal.WholeYears(p.Born, 0)
				if err != nil || age < defs.Ages[0] || age > defs.Ages[1] {
					t.Fatalf("epoch %s seed %d: player %d is %d (%v), want %v", epoch, seed, p.ID, age, err, defs.Ages)
				}
				seen[age] = true
			}
		}
		if len(seen) != defs.Ages[1]-defs.Ages[0]+1 {
			t.Fatalf("epoch %s: ages %v do not cover %v", epoch, seen, defs.Ages)
		}
	}
}

func TestYouth(t *testing.T) {
	defs := content.Default()
	cal, err := sim.NewCalendar(sim.CivilTime{Year: 2025, Month: 7, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := cal.Instant(sim.CivilTime{Year: 2031, Month: 6, Day: 30})
	a, pa, err := Youth(defs, 42, 500, players.Defender, at)
	if err != nil {
		t.Fatal(err)
	}
	b, pb, _ := Youth(defs, 42, 500, players.Defender, at)
	if a != b || pa != pb {
		t.Fatal("youth generation is not repeatable")
	}
	if c, _, _ := Youth(defs, 42, 501, players.Defender, at); c.FirstName+c.LastName == a.FirstName+a.LastName && c.Born == a.Born {
		t.Fatal("another ID produced the same youth")
	}
	for id := ids.PlayerID(161); id <= 400; id++ {
		for _, pos := range players.Positions() {
			p, prof, err := Youth(defs, 7, id, pos, at)
			if err != nil {
				t.Fatal(err)
			}
			if p.ID != id || prof.Player != id || prof.Position != pos || prof.Retired || p.FirstName == "" || p.LastName == "" {
				t.Fatalf("youth %d: %+v %+v", id, p, prof)
			}
			if age, _ := cal.WholeYears(p.Born, at); age < defs.Youth.Ages[0] || age > defs.Youth.Ages[1] {
				t.Fatalf("youth %d is %d, want %v", id, age, defs.Youth.Ages)
			}
			base, _ := defs.Profile(pos)
			for a, r := range prof.Attributes {
				if rg := defs.Youth.Range(base.Ranges[a]); r < rg.Min || r > rg.Max {
					t.Fatalf("youth %d %s=%d outside %v", id, players.Attribute(a), r, rg)
				}
			}
		}
	}
	if _, _, err := Youth(defs, 42, 500, 0, at); err == nil {
		t.Fatal("youth at no position accepted")
	}
	if _, _, err := Youth(defs, 42, 0, players.Forward, at); err == nil {
		t.Fatal("youth with ID 0 accepted")
	}
}
