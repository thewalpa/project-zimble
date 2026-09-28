package worldgen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
	"strings"
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
// Last changed by worldgen v6 and content v7 (the five match attributes,
// drawn after every earlier draw; see TestMatchAttributesKeepEarlierDraws).
const goldenSeed42 = "97707b41d757f481c66c2e4f5161d9886ce31f7f3051b1add1d69b34779568bc"

// goldenV5Seed42 was goldenSeed42 before the match attributes: worldgen v5,
// content v6, six attributes per player.
const goldenV5Seed42 = "c40c7f78e6e9a7c9c302a3396cbfaa3b0ce1dcdaabc34d95756b33941c7ad123"

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

// The match attributes are drawn last in each player's stream, so every
// earlier draw is unchanged: the snapshot without them, encoded as worldgen
// v5 did, is exactly the v5 world.
func TestMatchAttributesKeepEarlierDraws(t *testing.T) {
	s := generate(t, 42)
	s.GeneratorVersion, s.ContentVersion = 5, 6
	h := sha256.New()
	var body bytes.Buffer
	s.writeCanonical(&body)
	for line := range strings.Lines(body.String()) {
		if strings.HasPrefix(line, "profile ") {
			// "profile 1 pos=1 attrs=[a b c d e f g h i j k]": keep six.
			head, attrs, _ := strings.Cut(line, "attrs=[")
			fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(attrs), "]"))
			line = head + "attrs=[" + strings.Join(fields[:matchAttributes], " ") + "]\n"
		}
		h.Write([]byte(line))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != goldenV5Seed42 {
		t.Fatalf("seed 42 without the match attributes = %s, want the v5 world %s", got, goldenV5Seed42)
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
		if len(s.Clubs) != 32 || len(s.Teams) != 32 || len(s.Players) != 640 {
			t.Fatalf("seed %d: clubs=%d teams=%d players=%d", seed, len(s.Clubs), len(s.Teams), len(s.Players))
		}
		if len(s.Profiles) != 640 || len(s.Assignments) != 640 {
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
	defs.Nations = nil
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
	// Youth v2 appended the match attributes: names, birth dates and the
	// first six attributes are those youth v1 generated.
	for _, v1 := range []struct {
		id          ids.PlayerID
		pos         players.Position
		first, last string
		born        sim.GameInstant
		attrs       [6]players.Rating
	}{
		{641, players.Goalkeeper, "Yannick", "Tanaka", -5573760, [6]players.Rating{61, 5, 42, 2, 12, 40}},
		{678, players.Defender, "Hugo", "Varga", -5612640, [6]players.Rating{1, 61, 33, 12, 25, 25}},
		{715, players.Midfielder, "Jonas", "Moreau", -6128160, [6]players.Rating{1, 38, 60, 17, 28, 73}},
		{752, players.Forward, "Viktor", "Moreau", -6125280, [6]players.Rating{1, 13, 19, 47, 51, 20}},
	} {
		p, prof, err := Youth(defs, 42, v1.id, v1.pos, 3_000_000)
		if err != nil || p.FirstName != v1.first || p.LastName != v1.last || p.Born != v1.born || [6]players.Rating(prof.Attributes[:6]) != v1.attrs {
			t.Errorf("youth %d changed its v1 draws: %+v %v (%v)", v1.id, p, prof.Attributes, err)
		}
	}
	if _, _, err := Youth(defs, 42, 500, 0, at); err == nil {
		t.Fatal("youth at no position accepted")
	}
	if _, _, err := Youth(defs, 42, 0, players.Forward, at); err == nil {
		t.Fatal("youth with ID 0 accepted")
	}
}

// Lower divisions and further nations add clubs after those before them
// without changing them: a world with only the top divisions is exactly the
// first sixteen clubs (and 320 players) of the full one, a one-nation world
// exactly the first eight, and each club comes from the towns of its own
// nation and division.
func TestDivisionsAreGeneratedIndependently(t *testing.T) {
	defs := content.Default()
	topOnly := defs.Clone()
	for i := range topOnly.Nations {
		topOnly.Nations[i].Divisions = topOnly.Nations[i].Divisions[:1]
	}
	oneNation := topOnly.Clone()
	oneNation.Nations = oneNation.Nations[:1]
	for _, seed := range []random.Seed{1, 42} {
		full, err := Generate(defs, seed)
		if err != nil {
			t.Fatal(err)
		}
		for _, sub := range []struct {
			name  string
			defs  content.Definitions
			clubs int
		}{{"top divisions", topOnly, 16}, {"first nation's top division", oneNation, 8}} {
			part, err := Generate(sub.defs, seed)
			if err != nil {
				t.Fatal(err)
			}
			n := len(part.Players)
			if len(part.Clubs) != sub.clubs || !reflect.DeepEqual(full.Clubs[:sub.clubs], part.Clubs) || !reflect.DeepEqual(full.Players[:n], part.Players) ||
				!reflect.DeepEqual(full.Profiles[:n], part.Profiles) || !reflect.DeepEqual(full.Contracts[:n], part.Contracts) {
				t.Fatalf("seed %d: later clubs changed the %s", seed, sub.name)
			}
		}
		// Clubs 1-8 and 9-16 are the top divisions, 17-24 and 25-32 the second.
		for i, c := range full.Clubs {
			division := defs.Nations[i/8%2].Divisions[i/16]
			if !slices.ContainsFunc(division.Towns, func(tw content.Town) bool { return tw.Short == c.ShortName }) {
				t.Fatalf("seed %d: club %d (%s) is not from nation %d's division %d", seed, c.ID, c.Name, i/8%2+1, i/16+1)
			}
		}
	}
}
