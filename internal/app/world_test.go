package app

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

func newWorld(t *testing.T, seed uint64) *World {
	t.Helper()
	w, err := NewWorld(DefaultConfig(random.Seed(seed)))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestNewWorldBuildsValidWorld(t *testing.T) {
	w := newWorld(t, 42)
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	s := w.Summary()
	if s.Clubs != 8 || s.Teams != 8 || s.Players != 160 {
		t.Fatalf("clubs=%d teams=%d players=%d", s.Clubs, s.Teams, s.Players)
	}
	seenTeams := map[ids.TeamID]bool{}
	for i, row := range s.ClubRows {
		if i > 0 && row.ID <= s.ClubRows[i-1].ID {
			t.Fatal("club rows are not in ascending ID order")
		}
		if row.Players != 20 || seenTeams[row.SeniorTeam] {
			t.Fatalf("club %d: players=%d senior team %d reused=%v", row.ID, row.Players, row.SeniorTeam, seenTeams[row.SeniorTeam])
		}
		seenTeams[row.SeniorTeam] = true
		want := []PositionCount{{players.Goalkeeper, 3}, {players.Defender, 7}, {players.Midfielder, 6}, {players.Forward, 4}}
		if !slices.Equal(row.Positions, want) {
			t.Fatalf("club %d positions = %v", row.ID, row.Positions)
		}
	}
}

func TestSummaryIsDeterministic(t *testing.T) {
	a, b := newWorld(t, 42).Summary(), newWorld(t, 42).Summary()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed produced different summaries")
	}
	if a.Fingerprint == newWorld(t, 7).Summary().Fingerprint {
		t.Fatal("different seeds produced the same fingerprint")
	}
}

// Each case corrupts a generated snapshot in a way that one module alone may
// accept, and checks that loading the world rejects it.
func TestLoadRejectsInconsistentWorlds(t *testing.T) {
	cases := map[string]func(*content.Definitions, *worldgen.Snapshot){
		"missing profile": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Profiles = s.Profiles[:len(s.Profiles)-1]
		},
		"profile for unknown player": func(_ *content.Definitions, s *worldgen.Snapshot) {
			p := s.Profiles[0]
			p.Player = 999
			s.Profiles = append(s.Profiles, p)
		},
		"missing assignment": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Assignments = s.Assignments[1:]
		},
		"assignment for unknown player": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Assignments = append(s.Assignments, employment.Assignment{Player: 999, Club: 1, Team: 1})
		},
		"assignment to unknown team": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Assignments[0].Team = 99
		},
		"team belongs to another club": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Assignments[0].Team = 2
		},
		"squad of 19 and 21": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Assignments[0].Club, s.Assignments[0].Team = 2, 2
		},
		"wrong position mix": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Profiles[0].Position = players.Forward
		},
		"wrong club count": func(d *content.Definitions, _ *worldgen.Snapshot) {
			d.ClubCount = 9
		},
		"attribute out of range": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Profiles[0].Attributes[players.Pace] = 21
		},
		"duplicate player ID": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Players[1].ID = s.Players[0].ID
		},
		"club without senior team": func(_ *content.Definitions, s *worldgen.Snapshot) {
			s.Teams = s.Teams[1:]
		},
	}
	for name, corrupt := range cases {
		defs := content.Default()
		snap, err := worldgen.Generate(defs, 42)
		if err != nil {
			t.Fatal(err)
		}
		corrupt(&defs, &snap)
		if _, err := load(defs, []content.League{content.DefaultLeague()}, DefaultEpoch(), snap); err == nil {
			t.Errorf("%s: load succeeded, want error", name)
		}
	}
}

func TestNewWorldSchedulesLeagueForSeniorTeams(t *testing.T) {
	w := newWorld(t, 42)
	sched := w.Schedules()[0]
	if sched.Competition != 1 || sched.Season != 1 || sched.Entrants != 8 || len(sched.Rounds) != 14 {
		t.Fatalf("schedule header = %+v", sched)
	}
	seniorTeams := map[ids.TeamID]bool{}
	for _, row := range w.Summary().ClubRows {
		seniorTeams[row.SeniorTeam] = true
	}
	played := map[ids.TeamID]int{}
	for i, r := range sched.Rounds {
		if r.Round != competitions.Round(i+1) || len(r.Fixtures) != 4 {
			t.Fatalf("round %d: number %d with %d fixtures", i+1, r.Round, len(r.Fixtures))
		}
		for _, f := range r.Fixtures {
			for _, side := range []TeamLabel{f.Home, f.Away} {
				if !seniorTeams[side.Team] || side.ClubName == "" || side.ShortName == "" {
					t.Fatalf("fixture %d has unresolved or non-senior side %+v", f.ID, side)
				}
				played[side.Team]++
			}
		}
	}
	for team := range seniorTeams {
		if played[team] != 14 {
			t.Fatalf("team %d plays %d fixtures, want 14", team, played[team])
		}
	}
	if !reflect.DeepEqual(sched, newWorld(t, 42).Schedules()[0]) {
		t.Fatal("same seed produced different schedules")
	}
}

// Creating the league must not change any generated world data.
func TestCompetitionCreationPreservesGeneratedWorld(t *testing.T) {
	const worldFingerprintSeed42 = "8bc01a116a8c9d787dd1c2791dc304ab74d20c8af96d082245b7d8997723f0c1"
	defs := content.Default()
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	w, err := load(defs, []content.League{content.DefaultLeague()}, DefaultEpoch(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Summary().Fingerprint; got != worldFingerprintSeed42 || snap.Fingerprint() != worldFingerprintSeed42 {
		t.Fatalf("world fingerprint = %s, want %s", got, worldFingerprintSeed42)
	}
	if !reflect.DeepEqual(w.registry.Players(), snap.Players) {
		t.Fatal("player identities differ from the generated snapshot")
	}
	if !reflect.DeepEqual(w.employment.Assignments(), snap.Assignments) {
		t.Fatal("employment differs from the generated snapshot")
	}
	for _, want := range snap.Profiles {
		if got, _ := w.players.Profile(want.Player); got != want {
			t.Fatalf("player %d profile %+v, want %+v", want.Player, got, want)
		}
	}
}

func TestValidateRejectsBadLeagueEntrants(t *testing.T) {
	cases := map[string][]ids.TeamID{
		"unknown team, club 8 missing": {1, 2, 3, 4, 5, 6, 7, 99},
	}
	for name, entrants := range cases {
		w := newWorld(t, 42)
		w.competitions = competitions.New()
		timing := competitions.Timing{FirstKickoff: 100, RoundInterval: sim.Week}
		if err := w.competitions.CreateLeagueSeason(42, w.leagues[0].season, entrants, timing); err != nil {
			t.Fatal(err)
		}
		if err := w.Validate(); err == nil {
			t.Errorf("%s: Validate succeeded, want error", name)
		}
	}

	w := newWorld(t, 42)
	w.competitions = competitions.New()
	if err := w.Validate(); err == nil {
		t.Error("missing league season: Validate succeeded, want error")
	}
}

func TestLoadRejectsInvalidLeagueDefinition(t *testing.T) {
	defs := content.Default()
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	for name, league := range map[string]content.League{
		"zero ID":        {ID: 0, Name: "L", Entrants: 8},
		"wrong entrants": {ID: 1, Name: "L", Entrants: 6},
	} {
		if _, err := load(defs, []content.League{league}, DefaultEpoch(), snap); err == nil {
			t.Errorf("%s: load succeeded, want error", name)
		}
	}
}
