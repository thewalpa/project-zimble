// Package employment owns which club employs a player, which of that club's
// teams the player is assigned to, and the contract terms: expiry and weekly
// wage. A player without an assignment is unemployed: a free agent.
//
// Whether the referenced club and team exist is checked by the application,
// which can read the registry; this package does not import other domain
// modules.
package employment

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// Contract is a player's employment terms. The contract covers the
// half-open interval up to Expires: at Expires it has ended.
type Contract struct {
	Expires    sim.GameInstant
	WeeklyWage money.Money // positive
}

func (c Contract) validate() error {
	if !c.Expires.Valid() || c.Expires <= 0 || c.WeeklyWage <= 0 {
		return fmt.Errorf("employment: invalid contract %+v", c)
	}
	return nil
}

// Assignment records a player's employer, team and contract.
type Assignment struct {
	Player   ids.PlayerID
	Club     ids.ClubID
	Team     ids.TeamID
	Contract Contract
}

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("employment: plan is stale")

// Store is the authoritative employment store. Queries return copies.
type Store struct {
	rows       []Assignment // sorted by Player
	index      map[ids.PlayerID]int
	generation uint64 // increments on every Apply; plans are tied to one
}

// New validates the assignments and returns a store holding its own copy.
// Each player may have at most one assignment.
func New(assignments []Assignment) (*Store, error) {
	rows, index, err := build(assignments)
	if err != nil {
		return nil, err
	}
	return &Store{rows: rows, index: index}, nil
}

func build(assignments []Assignment) ([]Assignment, map[ids.PlayerID]int, error) {
	rows := slices.Clone(assignments)
	slices.SortFunc(rows, func(a, b Assignment) int { return cmp.Compare(a.Player, b.Player) })
	index := make(map[ids.PlayerID]int, len(rows))
	for i, a := range rows {
		if err := a.validate(); err != nil {
			return nil, nil, err
		}
		if _, dup := index[a.Player]; dup {
			return nil, nil, fmt.Errorf("employment: player %d has more than one assignment", a.Player)
		}
		index[a.Player] = i
	}
	return rows, index, nil
}

func (a Assignment) validate() error {
	if !a.Player.Valid() || !a.Club.Valid() || !a.Team.Valid() {
		return fmt.Errorf("employment: invalid IDs in assignment %+v", a)
	}
	if err := a.Contract.validate(); err != nil {
		return fmt.Errorf("player %d: %w", a.Player, err)
	}
	return nil
}

// Renewal replaces an employed player's contract; club and team stay.
type Renewal struct {
	Player   ids.PlayerID
	Contract Contract
}

// Changes is a set of employment changes made together. Renewals apply
// first, then Departures (each player's employment ends and they become a
// free agent), then Signings (each of an unemployed player, possibly one who
// departed in the same set). A player appears at most once across Renewals
// and Departures, and at most once in Signings.
type Changes struct {
	Renewals   []Renewal
	Departures []ids.PlayerID
	Signings   []Assignment
}

// Plan is a validated set of changes, applied by Apply.
type Plan struct {
	generation uint64
	rows       []Assignment
}

// Plan validates changes against the current store and returns the
// resulting state without changing the store.
func (s *Store) Plan(c Changes) (Plan, error) {
	next := maps.Clone(s.index)
	rows := slices.Clone(s.rows)
	touched := map[ids.PlayerID]bool{}
	for _, r := range c.Renewals {
		i, ok := next[r.Player]
		if !ok || touched[r.Player] {
			return Plan{}, fmt.Errorf("employment: cannot renew player %d: not employed or changed twice", r.Player)
		}
		if err := r.Contract.validate(); err != nil {
			return Plan{}, fmt.Errorf("player %d: %w", r.Player, err)
		}
		touched[r.Player] = true
		rows[i].Contract = r.Contract
	}
	gone := map[ids.PlayerID]bool{}
	for _, p := range c.Departures {
		if _, ok := next[p]; !ok || touched[p] {
			return Plan{}, fmt.Errorf("employment: player %d cannot depart: not employed or changed twice", p)
		}
		touched[p], gone[p] = true, true
		delete(next, p)
	}
	var kept []Assignment
	for _, a := range rows {
		if !gone[a.Player] {
			kept = append(kept, a)
		}
	}
	signed := map[ids.PlayerID]bool{}
	for _, a := range c.Signings {
		if err := a.validate(); err != nil {
			return Plan{}, err
		}
		if _, employed := next[a.Player]; employed || signed[a.Player] {
			return Plan{}, fmt.Errorf("employment: cannot sign player %d: already employed", a.Player)
		}
		signed[a.Player] = true
		kept = append(kept, a)
	}
	slices.SortFunc(kept, func(a, b Assignment) int { return cmp.Compare(a.Player, b.Player) })
	return Plan{generation: s.generation, rows: kept}, nil
}

// Apply commits a plan. It fails, changing nothing, with ErrStalePlan if the
// store changed after the plan was made.
func (s *Store) Apply(p Plan) error {
	if p.generation != s.generation {
		return fmt.Errorf("%w: made at generation %d, store at %d", ErrStalePlan, p.generation, s.generation)
	}
	rows, index, err := build(p.rows)
	if err != nil {
		return err // unreachable: Plan validated every row
	}
	s.rows, s.index = rows, index
	s.generation++
	return nil
}

func (s *Store) Assignment(player ids.PlayerID) (Assignment, bool) {
	i, ok := s.index[player]
	if !ok {
		return Assignment{}, false
	}
	return s.rows[i], true
}

// Assignments returns all assignments in ascending player order.
func (s *Store) Assignments() []Assignment { return slices.Clone(s.rows) }

// Snapshot exports the store's authoritative state as a fresh copy.
// New(Snapshot()) restores an equivalent store.
func (s *Store) Snapshot() []Assignment { return s.Assignments() }

// WageBill returns the weekly wages of every player the club employs, with
// overflow checking.
func (s *Store) WageBill(club ids.ClubID) (money.Money, error) {
	var total money.Money
	for _, a := range s.rows {
		if a.Club == club {
			var err error
			if total, err = total.Add(a.Contract.WeeklyWage); err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

// Squad returns the players assigned to a team, in ascending ID order.
func (s *Store) Squad(team ids.TeamID) []ids.PlayerID {
	var out []ids.PlayerID
	for _, a := range s.rows {
		if a.Team == team {
			out = append(out, a.Player)
		}
	}
	return out
}
