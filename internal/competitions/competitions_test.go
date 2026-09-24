package competitions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

var league1 = SeasonRef{Competition: 1, Season: 1}

// weekly is the default timing in tests: first kickoff one week after the
// epoch, then every seven days.
var weekly = Timing{FirstKickoff: sim.GameInstant(sim.Week), RoundInterval: sim.Week}

// goldenSeed42 pins the schedule for seed 42, league1 and entrants 1..8. If
// it changes intentionally, bump ScheduleVersion and update this value.
const goldenSeed42 = "26a343e03bee4e968e90f9d02b940e2fdaa9bd1005bf59346ca489fe8ceaf2ad"

func teams(idsIn ...ids.TeamID) []ids.TeamID { return idsIn }

func eight() []ids.TeamID { return teams(1, 2, 3, 4, 5, 6, 7, 8) }

func create(t *testing.T, seed random.Seed, ref SeasonRef, entrants []ids.TeamID) *Store {
	t.Helper()
	s := New()
	if err := s.CreateLeagueSeason(seed, ref, entrants, weekly); err != nil {
		t.Fatal(err)
	}
	return s
}

func fingerprint(fixtures []Fixture) string {
	h := sha256.New()
	for _, f := range fixtures {
		fmt.Fprintf(h, "%d %d/%d r%d %d-%d\n", f.ID, f.Season.Competition, f.Season.Season, f.Round, f.Home, f.Away)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// assertLeagueInvariants checks the required properties directly, without
// using checkDoubleRoundRobin.
func assertLeagueInvariants(t *testing.T, entrants []ids.TeamID, fixtures []Fixture) {
	t.Helper()
	if len(fixtures) != 56 {
		t.Fatalf("got %d fixtures, want 56", len(fixtures))
	}
	isEntrant := map[ids.TeamID]bool{}
	for _, e := range entrants {
		isEntrant[e] = true
	}
	perRound := map[Round]int{}
	playsInRound := map[Round]map[ids.TeamID]int{}
	home, away := map[ids.TeamID]int{}, map[ids.TeamID]int{}
	venues := map[[2]ids.TeamID]int{} // ordered (home, away)
	seenID := map[ids.FixtureID]bool{}
	for i, f := range fixtures {
		if i > 0 && (f.Round < fixtures[i-1].Round || (f.Round == fixtures[i-1].Round && f.ID <= fixtures[i-1].ID)) {
			t.Fatalf("fixtures not ordered by round then ID at index %d", i)
		}
		if f.ID == 0 || seenID[f.ID] {
			t.Fatalf("fixture ID %d zero or duplicated", f.ID)
		}
		seenID[f.ID] = true
		if f.Home == f.Away {
			t.Fatalf("self-match %+v", f)
		}
		if !isEntrant[f.Home] || !isEntrant[f.Away] {
			t.Fatalf("unknown entrant in %+v", f)
		}
		perRound[f.Round]++
		if playsInRound[f.Round] == nil {
			playsInRound[f.Round] = map[ids.TeamID]int{}
		}
		playsInRound[f.Round][f.Home]++
		playsInRound[f.Round][f.Away]++
		home[f.Home]++
		away[f.Away]++
		venues[[2]ids.TeamID{f.Home, f.Away}]++
	}
	if len(perRound) != 14 {
		t.Fatalf("got %d rounds, want 14", len(perRound))
	}
	for r := Round(1); r <= 14; r++ {
		if perRound[r] != 4 {
			t.Fatalf("round %d has %d fixtures, want 4", r, perRound[r])
		}
		for _, e := range entrants {
			if playsInRound[r][e] != 1 {
				t.Fatalf("team %d plays %d times in round %d", e, playsInRound[r][e], r)
			}
		}
	}
	for _, a := range entrants {
		if home[a] != 7 || away[a] != 7 {
			t.Fatalf("team %d has %d home and %d away", a, home[a], away[a])
		}
		for _, b := range entrants {
			if a != b && venues[[2]ids.TeamID{a, b}] != 1 {
				t.Fatalf("%d hosts %d %d times, want 1", a, b, venues[[2]ids.TeamID{a, b}])
			}
		}
	}
}

func TestScheduleInvariantsAcrossSeeds(t *testing.T) {
	entrantSets := [][]ids.TeamID{eight(), teams(40, 3, 17, 9, 1000, 2, 58, 11)}
	for _, entrants := range entrantSets {
		for seed := range random.Seed(200) {
			for _, ref := range []SeasonRef{league1, {1, 2}, {7, 3}} {
				s := create(t, seed*7919, ref, entrants)
				assertLeagueInvariants(t, entrants, s.Fixtures(ref))
				if err := s.Validate(); err != nil {
					t.Fatalf("seed %d %s: %v", seed, ref, err)
				}
			}
		}
	}
}

func TestScheduleIsDeterministic(t *testing.T) {
	a := create(t, 42, league1, eight()).Fixtures(league1)
	b := create(t, 42, league1, eight()).Fixtures(league1)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("identical inputs produced different schedules")
	}
	if got := fingerprint(a); got != goldenSeed42 {
		t.Fatalf("seed 42 schedule fingerprint = %s, want %s\n"+
			"schedule changed: fix the regression or bump ScheduleVersion and update goldenSeed42", got, goldenSeed42)
	}
}

func TestEntrantOrderDoesNotAffectSchedule(t *testing.T) {
	want := create(t, 42, league1, eight()).Fixtures(league1)
	for _, order := range [][]ids.TeamID{
		teams(8, 7, 6, 5, 4, 3, 2, 1),
		teams(3, 8, 1, 6, 2, 7, 4, 5),
	} {
		if got := create(t, 42, league1, order).Fixtures(league1); !reflect.DeepEqual(got, want) {
			t.Fatalf("entrant order %v changed the schedule", order)
		}
	}
}

func TestStreamDependsOnSeedAndSeason(t *testing.T) {
	base := fingerprint(stripIDs(create(t, 42, league1, eight()).Fixtures(league1)))
	for name, s := range map[string][]Fixture{
		"seed":        create(t, 43, league1, eight()).Fixtures(league1),
		"season":      create(t, 42, SeasonRef{1, 2}, eight()).Fixtures(SeasonRef{1, 2}),
		"competition": create(t, 42, SeasonRef{2, 1}, eight()).Fixtures(SeasonRef{2, 1}),
	} {
		if fingerprint(stripIDs(s)) == base {
			t.Errorf("changing %s did not change the pairings", name)
		}
	}
}

// stripIDs keeps only round and venue data so schedules for different season
// refs can be compared.
func stripIDs(fs []Fixture) []Fixture {
	out := slices.Clone(fs)
	for i := range out {
		out[i].ID, out[i].Season = 0, SeasonRef{}
	}
	return out
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		ref      SeasonRef
		entrants []ids.TeamID
	}{
		"seven entrants":      {league1, teams(1, 2, 3, 4, 5, 6, 7)},
		"nine entrants":       {league1, teams(1, 2, 3, 4, 5, 6, 7, 8, 9)},
		"no entrants":         {league1, nil},
		"zero team ID":        {league1, teams(0, 2, 3, 4, 5, 6, 7, 8)},
		"duplicate entrant":   {league1, teams(1, 2, 3, 4, 5, 6, 7, 1)},
		"zero competition ID": {SeasonRef{0, 1}, eight()},
		"zero season":         {SeasonRef{1, 0}, eight()},
	}
	for name, c := range cases {
		s := New()
		if err := s.CreateLeagueSeason(42, c.ref, c.entrants, weekly); err == nil {
			t.Errorf("%s: CreateLeagueSeason succeeded, want error", name)
		}
		if len(s.Seasons()) != 0 {
			t.Errorf("%s: failed create changed the store", name)
		}
	}
}

