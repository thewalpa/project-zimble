package competitions

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// A knockout season is a fixed bracket. Its entrants, in bracket order, are
// paired in the first round: entrants 1 v 2, 3 v 4, and so on, the first of
// each pair at home. Each tie is one match; a tie level after regulation is
// decided by a penalty shootout. The winners of ties 1 and 2 meet in the
// next round (the winner of tie 1 at home), the winners of ties 3 and 4, and
// so on, until the final. There is no draw: whoever arranges the bracket
// decides who can meet whom. A round's fixtures are created when the
// previous round's results are recorded, so their IDs follow every fixture
// allocated before then.

// knockoutRounds returns the number of rounds for n entrants, or false if n
// is not a power of two of at least 2.
func knockoutRounds(n int) (int, bool) {
	if n < 2 || n&(n-1) != 0 {
		return 0, false
	}
	return bits.TrailingZeros(uint(n)), true
}

// checkBracket validates a bracket: a power of two of at least two distinct
// valid teams.
func checkBracket(bracket []ids.TeamID) error {
	if _, ok := knockoutRounds(len(bracket)); !ok {
		return fmt.Errorf("competitions: a knockout needs a power of two of at least 2 entrants, not %d", len(bracket))
	}
	for i, t := range bracket {
		if !t.Valid() || slices.Contains(bracket[:i], t) {
			return fmt.Errorf("competitions: knockout entrant %d invalid or listed twice", t)
		}
	}
	return nil
}

// newKnockout builds a knockout season with its first round's fixtures,
// allocating fixture IDs from *next.
func newKnockout(ref SeasonRef, bracket []ids.TeamID, timing Timing, next *ids.FixtureID) (season, error) {
	if err := checkBracket(bracket); err != nil {
		return season{}, err
	}
	n, _ := knockoutRounds(len(bracket))
	kickoffs, err := roundKickoffs(timing, n)
	if err != nil {
		return season{}, err
	}
	rounds := make([]roundState, n)
	for i, k := range kickoffs {
		rounds[i] = roundState{kickoff: k, status: RoundScheduled}
	}
	fixtures := tiesOf(ref, 1, bracket, kickoffs[0], next)
	return season{
		ref: ref, format: FormatKnockout, entrants: slices.Clone(bracket),
		fixtures: fixtures, results: make([]result, len(fixtures)), rounds: rounds,
	}, nil
}

// tiesOf pairs teams in order into one round's fixtures, the first of each
// pair at home, allocating fixture IDs from *next.
func tiesOf(ref SeasonRef, round Round, teams []ids.TeamID, kickoff sim.GameInstant, next *ids.FixtureID) []Fixture {
	out := make([]Fixture, 0, len(teams)/2)
	for i := 0; i+1 < len(teams); i += 2 {
		*next++
		out = append(out, Fixture{ID: *next, Season: ref, Round: round, Home: teams[i], Away: teams[i+1], Kickoff: kickoff})
	}
	return out
}

// checkKnockout verifies a knockout season by replaying its bracket: every
// round's fixtures exist exactly when the previous round is complete (the
// first round's always) and pair the teams still in, in bracket order; every
// result follows the knockout rules. Fixture IDs are checked by the caller.
func (se *season) checkKnockout() error {
	if err := checkBracket(se.entrants); err != nil {
		return err
	}
	n, _ := knockoutRounds(len(se.entrants))
	if len(se.rounds) != n {
		return fmt.Errorf("%d rounds for %d entrants, want %d", len(se.rounds), len(se.entrants), n)
	}
	teams := se.entrants
	open := true // the previous round is complete (or this is the first)
	for r := range n {
		round := Round(r + 1)
		var idx []int
		for i, f := range se.fixtures {
			if f.Round == round {
				idx = append(idx, i)
			}
		}
		if !open {
			if len(idx) > 0 {
				return fmt.Errorf("round %d has fixtures before round %d is complete", round, round-1)
			}
			continue
		}
		if len(idx) != len(teams)/2 {
			return fmt.Errorf("round %d has %d fixtures for %d teams", round, len(idx), len(teams))
		}
		var winners []ids.TeamID
		for k, i := range idx {
			f, res := se.fixtures[i], se.results[i]
			if f.Home != teams[2*k] || f.Away != teams[2*k+1] {
				return fmt.Errorf("fixture %d is %d v %d, the bracket pairs %d v %d", f.ID, f.Home, f.Away, teams[2*k], teams[2*k+1])
			}
			if !res.recorded {
				continue
			}
			if err := checkScore(FormatKnockout, f.ID, [2]uint16{res.home, res.away}, res.pens); err != nil {
				return err
			}
			r, _ := se.resultAt(i)
			winner, _ := r.Winner()
			winners = append(winners, winner)
		}
		open = se.rounds[r].status == RoundCompleted
		teams = winners
	}
	return nil
}

// knockoutRanking ranks entrants by the round each reached, a team that did
// not lose there ahead of one that did, then bracket order. A team plays no
// more after a defeat, so "lost" is always its last fixture.
func (se *season) knockoutRanking() []ids.TeamID {
	type standing struct {
		bracket int
		reached Round
		lost    bool
	}
	rows := make([]standing, len(se.entrants))
	index := map[ids.TeamID]int{}
	for i, t := range se.entrants {
		rows[i].bracket = i
		index[t] = i
	}
	for i, f := range se.fixtures {
		for _, t := range []ids.TeamID{f.Home, f.Away} {
			row := &rows[index[t]]
			row.reached = max(row.reached, f.Round)
			if r, ok := se.resultAt(i); ok {
				if w, _ := r.Winner(); w != t {
					row.lost = true
				}
			}
		}
	}
	slices.SortStableFunc(rows, func(a, b standing) int {
		switch {
		case a.reached != b.reached:
			return int(b.reached) - int(a.reached)
		case a.lost != b.lost:
			if a.lost {
				return 1
			}
			return -1
		}
		return a.bracket - b.bracket
	})
	out := make([]ids.TeamID, len(rows))
	for i, r := range rows {
		out[i] = se.entrants[r.bracket]
	}
	return out
}

// Exit is how far one entrant got in a completed knockout season. Stage
// counts back from the final: 0 for the champion, 1 for the runner-up, 2 for
// a semi-final loser, and so on, so a stage means the same in a bracket of
// any size. Round is the round the team went out in, the final for both
// finalists.
type Exit struct {
	Team  ids.TeamID
	Round Round
	Stage int
}

// Exits returns every entrant's exit of a completed knockout season in
// Ranking order: the champion, the runner-up, the semi-final losers, and so
// on. It is the rule that stage-based awards (prize money, a club's record)
// read. False for an unknown, incomplete or non-knockout season.
func (s *Store) Exits(ref SeasonRef) ([]Exit, bool) {
	i, ok := s.seasonIdx[ref]
	if !ok || s.seasons[i].format != FormatKnockout || !s.seasons[i].completed() {
		return nil, false
	}
	se := &s.seasons[i]
	reached := map[ids.TeamID]Round{}
	for _, f := range se.fixtures {
		reached[f.Home] = max(reached[f.Home], f.Round)
		reached[f.Away] = max(reached[f.Away], f.Round)
	}
	final := Round(len(se.rounds))
	ranking := se.knockoutRanking()
	out := make([]Exit, len(ranking))
	for k, t := range ranking {
		out[k] = Exit{Team: t, Round: reached[t], Stage: int(final-reached[t]) + 1}
	}
	out[0].Stage = 0 // the champion won the final it reached
	return out, true
}
