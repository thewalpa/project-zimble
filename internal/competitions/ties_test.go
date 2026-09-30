package competitions

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

var play1 = SeasonRef{Competition: PlayoffBase, Season: 1}

// tiesEntrants is the flattened pair order used in tests: 11 v 18, 12 v 17.
func tiesEntrants() []ids.TeamID { return teams(11, 18, 12, 17) }

func ties(t *testing.T) *Store {
	t.Helper()
	s := New()
	if err := s.CreateSeasons(1, []NewSeason{{Ref: play1, Format: FormatTies, Entrants: tiesEntrants(), Timing: weekly}}); err != nil {
		t.Fatal(err)
	}
	return s
}

// playTiesRound begins and completes play1's round with the given score per
// fixture (in fixture order).
func playTiesRound(t *testing.T, s *Store, scores ...Score) {
	t.Helper()
	ref := RoundRef{Season: play1, Round: 1}
	info, _ := s.Round(ref)
	if err := s.BeginRounds([]RoundRef{ref}, info.Kickoff); err != nil {
		t.Fatal(err)
	}
	for i := range scores {
		scores[i].Fixture = info.Fixtures[i]
	}
	if err := s.CompleteRounds([]RoundRef{ref}, scores, info.Kickoff); err != nil {
		t.Fatal(err)
	}
}

