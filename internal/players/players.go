// Package players owns playing profiles: positions and attributes.
//
// Identity (names) belongs to the registry and employment to the employment
// module. This package holds only football capability data.
package players

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Position is a player's natural position. Values are durable; never reorder.
type Position uint8

const (
	Goalkeeper Position = 1
	Defender   Position = 2
	Midfielder Position = 3
	Forward    Position = 4
)

// Positions lists every valid position in display order.
func Positions() []Position {
	return []Position{Goalkeeper, Defender, Midfielder, Forward}
}

func (p Position) Valid() bool { return p >= Goalkeeper && p <= Forward }

func (p Position) String() string {
	switch p {
	case Goalkeeper:
		return "GK"
	case Defender:
		return "DF"
	case Midfielder:
		return "MF"
	case Forward:
		return "FW"
	}
	return fmt.Sprintf("Position(%d)", uint8(p))
}

// Rating is an attribute value on the inclusive scale MinRating..MaxRating.
type Rating uint8

const (
	MinRating Rating = 1
	MaxRating Rating = 20
)

func (r Rating) Valid() bool { return r >= MinRating && r <= MaxRating }

// Attribute indexes Attributes. Values are durable; never reorder.
type Attribute uint8

const (
	Goalkeeping Attribute = iota
	Defending
	Passing
	Finishing
	Pace
	Stamina
	NumAttributes = iota
)

var attributeNames = [NumAttributes]string{"goalkeeping", "defending", "passing", "finishing", "pace", "stamina"}

func (a Attribute) String() string {
	if int(a) < len(attributeNames) {
		return attributeNames[a]
	}
	return fmt.Sprintf("Attribute(%d)", uint8(a))
}

// Attributes holds one rating per Attribute, indexed by Attribute.
type Attributes [NumAttributes]Rating

// keyAttributes defines which attributes determine Overall for a position.
var keyAttributes = map[Position][]Attribute{
	Goalkeeper: {Goalkeeping, Passing},
	Defender:   {Defending, Pace, Stamina},
	Midfielder: {Passing, Stamina, Pace},
	Forward:    {Finishing, Pace, Passing},
}

// Profile is a player's playing profile.
type Profile struct {
	Player     ids.PlayerID
	Position   Position
	Attributes Attributes
}

// OverallTenths returns the mean of the position's key attributes, in tenths
// of a rating point, rounded half up. Integer arithmetic keeps it exact.
func (p Profile) OverallTenths() int {
	keys := keyAttributes[p.Position]
	if len(keys) == 0 {
		return 0
	}
	sum := 0
	for _, a := range keys {
		sum += int(p.Attributes[a])
	}
	return (sum*20 + len(keys)) / (2 * len(keys))
}

// Validate reports whether the profile satisfies this module's invariants.
func (p Profile) Validate() error {
	if !p.Player.Valid() {
		return fmt.Errorf("players: invalid player ID %d", p.Player)
	}
	if !p.Position.Valid() {
		return fmt.Errorf("players: player %d has invalid position %d", p.Player, p.Position)
	}
	for a, r := range p.Attributes {
		if !r.Valid() {
			return fmt.Errorf("players: player %d %s rating %d outside %d..%d",
				p.Player, Attribute(a), r, MinRating, MaxRating)
		}
	}
	return nil
}

// Store is the authoritative profile store. It is not safe for concurrent
// mutation; it currently has no mutating methods.
type Store struct {
	rows  []Profile // sorted by Player
	index map[ids.PlayerID]int
}

// New validates the profiles and returns a store holding its own copy.
func New(profiles []Profile) (*Store, error) {
	rows := slices.Clone(profiles)
	slices.SortFunc(rows, func(a, b Profile) int { return cmp.Compare(a.Player, b.Player) })
	index := make(map[ids.PlayerID]int, len(rows))
	for i, p := range rows {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if _, dup := index[p.Player]; dup {
			return nil, fmt.Errorf("players: duplicate player ID %d", p.Player)
		}
		index[p.Player] = i
	}
	return &Store{rows: rows, index: index}, nil
}

// Profile returns a copy of the player's profile.
func (s *Store) Profile(id ids.PlayerID) (Profile, bool) {
	i, ok := s.index[id]
	if !ok {
		return Profile{}, false
	}
	return s.rows[i], true
}

// Snapshot exports every profile in player ID order as a fresh copy.
// New(Snapshot()) restores an equivalent store.
func (s *Store) Snapshot() []Profile { return slices.Clone(s.rows) }

// PlayerIDs returns all player IDs with a profile, in ascending order.
func (s *Store) PlayerIDs() []ids.PlayerID {
	out := make([]ids.PlayerID, len(s.rows))
	for i, p := range s.rows {
		out[i] = p.Player
	}
	return out
}
