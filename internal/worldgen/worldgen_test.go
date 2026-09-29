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
	"github.com/thewalpa/project-zimble/internal/registry"
)

// goldenSeed42 pins the output of Generate(content.Default(), 42). If this
// test fails, either fix the regression or, when the change is intended, bump
// worldgen.Version (or content.Version / random.Version) and update the value.
// Last changed by worldgen v8 and content v9 (names from each nation's pools,
// drawn after every earlier draw; see TestEarlierDrawsUnchanged).
const goldenSeed42 = "f40190cef9c75f6e14e5ca383abe44e3fe17c8cf0b386369e5f12931c9a1d0d8"

// The worlds of earlier generator versions for seed 42, hashed without the
// names by canonicalAs (version 8 changed every name): goldenV7Seed42 is the
// v7 world (content v8), goldenV6Seed42 the v6 world (content v7, no
// nations) and goldenV5Seed42 the v5 world (content v6, six attributes per
// player).
const (
	goldenV7Seed42 = "d4ef7c38773a27876fe6c82b95c6a71c9c645a8488336ec356f5d386168258c9"
	goldenV6Seed42 = "2e7192faf9e672dd9705252705a255bece1e00f1b3ac9980c7430414b302ebdb"
	goldenV5Seed42 = "52e5590e0fd73f704a242fb828669f903bfaae00ee578e0a91d90dd4b91eee3c"
)

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

// canonicalAs hashes the snapshot's canonical text as generator version
// gen (5..7) encoded it, without the names: from version 7 down, it drops
// the names (v8); from 6 down also the nations and nationalities (v7), and
// at 5 also the match attributes (v6).
func canonicalAs(s Snapshot, gen int) string {
	s.GeneratorVersion, s.ContentVersion = gen, gen+1
	var body bytes.Buffer
	s.writeCanonical(&body)
	h := sha256.New()
	for line := range strings.Lines(body.String()) {
		if strings.HasPrefix(line, "player ") {
			// `player 1 "First" "Last" born=...`: drop the two quoted names.
			id, rest, _ := strings.Cut(strings.TrimPrefix(line, "player "), " ")
			_, rest, _ = strings.Cut(rest, " born=")
			line = "player " + id + " born=" + rest
		}
		switch {
		case gen >= 7:
		case strings.HasPrefix(line, "nation "):
			continue
		case strings.HasPrefix(line, "club ") || strings.HasPrefix(line, "player "):
			head, _, _ := strings.Cut(line, " nation=")
			line = head + "\n"
		case strings.HasPrefix(line, "profile ") && gen <= 5:
			// "profile 1 pos=1 attrs=[a b c d e f g h i j k]": keep six.
			head, attrs, _ := strings.Cut(line, "attrs=[")
			fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(attrs), "]"))
			line = head + "attrs=[" + strings.Join(fields[:matchAttributes], " ") + "]\n"
		}
		h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Names, nationalities and the match attributes are drawn last in each