// A ties season is one round of parallel ties in entrant order: the pairs'
// first team at home, every tie at the single kickoff. Its ranking puts the
// winners first in tie order (a penalty winner counts), then the rest; there
// is no champion.
func TestTiesSeason(t *testing.T) {
	s := ties(t)
	if f, _ := s.Format(play1); f != FormatTies {
		t.Fatalf("format %s", f)
	}
	rounds := s.Rounds(play1)
	if len(rounds) != 1 || rounds[0].Status != RoundScheduled {
		t.Fatalf("rounds %v", rounds)
	}
	fs := s.Fixtures(play1)
	want := [][2]ids.TeamID{{11, 18}, {12, 17}}
	if got := [][2]ids.TeamID{{fs[0].Home, fs[0].Away}, {fs[1].Home, fs[1].Away}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairs %v, want %v", got, want)
	}
	if fs[0].Kickoff != weekly.FirstKickoff || fs[1].Kickoff != weekly.FirstKickoff {
		t.Fatalf("kickoffs %d, %d, want both %d", fs[0].Kickoff, fs[1].Kickoff, weekly.FirstKickoff)
	}
	// 11 wins in regulation; 17 on penalties.
	playTiesRound(t, s, Score{HomeGoals: 2, AwayGoals: 0}, Score{HomeGoals: 1, AwayGoals: 1, HomePenalties: 3, AwayPenalties: 5})
	if !s.SeasonCompleted(play1) {
		t.Fatal("season not completed after its only round")
	}
	if got, want := s.Ranking(play1), teams(11, 17, 18, 12); !slices.Equal(got, want) {
		t.Fatalf("ranking %v, want %v", got, want)
	}
	if _, ok := s.Champion(play1); ok {
		t.Fatal("a ties season has a champion")
	}
	if s.Standings(play1) != nil {
		t.Fatal("a ties season has a table")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTiesResultRules(t *testing.T) {
	cases := map[string]Score{
		"level without penalties":  {HomeGoals: 1, AwayGoals: 1},
		"level after penalties":    {HomeGoals: 1, AwayGoals: 1, HomePenalties: 4, AwayPenalties: 4},
		"penalties after a winner": {HomeGoals: 2, AwayGoals: 1, HomePenalties: 4, AwayPenalties: 3},
	}
	for name, bad := range cases {
		s := ties(t)
		ref := RoundRef{Season: play1, Round: 1}
		info, _ := s.Round(ref)
		if err := s.BeginRounds([]RoundRef{ref}, info.Kickoff); err != nil {
			t.Fatal(err)
		}
		before := s.Snapshot()
		scores := []Score{bad, {HomeGoals: 1}}
		for i := range scores {
			scores[i].Fixture = info.Fixtures[i]
		}
		if err := s.CompleteRounds([]RoundRef{ref}, scores, info.Kickoff); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if !reflect.DeepEqual(s.Snapshot(), before) {
			t.Errorf("%s: a rejected result changed the store", name)
		}
	}
}

func TestTiesCreationRejections(t *testing.T) {
	for name, entrants := range map[string][]ids.TeamID{
		"odd count":  teams(1, 2, 3),
		"one team":   teams(1),
		"duplicate":  teams(1, 2, 2, 3),
		"zero team":  teams(1, 0),
		"no entrant": nil,
	} {
		s := New()
		if err := s.CreateSeasons(1, []NewSeason{{Ref: play1, Format: FormatTies, Entrants: entrants, Timing: weekly}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The batch stays atomic: a bad ties season leaves a good league unstored.
	s := New()
	err := s.CreateSeasons(1, []NewSeason{
		{Ref: league1, Format: FormatLeague, Entrants: eight(), Timing: weekly},
		{Ref: play1, Format: FormatTies, Entrants: teams(1, 2, 3), Timing: weekly},
	})
	if err == nil || len(s.Seasons()) != 0 {
		t.Fatal("invalid ties entrants accepted, or the batch was not atomic")
	}
}

func TestTiesSnapshots(t *testing.T) {
	s := ties(t)
	playTiesRound(t, s, Score{HomeGoals: 1}, Score{AwayGoals: 2})
	snap := s.Snapshot()
	r, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), snap) || !slices.Equal(r.Ranking(play1), s.Ranking(play1)) {
		t.Fatal("restore changed the ties")
	}
	cases := map[string]func(*Snapshot){
		"invalid format": func(s *Snapshot) { s.Seasons[0].Format = 0 },
		"three entrants": func(s *Snapshot) { s.Seasons[0].Entrants = s.Seasons[0].Entrants[:3] },
		"extra round": func(s *Snapshot) {
			s.Seasons[0].Rounds = append(s.Seasons[0].Rounds, RoundSnapshot{Kickoff: 1 << 30, Status: RoundScheduled})
		},
		"pair swapped": func(s *Snapshot) { f := &s.Seasons[0].Fixtures[0]; f.Home, f.Away = f.Away, f.Home },
		"wrong pair":   func(s *Snapshot) { s.Seasons[0].Fixtures[0].Home = 12 },    // 12 is the other home team
		"level result": func(s *Snapshot) { s.Seasons[0].Results[0].AwayGoals = 1 }, // was 1-0
		"penalties after a win": func(s *Snapshot) {
			s.Seasons[0].Results[1].HomePenalties = 3 // was 0-2
		},
	}
	for name, mutate := range cases {
		snap := s.Snapshot()
		mutate(&snap)
		if _, err := Restore(snap); err == nil {
			t.Errorf("%s: restore accepted", name)
		}
	}
	if _, err := Restore(s.Snapshot()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// The round interval is unused: every tie plays at FirstKickoff. Timing still
// needs one, like every other format.
func TestTiesTiming(t *testing.T) {
	s := New()
	bad := weekly
	bad.RoundInterval = 0
	if err := s.CreateSeasons(1, []NewSeason{{Ref: play1, Format: FormatTies, Entrants: tiesEntrants(), Timing: bad}}); err == nil {
		t.Fatal("ties accepted without a round interval")
	}
	at := Timing{FirstKickoff: sim.GameInstant(3 * sim.Day), RoundInterval: 90 * sim.Day}
	if err := s.CreateSeasons(1, []NewSeason{{Ref: play1, Format: FormatTies, Entrants: tiesEntrants(), Timing: at}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fixtures(play1) {
		if f.Kickoff != at.FirstKickoff {
			t.Fatalf("fixture %d kicks off at %d, want %d", f.ID, f.Kickoff, at.FirstKickoff)
		}
	}
}
