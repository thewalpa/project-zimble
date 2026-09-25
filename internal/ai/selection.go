// Package ai holds decision heuristics for non-user managers. It works on
// detached data (the match contract's player input) and returns decisions;
// it never reads or writes module state.
package ai

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// SelectionVersion identifies the lineup heuristic. Bump it whenever the
// same squad would produce a different selection.
const SelectionVersion = 3

var ErrNoLegalLineup = errors.New("ai: no legal lineup")

// Candidate is a squad player available for selection. Natural is the
// player's own position, used as their role unless moved to fill a gap.
// Condition is their current fitness, 1..matches.MaxCondition (100).
type Candidate struct {
	Player    ids.PlayerID
	Natural   matches.Role
	Ratings   matches.Ratings
	Condition uint8
}

// formation is 4-4-2, in slot order.
var formation = []struct {
	role  matches.Role
	count int
}{
	{matches.Goalkeeper, 1},
	{matches.Defender, 4},
	{matches.Midfielder, 4},
	{matches.Forward, 2},
}

// RoleScore is the heuristic value of a player in a role (integer; higher
// is better). It is the AI's own judgement, not the match model.
func RoleScore(r matches.Ratings, role matches.Role) int {
	g, d, p, f, pa, st := int(r.Goalkeeping), int(r.Defending), int(r.Passing), int(r.Finishing), int(r.Pace), int(r.Stamina)
	switch role {
	case matches.Goalkeeper:
		return 10 * g
	case matches.Defender:
		return 6*d + 2*pa + 2*p
	case matches.Midfielder:
		return 5*p + 2*st + 2*pa + f
	case matches.Forward:
		return 6*f + 3*pa + p
	}
	return 0
}

// fitScore is a candidate's value in a role discounted by condition: a
// player at 70% condition is worth 70% of their RoleScore.
func fitScore(c Candidate, role matches.Role) int {
	return RoleScore(c.Ratings, role) * int(c.Condition)
}

// SelectTeam picks a legal 4-4-2 starting eleven and bench for team.
//
//   - Candidates are canonicalized by player ID first; input order is
//     irrelevant and the slice is not modified.
//   - Players are ranked by fitScore: RoleScore times condition, so a tired
//     player gives way to a fresher one of similar ability.
//   - Each role is filled with natural players, best fitScore first (ties:
//     lower player ID). Outfield gaps are filled from unselected outfield
//     players by RoleScore for the missing role. A natural goalkeeper is
//     required; outfield players never start in goal.
//   - Bench: the best remaining goalkeeper (if any), then the best remaining
//     players by their natural-role fitScore, up to rules.MaxBench.
//   - Mentality is Balanced. No in-match decisions are made yet.
func SelectTeam(team ids.TeamID, candidates []Candidate, rules matches.Rules) (matches.TeamInput, error) {
	fail := func(format string, args ...any) (matches.TeamInput, error) {
		return matches.TeamInput{}, fmt.Errorf("%w for team %d: "+format, append([]any{ErrNoLegalLineup, team}, args...)...)
	}
	if !team.Valid() {
		return fail("invalid team ID")
	}
	pool := slices.Clone(candidates)
	slices.SortFunc(pool, func(a, b Candidate) int { return cmp.Compare(a.Player, b.Player) })
	for i, c := range pool {
		if !c.Player.Valid() || (i > 0 && pool[i-1].Player == c.Player) {
			return fail("player ID %d invalid or duplicated", c.Player)
		}
		if !c.Natural.Valid() {
			return fail("player %d has invalid role %d", c.Player, c.Natural)
		}
		if c.Condition == 0 || c.Condition > matches.MaxCondition {
			return fail("player %d has condition %d", c.Player, c.Condition)
		}
	}
	if len(pool) < matches.StartersPerTeam {
		return fail("%d players, need %d", len(pool), matches.StartersPerTeam)
	}

	used := make([]bool, len(pool))
	// best returns the index of the best unused candidate accepted by ok,
	// scored for role, or -1.
	best := func(role matches.Role, ok func(Candidate) bool) int {
		pick := -1
		for i, c := range pool {
			if used[i] || !ok(c) {
				continue
			}
			if pick < 0 || fitScore(c, role) > fitScore(pool[pick], role) {
				pick = i // ties keep the earlier (lower) player ID
			}
		}
		return pick
	}

	input := matches.TeamInput{Team: team, Tactics: matches.Tactics{Mentality: matches.Balanced}}
	for _, slot := range formation {
		for range slot.count {
			i := best(slot.role, func(c Candidate) bool { return c.Natural == slot.role })
			if i < 0 && slot.role != matches.Goalkeeper {
				i = best(slot.role, func(c Candidate) bool { return c.Natural != matches.Goalkeeper })
			}
			if i < 0 {
				return fail("cannot fill %d %s slots", slot.count, roleName(slot.role))
			}
			used[i] = true
			c := pool[i]
			input.Starters = append(input.Starters, matches.PlayerInput{Player: c.Player, Role: slot.role, Ratings: c.Ratings, Condition: c.Condition})
		}
	}

	addBench := func(i int) {
		used[i] = true
		c := pool[i]
		input.Bench = append(input.Bench, matches.PlayerInput{Player: c.Player, Role: c.Natural, Ratings: c.Ratings, Condition: c.Condition})
	}
	if rules.MaxBench > 0 {
		if i := best(matches.Goalkeeper, func(c Candidate) bool { return c.Natural == matches.Goalkeeper }); i >= 0 {
			addBench(i)
		}
	}
	for len(input.Bench) < int(rules.MaxBench) {
		pick := -1
		for i, c := range pool {
			if used[i] {
				continue
			}
			if pick < 0 || fitScore(c, c.Natural) > fitScore(pool[pick], pool[pick].Natural) {
				pick = i
			}
		}
		if pick < 0 {
			break
		}
		addBench(pick)
	}
	return input, nil
}

func roleName(r matches.Role) string {
	switch r {
	case matches.Goalkeeper:
		return "goalkeeper"
	case matches.Defender:
		return "defender"
	case matches.Midfielder:
		return "midfielder"
	case matches.Forward:
		return "forward"
	}
	return fmt.Sprintf("Role(%d)", uint8(r))
}
