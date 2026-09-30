package competitions

import (
	"maps"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func ranking(from, n int) []ids.TeamID {
	var r []ids.TeamID
	for i := range n {
		r = append(r, ids.TeamID(from+i))
	}
	return r
}

func TestNextEntrantsSwapsAndKeepsSizes(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8), 3: ranking(201, 8)}
	links := []Link{{Upper: 1, Lower: 2, Places: 2}, {Upper: 2, Lower: 3, Places: 1}}
	got, err := NextEntrants(rankings, links)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ids.CompetitionID][]ids.TeamID{
		1: {1, 2, 3, 4, 5, 6, 101, 102},
		// League 2 loses its top two (to 1) and bottom one (to 3), and
		// gains league 1's bottom two and league 3's champion.
		2: {7, 8, 103, 104, 105, 106, 107, 201},
		3: {108, 202, 203, 204, 205, 206, 207, 208},
	}
	for comp, w := range want {
		if !slices.Equal(got[comp], w) {
			t.Errorf("league %d = %v, want %v", comp, got[comp], w)
		}
	}
	total := 0
	for _, e := range got {
		total += len(e)
	}
	if total != 24 {
		t.Errorf("%d teams after movement, want 24", total)
	}
}

func TestNextEntrantsNoLinksKeepsTeams(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: {3, 1, 2}, 2: {6, 5, 4}}
	got, err := NextEntrants(rankings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[1], []ids.TeamID{1, 2, 3}) || !slices.Equal(got[2], []ids.TeamID{4, 5, 6}) {
		t.Errorf("got %v", got)
	}
	if !maps.EqualFunc(rankings, map[ids.CompetitionID][]ids.TeamID{1: {3, 1, 2}, 2: {6, 5, 4}}, slices.Equal) {
		t.Error("input rankings were modified")
	}
}

func TestNextEntrantsRejects(t *testing.T) {
	ok := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8)}
	cases := map[string]struct {
		rankings map[ids.CompetitionID][]ids.TeamID
		links    []Link
	}{
		"missing league":  {ok, []Link{{Upper: 1, Lower: 9, Places: 1}}},
		"same league":     {ok, []Link{{Upper: 1, Lower: 1, Places: 1}}},
		"zero places":     {ok, []Link{{Upper: 1, Lower: 2}}},
		"too many places": {ok, []Link{{Upper: 1, Lower: 2, Places: 5}}},
		"reused end":      {ok, []Link{{Upper: 1, Lower: 2, Places: 1}, {Upper: 1, Lower: 2, Places: 1}}},
		"shared team":     {map[ids.CompetitionID][]ids.TeamID{1: {1, 2}, 2: {2, 3}}, nil},
	}
	for name, c := range cases {
		if _, err := NextEntrants(c.rankings, c.links); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPlayoffCompetitionsAssignsIDsInLinkOrder(t *testing.T) {
	links := []Link{{Upper: 2, Lower: 5, Places: 2}, {Upper: 1, Lower: 4, Places: 1}}
	got := PlayoffCompetitions(links)
	want := map[ids.CompetitionID]Link{
		PlayoffBase:     {Upper: 1, Lower: 4, Places: 1},
		PlayoffBase + 1: {Upper: 2, Lower: 5, Places: 2},
	}
	if !maps.EqualFunc(got, want, func(a, b Link) bool { return a == b }) {
		t.Errorf("PlayoffCompetitions = %v, want %v", got, want)
	}
}

func TestPlayoffPairingsBottomHomeAgainstTop(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8)}
	got, err := PlayoffPairings(Link{Upper: 1, Lower: 2, Places: 2}, rankings)
	if err != nil {
		t.Fatal(err)
	}
	// Tie order: the deepest relegated team hosts the best challenger first.
	want := [][2]ids.TeamID{{8, 101}, {7, 102}}
	if !slices.EqualFunc(got, want, func(a, b [2]ids.TeamID) bool { return a == b }) {
		t.Errorf("pairings %v, want %v", got, want)
	}
	for name, l := range map[string]Link{
		"missing league":  {Upper: 1, Lower: 9, Places: 1},
		"same league":     {Upper: 1, Lower: 1, Places: 1},
		"zero places":     {Upper: 1, Lower: 2},
		"too many places": {Upper: 1, Lower: 2, Places: 5},
	} {
		if _, err := PlayoffPairings(l, rankings); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestApplyPlayoffsMovesWinnersUp(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8)}
	links := []Link{{Upper: 1, Lower: 2, Places: 2}}
	// 101 wins at 8; 7 survives at home to 102. Pairs are {8,101} and {7,102}.
	winners := map[Link][]ids.TeamID{{Upper: 1, Lower: 2, Places: 2}: {101, 7}}
	got, err := NextEntrantsAfterPlayoffs(rankings, links, winners)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ids.CompetitionID][]ids.TeamID{
		1: {1, 2, 3, 4, 5, 6, 7, 101},             // 101 up as a winner; 7 saved by winning
		2: {8, 102, 103, 104, 105, 106, 107, 108}, // 8 down as a loser; 102 stays as a loser
	}
	for comp, w := range want {
		if !slices.Equal(got[comp], w) {
			t.Errorf("league %d = %v, want %v", comp, got[comp], w)
		}
	}
	if !maps.EqualFunc(rankings, map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8)}, slices.Equal) {
		t.Error("input rankings were modified")
	}

	// Invariant: when every upper team wins its tie, nobody moves — the same
	// entrants return to each league.
	stay, err := NextEntrantsAfterPlayoffs(rankings, links, map[Link][]ids.TeamID{{Upper: 1, Lower: 2, Places: 2}: {8, 7}})
	if err != nil {
		t.Fatal(err)
	}
	for comp, r := range rankings {
		if !slices.Equal(stay[comp], slices.Sorted(slices.Values(r))) {
			t.Errorf("all favorites won but league %d became %v", comp, stay[comp])
		}
	}
	// Invariant: when every lower team wins, the play-offs move exactly the
	// link's direct swap.
	upset, err := NextEntrantsAfterPlayoffs(rankings, links, map[Link][]ids.TeamID{{Upper: 1, Lower: 2, Places: 2}: {101, 102}})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := NextEntrants(rankings, links)
	if err != nil {
		t.Fatal(err)
	}
	for comp := range direct {
		if !slices.Equal(upset[comp], direct[comp]) {
			t.Errorf("all underdogs won but league %d = %v, want %v", comp, upset[comp], direct[comp])
		}
	}
}