// player's stream, so every earlier draw is unchanged: the snapshot without
// the names, encoded as worldgen v7 did, is exactly the v7 world, without
// the nationalities too the v6 world, and without the match attributes too
// the v5 world.
func TestEarlierDrawsUnchanged(t *testing.T) {
	s := generate(t, 42)
	for _, g := range []struct {
		gen  int
		want string
	}{{7, goldenV7Seed42}, {6, goldenV6Seed42}, {5, goldenV5Seed42}} {
		if got := canonicalAs(s, g.gen); got != g.want {
			t.Errorf("seed 42 as worldgen v%d = %s, want %s", g.gen, got, g.want)
		}
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
	a, pa, err := Youth(defs, 42, 500, players.Defender, at, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, pb, _ := Youth(defs, 42, 500, players.Defender, at, 1)
	if a != b || pa != pb {
		t.Fatal("youth generation is not repeatable")
	}
	if c, _, _ := Youth(defs, 42, 501, players.Defender, at, 1); c.FirstName+c.LastName == a.FirstName+a.LastName && c.Born == a.Born {
		t.Fatal("another ID produced the same youth")
	}
	foreign, total := 0, 0
	for id := ids.PlayerID(161); id <= 400; id++ {
		for _, pos := range players.Positions() {
			home := NationID(int(id) % len(defs.Nations))
			p, prof, err := Youth(defs, 7, id, pos, at, home)
			if err != nil {
				t.Fatal(err)
			}
			if p.ID != id || prof.Player != id || prof.Position != pos || prof.Retired || p.FirstName == "" || p.LastName == "" {
				t.Fatalf("youth %d: %+v %+v", id, p, prof)
			}
			if p.Nationality < 1 || int(p.Nationality) > len(defs.Nations) || !namedFrom(defs, p) {
				t.Fatalf("youth %d has nationality %d and is named %s %s", id, p.Nationality, p.FirstName, p.LastName)
			}
			total++
			if p.Nationality != home {
				foreign++
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
	// A small share of youth players are foreign (5%: about 48 of 960).
	if foreign < total*defs.Youth.ForeignPct/200 || foreign > total*defs.Youth.ForeignPct*2/100 {
		t.Fatalf("%d of %d youth players are foreign, want about %d%%", foreign, total, defs.Youth.ForeignPct)
	}
	// Youth v2 to v4 appended their draws: birth dates, attributes and
	// nationalities are those youth v3 generated (and the birth dates and
	// first six attributes those of v1), and only the names moved.
	for _, v3 := range []struct {
		id          ids.PlayerID
		pos         players.Position
		born        sim.GameInstant
		attrs       players.Attributes
		nationality ids.NationID
	}{
		{641, players.Goalkeeper, -5573760, players.Attributes{61, 5, 42, 2, 12, 40, 2, 23, 45, 18, 28}, 1},
		{678, players.Defender, -5612640, players.Attributes{1, 61, 33, 12, 25, 25, 35, 30, 46, 48, 43}, 2},
		{715, players.Midfielder, -6128160, players.Attributes{1, 38, 60, 17, 28, 73, 52, 43, 42, 48, 58}, 1},
		{752, players.Forward, -6125280, players.Attributes{1, 13, 19, 47, 51, 20, 45, 45, 44, 60, 26}, 1},
	} {
		p, prof, err := Youth(defs, 42, v3.id, v3.pos, 3_000_000, 1)
		if err != nil || p.Born != v3.born || prof.Attributes != v3.attrs || p.Nationality != v3.nationality {
			t.Errorf("youth %d changed its v3 draws: %+v %v (%v)", v3.id, p, prof.Attributes, err)
		}
		if !namedFrom(defs, p) {
			t.Errorf("youth %d of nation %d is named %s %s", v3.id, p.Nationality, p.FirstName, p.LastName)
		}
	}
	if _, _, err := Youth(defs, 42, 500, 0, at, 1); err == nil {
		t.Fatal("youth at no position accepted")
	}
	if _, _, err := Youth(defs, 42, 0, players.Forward, at, 1); err == nil {
		t.Fatal("youth with ID 0 accepted")
	}
	for _, home := range []ids.NationID{0, ids.NationID(len(defs.Nations) + 1)} {
		if _, _, err := Youth(defs, 42, 500, players.Forward, at, home); err == nil {
			t.Fatalf("youth for nation %d accepted", home)
		}
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
			if len(part.Clubs) != sub.clubs || !reflect.DeepEqual(full.Clubs[:sub.clubs], part.Clubs) || !reflect.DeepEqual(withoutNationality(full.Players[:n]), withoutNationality(part.Players)) ||
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

// withoutNationality returns the players with their nationalities and names
// cleared: who is foreign, and so named from another nation's pools, depends
// on how many nations there are.
func withoutNationality(ps []registry.Player) []registry.Player {
	out := slices.Clone(ps)
	for i := range out {
		out[i].Nationality, out[i].FirstName, out[i].LastName = 0, "", ""
	}
	return out
}

// namedFrom reports whether p's names come from the pools of p's
// nationality.
func namedFrom(defs content.Definitions, p registry.Player) bool {
	n := defs.Nations[p.Nationality-1]
	return slices.Contains(n.FirstNames, p.FirstName) && slices.Contains(n.LastNames, p.LastName)
}

func TestNationalities(t *testing.T) {
	defs := content.Default()
	for _, seed := range []uint64{1, 42} {
		s := generate(t, seed)
		if len(s.Nations) != len(defs.Nations) {
			t.Fatalf("seed %d: %d nations", seed, len(s.Nations))
		}
		for i, n := range s.Nations {
			if n.ID != NationID(i) || n.Name != defs.Nations[i].Name {
				t.Fatalf("seed %d: nation %d is %+v", seed, i, n)
			}
		}
		clubNation := map[ids.ClubID]ids.NationID{}
		for i, c := range s.Clubs {
			// Clubs 1-8 and 17-24 are the first nation's, 9-16 and 25-32 the second's.
			if want := NationID(i / 8 % 2); c.Nation != want {
				t.Fatalf("seed %d: club %d is of nation %d, want %d", seed, c.ID, c.Nation, want)
			}
			clubNation[c.ID] = c.Nation
		}
		foreign := 0
		for i, p := range s.Players {
			if p.Nationality < 1 || int(p.Nationality) > len(s.Nations) {
				t.Fatalf("seed %d: player %d has nationality %d", seed, p.ID, p.Nationality)
			}
			// Names follow the nationality, not the club.
			if !namedFrom(defs, p) {
				t.Fatalf("seed %d: player %d of nation %d is named %s %s", seed, p.ID, p.Nationality, p.FirstName, p.LastName)
			}
			if p.Nationality != clubNation[s.Assignments[i].Club] {
				foreign++
			}
		}
		// 15% of 640 is 96; allow generous sampling noise.
		if foreign < 50 || foreign > 150 {
			t.Fatalf("seed %d: %d of %d players are foreign, want about %d%%", seed, foreign, len(s.Players), defs.ForeignPct)
		}
	}
	// No foreign share, no foreigners.
	none := defs.Clone()
	none.ForeignPct = 0
	s, err := Generate(none, 42)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range s.Players {
		if home := s.Clubs[s.Assignments[i].Club-1].Nation; p.Nationality != home {
			t.Fatalf("player %d is foreign with a 0%% share", p.ID)
		}
	}
	// A foreign share leaves every other draw as it was, and changes only
	// the foreign players' names.
	full := generate(t, 42)
	if !reflect.DeepEqual(withoutNationality(full.Players), withoutNationality(s.Players)) || !reflect.DeepEqual(full.Profiles, s.Profiles) {
		t.Fatal("players changed with the foreign share")
	}
	for i, p := range full.Players {
		if p.Nationality == s.Players[i].Nationality && p != s.Players[i] {
			t.Fatalf("player %d changed with the foreign share though not foreign", p.ID)
		}
	}
}
