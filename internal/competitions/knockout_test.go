package competitions

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

var cup1 = SeasonRef{Competition: 3, Season: 1}

// bracket is the knockout order used in tests: ties 11 v 18, 12 v 17,
// 13 v 16, 14 v 15.
func bracket() []ids.TeamID { return teams(11, 18, 12, 17, 13, 16, 14, 15) }

func knockout(t *testing.T) *Store {
	t.Helper()
	s := New()
	if err := s.CreateSeasons(1, []NewSeason{{Ref: cup1, Format: FormatKnockout, Entrants: bracket(), Timing: weekly}}); err != nil {
		t.Fatal(err)
	}
	return s
}

// playRound begins and completes a round of cup1 with the given score per
// fixture (in fixture order).
func playRound(t *testing.T, s *Store, round Round, scores ...Score) {
	t.Helper()
	ref := RoundRef{Season: cup1, Round: round}
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

func pairsOf(s *Store, round Round) [][2]ids.TeamID {
	var out [][2]ids.TeamID
	for _, f := range s.Fixtures(cup1) {
		if f.Round == round {
			out = append(out, [2]ids.TeamID{f.Home, f.Away})
		}
	}
	return out
}

// A bracket is played round by round: each round's fixtures appear when the
// previous round is complete, pairing its winners in bracket order (penalty
// winners included), with fixture IDs continuing the allocator.
func TestKnockoutBracket(t *testing.T) {
	s := knockout(t)
	if f, _ := s.Format(cup1); f != FormatKnockout || len(s.Rounds(cup1)) != 3 {
		t.Fatalf("format %s, %d rounds", f, len(s.Rounds(cup1)))
	}
	if got := pairsOf(s, 1); !reflect.DeepEqual(got, [][2]ids.TeamID{{11, 18}, {12, 17}, {13, 16}, {14, 15}}) {
		t.Fatalf("first round %v", got)
	}
	if len(pairsOf(s, 2)) != 0 || s.Standings(cup1) != nil {
		t.Fatal("later rounds or a table exist before any result")
	}
	sf, _ := s.Round(RoundRef{Season: cup1, Round: 2})
	if err := s.BeginRounds([]RoundRef{sf.Ref}, sf.Kickoff); err == nil {
		t.Fatal("a round without fixtures kicked off")
	}
	// 18 wins away; 12 on penalties; 13 at home; 15 on penalties.
	playRound(t, s, 1, Score{HomeGoals: 0, AwayGoals: 1}, Score{HomeGoals: 2, AwayGoals: 2, HomePenalties: 5, AwayPenalties: 4},
		Score{HomeGoals: 3, AwayGoals: 1}, Score{HomeGoals: 1, AwayGoals: 1, HomePenalties: 2, AwayPenalties: 4})
	if got := pairsOf(s, 2); !reflect.DeepEqual(got, [][2]ids.TeamID{{18, 12}, {13, 15}}) {
		t.Fatalf("semi-finals %v", got)
	}
	if fs := s.Fixtures(cup1); fs[4].ID != 5 || fs[5].ID != 6 || s.lastFixture != 6 {
		t.Fatalf("semi-final IDs %d, %d", fs[4].ID, fs[5].ID)
	}
	if r, _ := s.Result(2); r.HomePenalties != 5 || r.AwayPenalties != 4 {
		t.Fatalf("result %+v lost its shootout", r)
	}
	if s.SeasonCompleted(cup1) {
		t.Fatal("completed after one round")
	}
	playRound(t, s, 2, Score{HomeGoals: 0, AwayGoals: 0, HomePenalties: 3, AwayPenalties: 1}, Score{HomeGoals: 0, AwayGoals: 2})
	if got := pairsOf(s, 3); !reflect.DeepEqual(got, [][2]ids.TeamID{{18, 15}}) {
		t.Fatalf("final %v", got)
	}
	playRound(t, s, 3, Score{HomeGoals: 1, AwayGoals: 2})
	if c, ok := s.Champion(cup1); !ok || c != 15 {
		t.Fatalf("champion %d, %v", c, ok)
	}
	// Champion, runner-up, semi-final losers, quarter-final losers; each
	// group in bracket order.
	if got, want := s.Ranking(cup1), teams(15, 18, 12, 13, 11, 17, 16, 14); !slices.Equal(got, want) {
		t.Fatalf("ranking %v, want %v", got, want)
	}
	wantExits := []Exit{
		{Team: 15, Round: 3, Stage: 0}, {Team: 18, Round: 3, Stage: 1},
		{Team: 12, Round: 2, Stage: 2}, {Team: 13, Round: 2, Stage: 2},
		{Team: 11, Round: 1, Stage: 3}, {Team: 17, Round: 1, Stage: 3}, {Team: 16, Round: 1, Stage: 3}, {Team: 14, Round: 1, Stage: 3},
	}
	if got, ok := s.Exits(cup1); !ok || !reflect.DeepEqual(got, wantExits) {
		t.Fatalf("exits %+v, %v", got, ok)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Exits exist only for a completed knockout. Whatever the bracket size, one
// champion and one runner-up leave in the final, and 2^(k-1) teams at each
// stage k after that.
func TestKnockoutExits(t *testing.T) {
	s := knockout(t)
	if _, ok := s.Exits(cup1); ok {
		t.Fatal("exits before any result")
	}
	playRound(t, s, 1, Score{HomeGoals: 1}, Score{HomeGoals: 1}, Score{HomeGoals: 1}, Score{HomeGoals: 1})
	if _, ok := s.Exits(cup1); ok {
		t.Fatal("exits of an unfinished bracket")
	}
	if _, ok := s.Exits(SeasonRef{Competition: 9, Season: 1}); ok {
		t.Fatal("exits of an unknown season")
	}
	l := create(t, 1, league1, eight())
	for _, info := range l.Rounds(league1) {
		if err := l.BeginRounds([]RoundRef{info.Ref}, info.Kickoff); err != nil {
			t.Fatal(err)
		}
		if err := l.CompleteRounds([]RoundRef{info.Ref}, scoresFor(l, info.Ref, func(int) (uint16, uint16) { return 1, 0 }), info.Kickoff); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := l.Exits(league1); ok || !l.SeasonCompleted(league1) {
		t.Fatal("a completed league has exits")
	}

	for _, size := range []int{2, 4, 8, 16} {
		s := New()
		entrants := make([]ids.TeamID, size)
		for i := range entrants {
			entrants[i] = ids.TeamID(100 + i)
		}
		if err := s.CreateSeasons(1, []NewSeason{{Ref: cup1, Format: FormatKnockout, Entrants: entrants, Timing: weekly}}); err != nil {
			t.Fatal(err)
		}
		for r := Round(1); int(r) <= len(s.Rounds(cup1)); r++ {
			info, _ := s.Round(RoundRef{Season: cup1, Round: r})
			scores := make([]Score, len(info.Fixtures))
			for i := range scores {
				scores[i] = Score{HomeGoals: uint16(i % 2), AwayGoals: uint16((i + 1) % 2)}
			}
			playRound(t, s, r, scores...)
		}
		exits, ok := s.Exits(cup1)
		if !ok || len(exits) != size {
			t.Fatalf("%d entrants: %d exits, %v", size, len(exits), ok)
		}
		count := map[int]int{}
		for k, e := range exits {
			count[e.Stage]++
			if ranking := s.Ranking(cup1); e.Team != ranking[k] {
				t.Fatalf("%d entrants: exit %d is team %d, ranking has %d", size, k, e.Team, ranking[k])
			}
			if final := len(s.Rounds(cup1)); e.Stage > 0 && int(e.Round) != final-e.Stage+1 {
				t.Fatalf("%d entrants: %+v went out in the wrong round of %d", size, e, final)
			}
		}
		if champion, _ := s.Champion(cup1); exits[0].Team != champion || count[0] != 1 {
			t.Fatalf("%d entrants: champion %+v, %d at stage 0", size, exits[0], count[0])
		}
		for k := 1; k <= len(s.Rounds(cup1)); k++ {
			if count[k] != 1<<(k-1) {
				t.Fatalf("%d entrants: %d teams at stage %d", size, count[k], k)
			}
		}
	}
}

func TestKnockoutResultRules(t *testing.T) {
	cases := map[string]Score{
		"level without penalties":   {HomeGoals: 1, AwayGoals: 1},
		"level after penalties":     {HomeGoals: 1, AwayGoals: 1, HomePenalties: 4, AwayPenalties: 4},
		"penalties after a winner":  {HomeGoals: 2, AwayGoals: 1, HomePenalties: 4, AwayPenalties: 3},
		"penalties above the bound": {HomeGoals: 0, AwayGoals: 0, HomePenalties: MaxGoals + 1},
	}
	for name, bad := range cases {
		s := knockout(t)
		ref := RoundRef{Season: cup1, Round: 1}
		info, _ := s.Round(ref)
		if err := s.BeginRounds([]RoundRef{ref}, info.Kickoff); err != nil {
			t.Fatal(err)
		}
		before := s.Snapshot()
		scores := []Score{bad, {HomeGoals: 1}, {HomeGoals: 1}, {HomeGoals: 1}}
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
	// A league result never has a shootout.
	s, ref, at := begun(t)
	scores := scoresFor(s, ref, func(int) (uint16, uint16) { return 1, 1 })
	scores[0].HomePenalties = 3
	if err := s.CompleteRounds([]RoundRef{ref}, scores, at); err == nil {
		t.Fatal("a league result with penalties was accepted")
	}
}

func TestKnockoutCreationRejections(t *testing.T) {
	for name, entrants := range map[string][]ids.TeamID{
		"six teams":  teams(1, 2, 3, 4, 5, 6),
		"one team":   teams(1),
		"duplicate":  teams(1, 2, 3, 3),
		"zero team":  teams(1, 0),
		"no entrant": nil,
	} {
		s := New()
		if err := s.CreateSeasons(1, []NewSeason{{Ref: cup1, Format: FormatKnockout, Entrants: entrants, Timing: weekly}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := New()
	err := s.CreateSeasons(1, []NewSeason{
		{Ref: league1, Format: FormatLeague, Entrants: eight(), Timing: weekly},
		{Ref: cup1, Format: 0, Entrants: bracket(), Timing: weekly},
	})
	if err == nil || len(s.Seasons()) != 0 {
		t.Fatal("a season without a format was accepted, or the batch was not atomic")
	}
}

// Leagues and knockouts created together allocate fixture IDs in
// competition order.
func TestCreateSeasonsMixesFormats(t *testing.T) {
	s := New()
	if err := s.CreateSeasons(1, []NewSeason{
		{Ref: cup1, Format: FormatKnockout, Entrants: bracket(), Timing: weekly},
		{Ref: league1, Format: FormatLeague, Entrants: eight(), Timing: weekly},
	}); err != nil {
		t.Fatal(err)
	}
	if l, c := s.Fixtures(league1), s.Fixtures(cup1); l[len(l)-1].ID != 56 || c[0].ID != 57 {
		t.Fatalf("league ends at fixture %d, cup starts at %d", l[len(l)-1].ID, c[0].ID)
	}
}

func TestKnockoutSnapshots(t *testing.T) {
	s := knockout(t)
	playRound(t, s, 1, Score{HomeGoals: 1}, Score{HomeGoals: 1, AwayGoals: 1, HomePenalties: 3, AwayPenalties: 5},
		Score{AwayGoals: 1}, Score{HomeGoals: 2})
	snap := s.Snapshot()
	r, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), snap) || !slices.Equal(r.Ranking(cup1), s.Ranking(cup1)) {
		t.Fatal("restore changed the bracket")
	}
	cases := map[string]func(*Snapshot){
		"invalid format": func(s *Snapshot) { s.Seasons[0].Format = 0 },
		"six entrants":   func(s *Snapshot) { s.Seasons[0].Entrants = s.Seasons[0].Entrants[:6] },
		"extra round": func(s *Snapshot) {
			s.Seasons[0].Rounds = append(s.Seasons[0].Rounds, RoundSnapshot{Kickoff: 1 << 30, Status: RoundScheduled})
		},
		"semi-final wrong team": func(s *Snapshot) { s.Seasons[0].Fixtures[4].Home = 18 }, // 18 lost to 11
		"semi-final swapped":    func(s *Snapshot) { f := &s.Seasons[0].Fixtures[4]; f.Home, f.Away = f.Away, f.Home },
		"final too early": func(s *Snapshot) {
			f := s.Seasons[0].Fixtures[5]
			f.ID, f.Round, f.Kickoff = 7, 3, s.Seasons[0].Rounds[2].Kickoff
			s.Seasons[0].Fixtures = append(s.Seasons[0].Fixtures, f)
			s.LastFixture = 7
		},
		"semi-finals missing": func(s *Snapshot) { s.Seasons[0].Fixtures = s.Seasons[0].Fixtures[:4] },
		"level result":        func(s *Snapshot) { s.Seasons[0].Results[1].AwayPenalties = 3 },
		"penalties after a win": func(s *Snapshot) {
			s.Seasons[0].Results[0].HomePenalties = 1
		},
		"league with penalties": func(s *Snapshot) {
			s.Seasons[0].Format = FormatLeague
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
