package competitions

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Link joins two adjacent divisions: at a season's end the bottom Places
// teams of Upper's final ranking swap leagues with the top Places teams of
// Lower's. It is a direct swap, so both divisions keep their size.
type Link struct {
	Upper, Lower ids.CompetitionID
	Places       int
}

// NextEntrants returns each league's entrants for the next season, in
// ascending team order, from every league's final ranking (best first).
// Leagues linked by no Link keep their teams; a division may be the lower
// end of one link and the upper end of another (a team then moves at most
// one step, since every link is decided from the season just finished).
//
// It is a pure function of the rankings and links, so validation can replay
// movement from recorded results. It rejects a link to a missing league,
// a league that is the upper or lower end of two links, a link whose places
// exceed either ranking (or overlap, when places exceed half a ranking), and
// a team that ranks in two leagues.
func NextEntrants(rankings map[ids.CompetitionID][]ids.TeamID, links []Link) (map[ids.CompetitionID][]ids.TeamID, error) {
	seen := map[ids.TeamID]ids.CompetitionID{}
	for comp, r := range rankings {
		for _, t := range r {
			if other, dup := seen[t]; dup {
				return nil, fmt.Errorf("competitions: team %d ranks in leagues %d and %d", t, other, comp)
			}
			seen[t] = comp
		}
	}
	upperUsed, lowerUsed := map[ids.CompetitionID]bool{}, map[ids.CompetitionID]bool{}
	next := map[ids.CompetitionID][]ids.TeamID{}
	for comp, r := range rankings {
		next[comp] = slices.Clone(r)
	}
	type swap struct{ down, up []ids.TeamID }
	swaps := make([]swap, len(links))
	for i, l := range links {
		up, okU := rankings[l.Upper]
		low, okL := rankings[l.Lower]
		switch {
		case !okU || !okL || l.Upper == l.Lower:
			return nil, fmt.Errorf("competitions: link %+v names a missing or identical league", l)
		case l.Places < 1 || l.Places > len(up)/2 || l.Places > len(low)/2:
			return nil, fmt.Errorf("competitions: link %+v moves %d places of %d and %d teams", l, l.Places, len(up), len(low))
		case upperUsed[l.Upper] || lowerUsed[l.Lower]:
			return nil, fmt.Errorf("competitions: link %+v reuses a league end", l)
		}
		upperUsed[l.Upper], lowerUsed[l.Lower] = true, true
		swaps[i] = swap{down: up[len(up)-l.Places:], up: low[:l.Places]}
	}
	remove := func(comp ids.CompetitionID, out []ids.TeamID) {
		next[comp] = slices.DeleteFunc(next[comp], func(t ids.TeamID) bool { return slices.Contains(out, t) })
	}
	for i, l := range links {
		remove(l.Upper, swaps[i].down)
		remove(l.Lower, swaps[i].up)
	}
	for i, l := range links {
		next[l.Upper] = append(next[l.Upper], swaps[i].up...)
		next[l.Lower] = append(next[l.Lower], swaps[i].down...)
	}
	for comp := range next {
		slices.Sort(next[comp])
	}
	return next, nil
}
