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