func TestNextEntrantsAfterPlayoffsChains(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8), 3: ranking(201, 8)}
	links := []Link{{Upper: 1, Lower: 2, Places: 2}, {Upper: 2, Lower: 3, Places: 1}}
	// Pairs: {8,101}, {7,102} over 1-2 and {108,201} over 2-3.
	winners := map[Link][]ids.TeamID{
		{Upper: 1, Lower: 2, Places: 2}: {8, 102}, // 8 saved; 102 up
		{Upper: 2, Lower: 3, Places: 1}: {201},    // the challenger goes up
	}
	got, err := NextEntrantsAfterPlayoffs(rankings, links, winners)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ids.CompetitionID][]ids.TeamID{
		1: {1, 2, 3, 4, 5, 6, 8, 102},
		// 7 is down after losing and 201 is up after winning over 2-3; 101
		// stays as a loser and 108 drops to 3 the same way.
		2: {7, 101, 103, 104, 105, 106, 107, 201},
		3: {108, 202, 203, 204, 205, 206, 207, 208},
	}
	for comp, w := range want {
		if !slices.Equal(got[comp], w) {
			t.Errorf("league %d = %v, want %v", comp, got[comp], w)
		}
	}
}

func TestApplyPlayoffsRejects(t *testing.T) {
	rankings := map[ids.CompetitionID][]ids.TeamID{1: ranking(1, 8), 2: ranking(101, 8)}
	links := []Link{{Upper: 1, Lower: 2, Places: 2}}
	l := links[0]
	for name, winners := range map[string]map[Link][]ids.TeamID{
		"missing outcome":   {},
		"too few winners":   {l: {101}},
		"too many winners":  {l: {101, 7, 8}},
		"winner of no tie":  {l: {5, 7}},
		"duplicate winners": {l: {8, 8}},
	} {
		if _, err := NextEntrantsAfterPlayoffs(rankings, links, winners); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
