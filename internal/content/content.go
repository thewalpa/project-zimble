// Package content holds versioned, read-only definitions used to generate a
// world: name pools, roster template and baseline attribute ranges.
//
// Definitions are inputs, never runtime state. Changing Default's output
// changes generated worlds and requires bumping Version.
package content

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/players"
)

// Version identifies the content returned by Default.
const Version = 1

// Town is a fictional club home town with a unique three-letter code.
type Town struct {
	Name  string
	Short string
}

// Quota is how many players of a position a generated squad contains.
type Quota struct {
	Position players.Position
	Count    int
}

// Range is an inclusive rating range.
type Range struct {
	Min, Max players.Rating
}

// PositionProfile gives the baseline attribute ranges for a position.
type PositionProfile struct {
	Position players.Position
	Ranges   [players.NumAttributes]Range
}

type Definitions struct {
	Version      int
	ClubCount    int
	Towns        []Town
	ClubSuffixes []string
	FirstNames   []string
	LastNames    []string
	Roster       []Quota // in generation order
	Profiles     []PositionProfile
}

// Clone returns a deep copy that shares no slices with d.
func (d Definitions) Clone() Definitions {
	d.Towns = slices.Clone(d.Towns)
	d.ClubSuffixes = slices.Clone(d.ClubSuffixes)
	d.FirstNames = slices.Clone(d.FirstNames)
	d.LastNames = slices.Clone(d.LastNames)
	d.Roster = slices.Clone(d.Roster)
	d.Profiles = slices.Clone(d.Profiles)
	return d
}

// SquadSize is the number of players in each generated senior squad.
func (d Definitions) SquadSize() int {
	n := 0
	for _, q := range d.Roster {
		n += q.Count
	}
	return n
}

// Profile returns the baseline profile for a position.
func (d Definitions) Profile(p players.Position) (PositionProfile, bool) {
	for _, pp := range d.Profiles {
		if pp.Position == p {
			return pp, true
		}
	}
	return PositionProfile{}, false
}

// Validate checks that the definitions can generate a valid world.
func (d Definitions) Validate() error {
	var errs []error
	if d.ClubCount <= 0 {
		errs = append(errs, fmt.Errorf("content: club count %d must be positive", d.ClubCount))
	}
	if len(d.Towns) < d.ClubCount {
		errs = append(errs, fmt.Errorf("content: %d towns for %d clubs", len(d.Towns), d.ClubCount))
	}
	names, shorts := map[string]bool{}, map[string]bool{}
	for _, t := range d.Towns {
		if t.Name == "" || t.Short == "" || names[t.Name] || shorts[t.Short] {
			errs = append(errs, fmt.Errorf("content: town %+v is empty or duplicated", t))
		}
		names[t.Name], shorts[t.Short] = true, true
	}
	if len(d.ClubSuffixes) == 0 || len(d.FirstNames) == 0 || len(d.LastNames) == 0 {
		errs = append(errs, errors.New("content: name pools must not be empty"))
	}
	seen := map[players.Position]bool{}
	for _, q := range d.Roster {
		if !q.Position.Valid() || q.Count <= 0 || seen[q.Position] {
			errs = append(errs, fmt.Errorf("content: invalid roster quota %+v", q))
		}
		seen[q.Position] = true
		if _, ok := d.Profile(q.Position); !ok {
			errs = append(errs, fmt.Errorf("content: no attribute profile for %s", q.Position))
		}
	}
	if d.SquadSize() == 0 {
		errs = append(errs, errors.New("content: roster template is empty"))
	}
	for _, pp := range d.Profiles {
		for a, r := range pp.Ranges {
			if !r.Min.Valid() || !r.Max.Valid() || r.Min > r.Max {
				errs = append(errs, fmt.Errorf("content: %s %s range %d..%d invalid",
					pp.Position, players.Attribute(a), r.Min, r.Max))
			}
		}
	}
	return errors.Join(errs...)
}

// Default returns a fresh copy of the built-in definitions.
func Default() Definitions {
	return Definitions{
		Version:   Version,
		ClubCount: 8,
		Towns: []Town{
			{"Brackenmoor", "BRK"}, {"Eldhaven", "ELD"}, {"Marrowdale", "MRW"},
			{"Osterholm", "OST"}, {"Quillford", "QUI"}, {"Saltmere", "SAL"},
			{"Veldmouth", "VEL"}, {"Wyrmsby", "WYR"}, {"Caldershaw", "CAL"},
			{"Dunmarrow", "DUN"}, {"Greyfen", "GRY"}, {"Hollowick", "HOL"},
		},
		ClubSuffixes: []string{"United", "Rovers", "Athletic", "Albion", "Wanderers", "Town", "City", "FC"},
		FirstNames: []string{
			"Aaron", "Ben", "Callum", "Dario", "Elias", "Felix", "Gareth", "Hugo",
			"Ivan", "Jonas", "Kofi", "Luca", "Mateo", "Nils", "Oscar", "Pavel",
			"Quentin", "Rafael", "Samir", "Tomas", "Umar", "Viktor", "Wes", "Yannick",
		},
		LastNames: []string{
			"Abbott", "Brandt", "Castell", "Doyle", "Eriksen", "Farrow", "Gallo", "Hart",
			"Ibsen", "Jansen", "Kowal", "Lindqvist", "Moreau", "Novak", "Okafor", "Pereira",
			"Quinlan", "Rossi", "Sauer", "Tanaka", "Ulrich", "Varga", "Whitlock", "Zeller",
			"Adeyemi", "Bellamy", "Costa", "Duarte", "Engel", "Fonseca", "Grady", "Holm",
		},
		Roster: []Quota{
			{players.Goalkeeper, 3},
			{players.Defender, 7},
			{players.Midfielder, 6},
			{players.Forward, 4},
		},
		// Ranges are ordered: goalkeeping, defending, passing, finishing, pace, stamina.
		Profiles: []PositionProfile{
			{players.Goalkeeper, [players.NumAttributes]Range{{10, 18}, {3, 8}, {5, 12}, {1, 4}, {3, 10}, {6, 14}}},
			{players.Defender, [players.NumAttributes]Range{{1, 3}, {9, 18}, {5, 13}, {2, 8}, {6, 15}, {8, 17}}},
			{players.Midfielder, [players.NumAttributes]Range{{1, 3}, {5, 13}, {9, 18}, {5, 13}, {7, 15}, {9, 18}}},
			{players.Forward, [players.NumAttributes]Range{{1, 3}, {2, 8}, {6, 14}, {9, 18}, {9, 18}, {7, 15}}},
		},
	}
}
