package competitions

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

type pairing struct {
	round Round
	home  ids.TeamID
	away  ids.TeamID
}

// doubleRoundRobin schedules an even number of canonical (sorted) entrants
// with the circle method and de Werra's canonical venue assignment. The
// seeded stream only permutes entrants into schedule slots; the rest is
// fixed. With m = n-1 rotating slots and slot m fixed, first-half round r
// (0-based) contains:
//
//   - slot m against slot r; slot m is at home when r is odd;
//   - for k = 1..n/2-1, slots (r+k) mod m and (r-k) mod m; the first is at
//     home when k is odd, otherwise the second.
//
// Round r+m repeats round r with venues swapped. No team has more than two
// consecutive home or away fixtures within a half, or three across the
// halfway point. Output is ordered by round, then pair index.
func doubleRoundRobin(canonical []ids.TeamID, rng *random.Stream) []pairing {
	n := len(canonical)
	slot := make([]ids.TeamID, n)
	for i, j := range rng.Perm(n) {
		slot[i] = canonical[j]
	}

	m := n - 1
	out := make([]pairing, 0, m*n)
	for r := range m {
		round := Round(r + 1)
		fixed, other := slot[m], slot[r]
		if r%2 == 0 {
			fixed, other = other, fixed
		}
		out = append(out, pairing{round: round, home: fixed, away: other})
		for k := 1; k < n/2; k++ {
			x, y := slot[(r+k)%m], slot[(r-k+m)%m]
			if k%2 == 0 {
				x, y = y, x
			}
			out = append(out, pairing{round: round, home: x, away: y})
		}
	}
	firstHalf := len(out)
	for i := range firstHalf {
		p := out[i]
		out = append(out, pairing{round: p.round + Round(m), home: p.away, away: p.home})
	}
	return out
}

// checkDoubleRoundRobin verifies the schedule invariants for sorted entrants:
// 2(n-1) rounds of n/2 fixtures, each entrant once per round, every ordered
// pair of distinct entrants exactly once (so each pair meets home and away),
// and fixture IDs valid and unique.
func checkDoubleRoundRobin(entrants []ids.TeamID, fixtures []Fixture) error {
	n := len(entrants)
	if n < 2 || n%2 != 0 {
		return fmt.Errorf("unsupported entrant count %d", n)
	}
	rounds := 2 * (n - 1)
	if want := rounds * n / 2; len(fixtures) != want {
		return fmt.Errorf("%d fixtures, want %d", len(fixtures), want)
	}
	isEntrant := func(t ids.TeamID) bool { _, ok := slices.BinarySearch(entrants, t); return ok }

	type pair struct{ home, away ids.TeamID }
	meetings := map[pair]bool{}
	playedInRound := map[Round]map[ids.TeamID]bool{}
	fixtureIDs := map[ids.FixtureID]bool{}
	for _, f := range fixtures {
		switch {
		case !f.ID.Valid() || fixtureIDs[f.ID]:
			return fmt.Errorf("fixture ID %d invalid or duplicated", f.ID)
		case f.Round < 1 || int(f.Round) > rounds:
			return fmt.Errorf("fixture %d has round %d outside 1..%d", f.ID, f.Round, rounds)
		case f.Home == f.Away:
			return fmt.Errorf("fixture %d is a self-match for team %d", f.ID, f.Home)
		case !isEntrant(f.Home) || !isEntrant(f.Away):
			return fmt.Errorf("fixture %d has a non-entrant team (%d v %d)", f.ID, f.Home, f.Away)
		case meetings[pair{f.Home, f.Away}]:
			return fmt.Errorf("fixture %d repeats %d v %d at the same venue", f.ID, f.Home, f.Away)
		}
		fixtureIDs[f.ID] = true
		meetings[pair{f.Home, f.Away}] = true
		played := playedInRound[f.Round]
		if played == nil {
			played = map[ids.TeamID]bool{}
			playedInRound[f.Round] = played
		}
		for _, t := range []ids.TeamID{f.Home, f.Away} {
			if played[t] {
				return fmt.Errorf("team %d plays twice in round %d", t, f.Round)
			}
			played[t] = true
		}
	}
	// Counts above imply every round is full and every ordered pair appears
	// once; each team is therefore home n-1 times and away n-1 times.
	return nil
}