func TestDuplicateSeasonIsRejectedAndStoreUnchanged(t *testing.T) {
	s := create(t, 42, league1, eight())
	before := s.Fixtures(league1)
	if err := s.CreateLeagueSeason(99, league1, teams(11, 12, 13, 14, 15, 16, 17, 18), weekly); err == nil {
		t.Fatal("duplicate season accepted")
	}
	if !reflect.DeepEqual(s.Fixtures(league1), before) || len(s.Seasons()) != 1 {
		t.Fatal("rejected create changed the store")
	}
}

func TestFixtureIDsAreUniqueAcrossSeasons(t *testing.T) {
	s := create(t, 42, league1, eight())
	next := SeasonRef{Competition: 1, Season: 2}
	if err := s.CreateLeagueSeason(42, next, eight(), weekly); err != nil {
		t.Fatal(err)
	}
	first, second := s.Fixtures(league1), s.Fixtures(next)
	if first[0].ID != 1 || first[55].ID != 56 || second[0].ID != 57 || second[55].ID != 112 {
		t.Fatalf("fixture IDs not allocated sequentially across seasons: %d..%d, %d..%d",
			first[0].ID, first[55].ID, second[0].ID, second[55].ID)
	}
	for _, f := range slices.Concat(first, second) {
		if got, ok := s.Fixture(f.ID); !ok || got != f {
			t.Fatalf("Fixture(%d) = %+v, %v; want %+v", f.ID, got, ok, f)
		}
	}
	if _, ok := s.Fixture(113); ok {
		t.Fatal("Fixture found an unallocated ID")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCallerSlicesAreNotMutatedOrRetained(t *testing.T) {
	in := teams(8, 3, 5, 1, 7, 2, 6, 4)
	orig := slices.Clone(in)
	s := create(t, 42, league1, in)
	if !slices.Equal(in, orig) {
		t.Fatalf("input slice mutated: %v", in)
	}
	want := s.Fixtures(league1)
	in[0] = 99
	if !reflect.DeepEqual(s.Fixtures(league1), want) {
		t.Fatal("store changed after caller modified input")
	}
	if e, _ := s.Entrants(league1); !slices.Equal(e, eight()) {
		t.Fatalf("Entrants = %v, want canonical 1..8", e)
	}

	e, _ := s.Entrants(league1)
	e[0] = 99
	fs := s.Fixtures(league1)
	fs[0].Home = 99
	if again, _ := s.Entrants(league1); again[0] != 1 {
		t.Fatal("store changed via returned entrants")
	}
	if !reflect.DeepEqual(s.Fixtures(league1), want) {
		t.Fatal("store changed via returned fixtures")
	}
}

func TestCheckRejectsBrokenSchedules(t *testing.T) {
	good := create(t, 42, league1, eight()).Fixtures(league1)
	if err := checkDoubleRoundRobin(eight(), good); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]Fixture) []Fixture{
		"missing fixture": func(f []Fixture) []Fixture { return f[1:] },
		"self-match":      func(f []Fixture) []Fixture { f[0].Away = f[0].Home; return f },
		"unknown team":    func(f []Fixture) []Fixture { f[0].Home = 99; return f },
		"zero fixture ID": func(f []Fixture) []Fixture { f[0].ID = 0; return f },
		"duplicate ID":    func(f []Fixture) []Fixture { f[1].ID = f[0].ID; return f },
		"round 0":         func(f []Fixture) []Fixture { f[0].Round = 0; return f },
		"round 15":        func(f []Fixture) []Fixture { f[0].Round = 15; return f },
		"venues not swapped": func(f []Fixture) []Fixture {
			// Make the return leg of the first meeting a repeat of the first leg.
			for i := range f {
				if f[i].Home == f[0].Away && f[i].Away == f[0].Home {
					f[i].Home, f[i].Away = f[0].Home, f[0].Away
				}
			}
			return f
		},
		"team twice in round": func(f []Fixture) []Fixture {
			f[1].Round = f[0].Round
			f[1].Home, f[1].Away = f[0].Home, f[0].Away+1
			return f
		},
	}
	for name, corrupt := range cases {
		if err := checkDoubleRoundRobin(eight(), corrupt(slices.Clone(good))); err == nil {
			t.Errorf("%s: check accepted a broken schedule", name)
		}
	}
}

// Venue balance is not a hard requirement, but long home or away runs make a
// poor league. The circle method keeps them short for every seed.
func TestHomeAwayRunsAreShort(t *testing.T) {
	for seed := range random.Seed(50) {
		fixtures := create(t, seed, league1, eight()).Fixtures(league1)
		venues := map[ids.TeamID][]bool{} // true = home, in round order
		for _, f := range fixtures {
			venues[f.Home] = append(venues[f.Home], true)
			venues[f.Away] = append(venues[f.Away], false)
		}
		for team, v := range venues {
			for _, half := range [][]bool{v[:7], v[7:]} {
				if n := longestRun(half); n > 2 {
					t.Fatalf("seed %d team %d: %d-game venue run within a half", seed, team, n)
				}
			}
			if n := longestRun(v); n > 3 {
				t.Fatalf("seed %d team %d: %d-game venue run", seed, team, n)
			}
		}
	}
}

func longestRun(v []bool) int {
	best, cur := 0, 0
	for i := range v {
		if i > 0 && v[i] == v[i-1] {
			cur++
		} else {
			cur = 1
		}
		best = max(best, cur)
	}
	return best
}
