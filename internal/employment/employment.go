// Package employment owns which club employs a player and which of that
// club's teams the player is assigned to.
//
// Contract terms (dates, wages) are not modeled yet. Whether the referenced
// club and team exist is checked by the application, which can read the
// registry; this package does not import other domain modules.
package employment

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Assignment records a player's employer and team.
type Assignment struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
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
