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

	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/players"
)

// Version identifies the content returned by Default. Version 2 moved
// attribute ranges to the 1..100 scale; version 3 added the economy.
const Version = 3

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

// Economy holds the money rules for generated clubs and contracts.
//
// A player's weekly wage is WageReference * (overall / ReferenceOverall)^2,
// varied uniformly by up to WageVariationPct percent and rounded to the
// nearest 10 units (never below 10). Contracts run for ContractYears[0] to
// ContractYears[1] whole years from the career start.
type Economy struct {
	OpeningBalance   money.Money
	GatePerHomeMatch money.Money // paid to the home club for each league match
	WageReference    money.Money
	ReferenceOverall int
	WageVariationPct int
	ContractYears    [2]int
}

// Wage returns the weekly wage for an overall rating and a variation in
// percent (-WageVariationPct..WageVariationPct). Integer arithmetic only;
// the result fits comfortably in an int64 for validated economies.
func (e Economy) Wage(overall, variationPct int) money.Money {
	r := int64(e.ReferenceOverall)
	w := int64(e.WageReference) * int64(overall) * int64(overall) / (r * r)
	w = w * int64(100+variationPct) / 100
	const step = 10 * money.MinorPerUnit
	w = (w + step/2) / step * step
	return money.Money(max(w, step))
}

func (e Economy) validate() error {
	if e.OpeningBalance < 0 || e.GatePerHomeMatch < 0 || e.WageReference <= 0 || e.WageReference > money.Units(1_000_000) ||
		e.ReferenceOverall <= 0 || e.WageVariationPct < 0 || e.WageVariationPct > 90 ||
		e.ContractYears[0] < 1 || e.ContractYears[0] > e.ContractYears[1] || e.ContractYears[1] > 10 {
		return fmt.Errorf("content: invalid economy %+v", e)
	}
	return nil
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
	Economy      Economy
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
	if err := d.Economy.validate(); err != nil {
		errs = append(errs, err)
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
		// Ranges (1..100) are ordered: goalkeeping, defending, passing, finishing, pace, stamina.
		Profiles: []PositionProfile{
			{players.Goalkeeper, [players.NumAttributes]Range{{48, 90}, {11, 37}, {22, 58}, {1, 17}, {11, 48}, {27, 69}}},
			{players.Defender, [players.NumAttributes]Range{{1, 11}, {43, 90}, {22, 64}, {6, 37}, {27, 74}, {37, 84}}},
			{players.Midfielder, [players.NumAttributes]Range{{1, 11}, {22, 64}, {43, 90}, {22, 64}, {32, 74}, {43, 90}}},
			{players.Forward, [players.NumAttributes]Range{{1, 11}, {6, 37}, {27, 69}, {43, 90}, {43, 90}, {32, 74}}},
		},
		Economy: Economy{
			OpeningBalance:   money.Units(2_000_000),
			GatePerHomeMatch: money.Units(250_000),
			WageReference:    money.Units(1_600), // a squad costs about 1.7 million a year
			ReferenceOverall: 60,
			WageVariationPct: 20,
			ContractYears:    [2]int{1, 4},
		},
	}
}
