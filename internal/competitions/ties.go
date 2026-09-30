package competitions

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// A ties season is one round of parallel single-match ties, in tie order:
// entrants 1 v 2, 3 v 4, and so on, the first of each pair at home. Each tie
// is one match; a tie level after regulation is decided by a penalty
// shootout. Every tie is played at the season's single kickoff; there are no
// later rounds and no draw. It is the format of a promotion play-off (see
// movement.go), whose pairings decide who crosses a link.

// PlayoffDelay is how long after the linked leagues' last kickoff a
// promotion play-off's ties kick off. The ties end well before the next
// league season (and before a cup edition's first round, which the linked
// leagues' own last kickoff delays further).
const PlayoffDelay = sim.Week

// newTies builds a ties season: one round of len(entrants)/2 fixtures pairing
// entrants in order, the first of each pair at home, allocating fixture IDs
// from *next. RoundInterval is unused: the round all plays at FirstKickoff.
func newTies(ref SeasonRef, entrants []ids.TeamID, timing Timing, next *ids.FixtureID) (season, error) {
	if err := checkTiesEntrants(entrants); err != nil {
		return season{}, err
	}
	kickoffs, err := roundKickoffs(timing, 1)
	if err != nil {
		return season{}, err
	}
	return season{
		ref: ref, format: FormatTies, entrants: slices.Clone(entrants),
		fixtures: tiesOf(ref, 1, entrants, kickoffs[0], next), results: make([]result, len(entrants)/2),
		rounds: []roundState{{kickoff: kickoffs[0], status: RoundScheduled}},
	}, nil
}

// checkTiesEntrants validates ties entrants: an even count of at least two
// distinct valid teams.
func checkTiesEntrants(entrants []ids.TeamID) error {
	if len(entrants) < 2 || len(entrants)%2 != 0 {
		return fmt.Errorf("competitions: ties need an even count of at least 2 entrants, not %d", len(entrants))
	}
	for i, t := range entrants {
		if !t.Valid() || slices.Contains(entrants[:i], t) {
			return fmt.Errorf("competitions: ties entrant %d invalid or listed twice", t)
		}
	}
	return nil
}

// checkTies verifies a ties season by replaying it: one round whose fixtures
// pair the entrants in order, every result following the ties rules.
// Fixture IDs are checked by the caller.
func (se *season) checkTies() error {
	if err := checkTiesEntrants(se.entrants); err != nil {
		return err
	}
	if len(se.rounds) != 1 {
		return fmt.Errorf("%d rounds for %d ties entrants, want 1", len(se.rounds), len(se.entrants))
	}
	if len(se.fixtures) != len(se.entrants)/2 {
		return fmt.Errorf("%d fixtures for %d ties entrants, want %d", len(se.fixtures), len(se.entrants), len(se.entrants)/2)
	}
	for k, f := range se.fixtures {
		if f.Round != 1 || f.Home != se.entrants[2*k] || f.Away != se.entrants[2*k+1] {
			return fmt.Errorf("fixture %d is round %d: %d v %d, the ties pair %d v %d", f.ID, f.Round, f.Home, f.Away, se.entrants[2*k], se.entrants[2*k+1])
		}
		if se.results[k].recorded {
			if err := checkScore(FormatTies, f.ID, [2]uint16{se.results[k].home, se.results[k].away}, se.results[k].pens); err != nil {
				return err
			}
		}
	}
	return nil
}

// tiesRanking orders the entrants: each decided tie's winner, in tie order,
// then every team without a win, in tie order. For a completed season that is
// the promoted (or saved) teams first, then the others. Unlike a knockout,
// a ties season has no champion: every tie stands alone.
func (se *season) tiesRanking() []ids.TeamID {
	won := map[ids.TeamID]bool{}
	for i := range se.fixtures {
		if r, ok := se.resultAt(i); ok {
			w, _ := r.Winner()
			won[w] = true
		}
	}
	out := make([]ids.TeamID, 0, len(se.entrants))
	for i := range se.fixtures {
		if r, ok := se.resultAt(i); ok {
			w, _ := r.Winner()
			out = append(out, w)
		}
	}
	for _, t := range se.entrants {
		if !won[t] {
			out = append(out, t)
		}
	}
	return out
}
