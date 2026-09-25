// Package employment owns which club employs a player, which of that club's
// teams the player is assigned to, and the contract terms: expiry and weekly
// wage.
//
// Whether the referenced club and team exist is checked by the application,
// which can read the registry; this package does not import other domain
// modules.
package employment

import (
	"cmp"
	"fmt"
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

// Store is the authoritative employment store. Queries return copies.
type Store struct {
	rows  []Assignment // sorted by Player
	index map[ids.PlayerID]int
}

// New validates the assignments and returns a store holding its own copy.
// Each player may have at most one assignment.
func New(assignments []Assignment) (*Store, error) {
	rows := slices.Clone(assignments)
	slices.SortFunc(rows, func(a, b Assignment) int { return cmp.Compare(a.Player, b.Player) })
	index := make(map[ids.PlayerID]int, len(rows))
	for i, a := range rows {
		if !a.Player.Valid() || !a.Club.Valid() || !a.Team.Valid() {
			return nil, fmt.Errorf("employment: invalid IDs in assignment %+v", a)
		}
		if err := a.Contract.validate(); err != nil {
			return nil, fmt.Errorf("player %d: %w", a.Player, err)
		}
		if _, dup := index[a.Player]; dup {
			return nil, fmt.Errorf("employment: player %d has more than one assignment", a.Player)
		}
		index[a.Player] = i
	}
	return &Store{rows: rows, index: index}, nil
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
