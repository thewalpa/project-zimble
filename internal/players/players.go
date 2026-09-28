// Package players owns playing profiles: positions, attributes and whether
// the player has retired, with the rules that develop attributes and retire
// players over the years (see development.go).
//
// Identity (names, birth dates) belongs to the registry and employment to
// the employment module. This package holds only football capability data;
// rules take a player's age as detached input.
package players

import (
	"cmp"
	"errors"
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
// The same 1..100 scale is stored, simulated and shown to users.
type Rating uint8

const (
	MinRating Rating = 1
	MaxRating Rating = 100
)

func (r Rating) Valid() bool { return r >= MinRating && r <= MaxRating }

// Attribute indexes Attributes. Values are durable; never reorder.
type Attribute uint8

const (
	Goalkeeping  Attribute = 0
	Defending    Attribute = 1
	Passing      Attribute = 2
	Finishing    Attribute = 3
	Pace         Attribute = 4
	Stamina      Attribute = 5
	Dribbling    Attribute = 6 // keeping the ball in a duel, first touch
	Heading      Attribute = 7
	Strength     Attribute = 8
	Acceleration Attribute = 9  // the first steps of a sprint; Pace is top speed
	Positioning  Attribute = 10 // off-ball runs for attackers, marking for defenders
	// NumAttributes counts the attributes; keep it last.
	NumAttributes = 11
)

var attributeNames = [NumAttributes]string{
	"goalkeeping", "defending", "passing", "finishing", "pace", "stamina",
	"dribbling", "heading", "strength", "acceleration", "positioning",
}

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

// Profile is a player's playing profile. A retired player's profile is
// kept as it was when they retired; it never changes again.
type Profile struct {
	Player     ids.PlayerID
	Position   Position
	Attributes Attributes
	Retired    bool
}

// Overall returns the mean of the position's key attributes on the 1..100
// scale, rounded half up. Integer arithmetic keeps it exact.
func (p Profile) Overall() int {
	keys := keyAttributes[p.Position]
	if len(keys) == 0 {
		return 0
	}
	sum := 0
	for _, a := range keys {
		sum += int(p.Attributes[a])
	}
	return (2*sum + len(keys)) / (2 * len(keys))
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
// mutation. Changes are two-step: Plan validates a set of changes against
// the current state and Apply commits it.
type Store struct {
	rows       []Profile // sorted by Player
	index      map[ids.PlayerID]int
	generation uint64 // increments on every Apply; plans are tied to one
}

// New validates the profiles and returns a store holding its own copy.
func New(profiles []Profile) (*Store, error) {
	rows, index, err := build(profiles)
	if err != nil {
		return nil, err
	}
	return &Store{rows: rows, index: index}, nil
}

func build(profiles []Profile) ([]Profile, map[ids.PlayerID]int, error) {
	rows := slices.Clone(profiles)
	slices.SortFunc(rows, func(a, b Profile) int { return cmp.Compare(a.Player, b.Player) })
	index := make(map[ids.PlayerID]int, len(rows))
	for i, p := range rows {
		if err := p.Validate(); err != nil {
			return nil, nil, err
		}
		if _, dup := index[p.Player]; dup {
			return nil, nil, fmt.Errorf("players: duplicate player ID %d", p.Player)
		}
		index[p.Player] = i
	}
	return rows, index, nil
}

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("players: plan is stale")

// Development replaces an active player's attributes.
type Development struct {
	Player     ids.PlayerID
	Attributes Attributes
}

// Changes is a set of profile changes made together: new attributes for
// active players, retirements of active players and new (active) profiles.
// A player appears at most once across Developments and Retirements.
type Changes struct {
	Developments []Development
	Retirements  []ids.PlayerID
	Additions    []Profile
}

// Plan is a validated set of changes, applied by Apply.
type Plan struct {
	generation uint64
	rows       []Profile
}

// Plan validates changes against the current store and returns the
// resulting state without changing the store.
func (s *Store) Plan(c Changes) (Plan, error) {
	rows := slices.Clone(s.rows)
	touched := map[ids.PlayerID]bool{}
	active := func(id ids.PlayerID) (int, error) {
		i, ok := s.index[id]
		if !ok || rows[i].Retired || touched[id] {
			return 0, fmt.Errorf("players: player %d is unknown, retired or changed twice", id)
		}
		touched[id] = true
		return i, nil
	}
	for _, d := range c.Developments {
		i, err := active(d.Player)
		if err != nil {
			return Plan{}, err
		}
		rows[i].Attributes = d.Attributes
		if err := rows[i].Validate(); err != nil {
			return Plan{}, err
		}
	}
	for _, id := range c.Retirements {
		i, err := active(id)
		if err != nil {
			return Plan{}, err
		}
		rows[i].Retired = true
	}
	for _, p := range c.Additions {
		if p.Retired {
			return Plan{}, fmt.Errorf("players: new player %d is retired", p.Player)
		}
		rows = append(rows, p) // build rejects invalid and duplicate IDs
	}
	if _, _, err := build(rows); err != nil {
		return Plan{}, err
	}
	return Plan{generation: s.generation, rows: rows}, nil
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
