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
	pool, err := canonicalPool(team, candidates)
	if err != nil {
		return matches.TeamInput{}, err
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

// canonicalPool returns a copy of candidates sorted by player ID, rejecting
// invalid or duplicated players, roles and conditions.
func canonicalPool(team ids.TeamID, candidates []Candidate) ([]Candidate, error) {
	pool := slices.Clone(candidates)
	slices.SortFunc(pool, func(a, b Candidate) int { return cmp.Compare(a.Player, b.Player) })
	for i, c := range pool {
		switch {
		case !c.Player.Valid() || (i > 0 && pool[i-1].Player == c.Player):
			return nil, fmt.Errorf("%w for team %d: player ID %d invalid or duplicated", ErrNoLegalLineup, team, c.Player)
		case !c.Natural.Valid():
			return nil, fmt.Errorf("%w for team %d: player %d has invalid role %d", ErrNoLegalLineup, team, c.Player, c.Natural)
		case c.Condition == 0 || c.Condition > matches.MaxCondition:
			return nil, fmt.Errorf("%w for team %d: player %d has condition %d", ErrNoLegalLineup, team, c.Player, c.Condition)
		}
	}
	return pool, nil
}

// Slot is one starting place of a saved lineup. A zero Player is a vacancy:
// the player who held it is no longer available.
type Slot struct {
	Player ids.PlayerID
	Role   matches.Role
}

// RefillLineup carries a manager's saved lineup into a new match, changing
// as little as possible. starters are the saved slots in slot order, with
// vacancies; bench is the saved bench without the players who are gone;
// candidates are the team's whole current squad.
//
//   - Kept starters keep their slot and role; the bench keeps its order.
//   - Each vacancy, in slot order, is filled as SelectTeam fills a role:
//     the natural player not starting with the best fitScore (bench players
//     included; ties: lower player ID), else for an outfield slot the best
//     outfield player not starting. A bench player who starts leaves the
//     bench.
//   - The bench is cut to rules.MaxBench, keeping its first players. Its
//     empty places are not filled.
//
// Every kept player must be a candidate and selected once, and the saved
// lineup must have exactly one goalkeeper slot. A vacancy nobody can fill
// is ErrNoLegalLineup.
func RefillLineup(team ids.TeamID, starters []Slot, bench []ids.PlayerID, candidates []Candidate, rules matches.Rules) ([]Slot, []ids.PlayerID, error) {
	fail := func(format string, args ...any) ([]Slot, []ids.PlayerID, error) {
		return nil, nil, fmt.Errorf("%w for team %d: "+format, append([]any{ErrNoLegalLineup, team}, args...)...)
	}
	if !team.Valid() {
		return fail("invalid team ID")
	}
	pool, err := canonicalPool(team, candidates)
	if err != nil {
		return nil, nil, err
	}
	if len(starters) != matches.StartersPerTeam {
		return fail("%d starting slots, want %d", len(starters), matches.StartersPerTeam)
	}
	index := func(p ids.PlayerID) int {
		i, found := slices.BinarySearchFunc(pool, p, func(c Candidate, p ids.PlayerID) int { return cmp.Compare(c.Player, p) })
		if !found {
			return -1
		}
		return i
	}
	starting := make([]bool, len(pool))
	benched := make([]bool, len(pool))
	keepers := 0
	for _, s := range starters {
		if !s.Role.Valid() {
			return fail("slot with role %d", s.Role)
		}
		if s.Role == matches.Goalkeeper {
			keepers++
		}
		if s.Player == 0 {
			continue
		}
		i := index(s.Player)
		if i < 0 || starting[i] {
			return fail("starter %d is not a candidate or starts twice", s.Player)
		}
		starting[i] = true
	}
	if keepers != 1 {
		return fail("%d goalkeeper slots, want 1", keepers)
	}
	for _, p := range bench {
		i := index(p)
		if i < 0 || starting[i] || benched[i] {
			return fail("substitute %d is not a candidate or selected twice", p)
		}
		benched[i] = true
	}

	// best returns the index of the best candidate not starting accepted by
	// ok, scored for role, or -1.
	best := func(role matches.Role, ok func(Candidate) bool) int {
		pick := -1
		for i, c := range pool {
			if starting[i] || !ok(c) {
				continue
			}
			if pick < 0 || fitScore(c, role) > fitScore(pool[pick], role) {
				pick = i // ties keep the earlier (lower) player ID
			}
		}
		return pick
	}
	out := slices.Clone(starters)
	for n, s := range out {
		if s.Player != 0 {
			continue
		}
		i := best(s.Role, func(c Candidate) bool { return c.Natural == s.Role })
		if i < 0 && s.Role != matches.Goalkeeper {
			i = best(s.Role, func(c Candidate) bool { return c.Natural != matches.Goalkeeper })
		}
		if i < 0 {
			return fail("cannot fill a %s slot", roleName(s.Role))
		}
		starting[i], benched[i] = true, false
		out[n].Player = pool[i].Player
	}
	var subs []ids.PlayerID
	for _, p := range bench {
		if benched[index(p)] && len(subs) < int(rules.MaxBench) {
			subs = append(subs, p)
		}
	}
	return out, subs, nil
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
