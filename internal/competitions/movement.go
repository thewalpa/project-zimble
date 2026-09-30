package competitions

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Link joins two adjacent divisions: at a season's end the bottom Places
// teams of Upper's final ranking swap leagues with the top Places teams of
// Lower's. It is a direct swap, so both divisions keep their size. The swap
// is the primitive: a promotion play-off decides which teams sit in the
// swapped places (see PlayoffPairings and ApplyPlayoffs), and NextEntrants
// then moves them.
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

// PlayoffBase is the first competition ID a promotion play-off edition uses.
// Content competition definitions take IDs below it (the default world uses
// 1..5); each link's play-off competition is assigned from PlayoffBase upward
// in (Upper, Lower) link order. The assignment is durable: a save's play-off
// seasons are keyed by these IDs.
const PlayoffBase ids.CompetitionID = 1000

// PlayoffCompetitions assigns every link its play-off competition ID (see
// PlayoffBase). The result is keyed by competition ID; links are taken in
// (Upper, Lower) order whatever the input order.
func PlayoffCompetitions(links []Link) map[ids.CompetitionID]Link {
	ordered := slices.Clone(links)
	slices.SortFunc(ordered, func(a, b Link) int {
		return cmp.Or(cmp.Compare(a.Upper, b.Upper), cmp.Compare(a.Lower, b.Lower))
	})
	out := make(map[ids.CompetitionID]Link, len(ordered))
	for i, l := range ordered {
		out[PlayoffBase+ids.CompetitionID(i)] = l
	}
	return out
}

// PlayoffPairings returns the promotion play-off ties over a link, in tie
// order, from the linked leagues' final rankings (best first): the i-th
// bottom team of Upper at home against the i-th top team of Lower. Each tie's
// winner takes a place in Upper next season and its loser a place in Lower
// (see ApplyPlayoffs); the ties are one round of a FormatTies season whose
// entrants are the pairs flattened in order.
func PlayoffPairings(l Link, rankings map[ids.CompetitionID][]ids.TeamID) ([][2]ids.TeamID, error) {
	up, okU := rankings[l.Upper]
	low, okL := rankings[l.Lower]
	if !okU || !okL || l.Upper == l.Lower {
		return nil, fmt.Errorf("competitions: link %+v names a missing or identical league", l)
	}
	if l.Places < 1 || l.Places > len(up)/2 || l.Places > len(low)/2 {
		return nil, fmt.Errorf("competitions: link %+v plays off %d places of %d and %d teams", l, l.Places, len(up), len(low))
	}
	out := make([][2]ids.TeamID, l.Places)
	for i := range out {
		out[i] = [2]ids.TeamID{up[len(up)-1-i], low[i]} // home, away
	}
	return out, nil
}

// ApplyPlayoffs re-partitions each link's boundary teams by the play-off
// outcome and returns the adjusted rankings to feed NextEntrants: winners sit
// where they end in Upper, losers where they end in Lower. winners lists each
// tie's winner in tie order (see PlayoffPairings); the loser of a tie is the
// other team of its pair. Every link needs an outcome.
func ApplyPlayoffs(rankings map[ids.CompetitionID][]ids.TeamID, links []Link, winners map[Link][]ids.TeamID) (map[ids.CompetitionID][]ids.TeamID, error) {
	adjusted := map[ids.CompetitionID][]ids.TeamID{}
	for comp, r := range rankings {
		adjusted[comp] = slices.Clone(r)
	}
	for _, l := range links {
		pairs, err := PlayoffPairings(l, rankings)
		if err != nil {
			return nil, err
		}
		won, ok := winners[l]
		if !ok || len(won) != len(pairs) {
			return nil, fmt.Errorf("competitions: link %+v has %d play-off winners for %d ties", l, len(won), len(pairs))
		}
		losers := make([]ids.TeamID, len(pairs))
		for i, p := range pairs {
			switch won[i] {
			case p[0]:
				losers[i] = p[1]
			case p[1]:
				losers[i] = p[0]
			default:
				return nil, fmt.Errorf("competitions: play-off winner %d won no tie %d of %+v", won[i], i+1, l)
			}
		}
		// NextEntrants swaps the bottom of adjusted Upper into Lower and the
		// top of adjusted Lower into Upper, so the losers sit at the bottom of
		// Upper and the winners at the top of Lower.
		up, low := adjusted[l.Upper], adjusted[l.Lower]
		boundary := func(t ids.TeamID) bool {
			for _, p := range pairs {
				if t == p[0] || t == p[1] {
					return true
				}
			}
			return false
		}
		var keptUp, keptLow []ids.TeamID
		for _, t := range up {
			if !boundary(t) {
				keptUp = append(keptUp, t)
			}
		}
		for _, t := range low {
			if !boundary(t) {
				keptLow = append(keptLow, t)
			}
		}
		adjusted[l.Upper] = append(keptUp, losers...)
		adjusted[l.Lower] = append(slices.Clone(won), keptLow...)
	}
	return adjusted, nil
}

// NextEntrantsAfterPlayoffs returns each league's entrants for the next
// season from the final rankings and the play-off outcomes: ApplyPlayoffs
// seats the winners and losers of every link's ties, then NextEntrants swaps
// them along the links. It is a pure function of its inputs, so validation
// can replay movement from recorded results.
func NextEntrantsAfterPlayoffs(rankings map[ids.CompetitionID][]ids.TeamID, links []Link, winners map[Link][]ids.TeamID) (map[ids.CompetitionID][]ids.TeamID, error) {
	adjusted, err := ApplyPlayoffs(rankings, links, winners)
	if err != nil {
		return nil, err
	}
	return NextEntrants(adjusted, links)
}
