package ai

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// ContractsVersion identifies the renewal and signing heuristics. Bump it
// whenever the same squads and free agents would produce different
// decisions or contract lengths. Version 2 made renewals allow for youth.
const ContractsVersion = 2

const (
	// RenewalMargin is how far below its squad's average overall a player
	// may be and still be offered a new contract.
	RenewalMargin = 3
	// A player younger than PromiseAge is expected to improve: each year
	// short of it widens their margin by PromisePerYear points.
	PromiseAge     = 24
	PromisePerYear = 2
)

// Renew reports whether an AI club offers a player whose contract is ending
// a new one: it keeps players whose overall is at least the squad's average
// overall minus RenewalMargin, and young players by a wider margin (see
// PromiseAge). Age is in whole years.
func Renew(overall, squadAverage, age int) bool {
	margin := RenewalMargin + PromisePerYear*max(PromiseAge-age, 0)
	return overall >= squadAverage-margin
}

// ContractLength is the number of contract years an AI club offers: a
// uniform draw in [minYears, maxYears] from a stream keyed by the player and
// the calendar year the contract (or its extension) starts. It is a pure
// function, so the user's suggested terms can reproduce it.
func ContractLength(seed random.Seed, player ids.PlayerID, startYear, minYears, maxYears int) int {
	s := random.Derive(seed, "ai/contract-length", ContractsVersion, uint64(player), uint64(startYear))
	return minYears + s.IntN(maxYears-minYears+1)
}

// RoleCount is how many players of a role a club wants.
type RoleCount struct {
	Role  matches.Role
	Count int
}

// ClubNeeds is a club looking for players. Average is its squad's average
// overall: weaker clubs choose first.
type ClubNeeds struct {
	Club    ids.ClubID
	Average int
	Needs   []RoleCount
}

// FreeAgent is an unemployed player available to sign. ReleasedBy is the
// club that just let the player go, if any.
type FreeAgent struct {
	Player     ids.PlayerID
	Role       matches.Role // natural role
	Overall    int
	ReleasedBy ids.ClubID
}

// Signing is one club signing one free agent.
type Signing struct {
	Club   ids.ClubID
	Player ids.PlayerID
}

// Signings allocates free agents to clubs' needs in rounds. In each round,
// every club with an unmet need, weakest average first (ties: lower club
// ID), signs the best free agent (highest overall, ties: lower player ID)
// for its largest need that the pool can still fill (ties: role order). A
// club prefers players it did not just release, and takes one back only
// when no one else can fill the need, so a need is never left open while
// the pool has a player for it. Rounds repeat until no club can sign
// anyone. Inputs are canonicalized and not modified; signings are returned
// in pick order.
func Signings(clubs []ClubNeeds, pool []FreeAgent) ([]Signing, error) {
	clubs = slices.Clone(clubs)
	slices.SortFunc(clubs, func(a, b ClubNeeds) int {
		return cmp.Or(cmp.Compare(a.Average, b.Average), cmp.Compare(a.Club, b.Club))
	})
	need := make([]map[matches.Role]int, len(clubs))
	for i, c := range clubs {
		if !c.Club.Valid() || (i > 0 && clubs[i-1].Club == c.Club) {
			return nil, fmt.Errorf("ai: club %d invalid or listed twice", c.Club)
		}
		need[i] = map[matches.Role]int{}
		for _, n := range c.Needs {
			if !n.Role.Valid() || n.Count < 0 {
				return nil, fmt.Errorf("ai: club %d need %+v", c.Club, n)
			}
			need[i][n.Role] += n.Count
		}
	}
	agents := slices.Clone(pool)
	slices.SortFunc(agents, func(a, b FreeAgent) int {
		return cmp.Or(cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
	for i, a := range agents {
		if !a.Player.Valid() || !a.Role.Valid() {
			return nil, fmt.Errorf("ai: free agent %+v", a)
		}
		if slices.ContainsFunc(agents[:i], func(b FreeAgent) bool { return b.Player == a.Player }) {
			return nil, fmt.Errorf("ai: free agent %d listed twice", a.Player)
		}
	}
	taken := make([]bool, len(agents))
	// bestFor returns the index of the best untaken agent of role for club,
	// preferring those the club did not release, or -1.
	bestFor := func(role matches.Role, club ids.ClubID) int {
		fallback := -1
		for i, a := range agents {
			if taken[i] || a.Role != role {
				continue
			}
			if a.ReleasedBy != club {
				return i
			}
			if fallback < 0 {
				fallback = i
			}
		}
		return fallback
	}
	var out []Signing
	for signed := true; signed; {
		signed = false
		for i, c := range clubs {
			pick, pickNeed := -1, 0
			for _, role := range []matches.Role{matches.Goalkeeper, matches.Defender, matches.Midfielder, matches.Forward} {
				if n := need[i][role]; n > pickNeed {
					if a := bestFor(role, c.Club); a >= 0 {
						pick, pickNeed = a, n
					}
				}
			}
			if pick < 0 {
				continue
			}
			taken[pick] = true
			need[i][agents[pick].Role]--
			out = append(out, Signing{Club: c.Club, Player: agents[pick].Player})
			signed = true
		}
	}
	return out, nil
}
