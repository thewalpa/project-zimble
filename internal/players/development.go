package players

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

// DevelopmentVersion identifies the development and retirement rules below.
// Bump it whenever the same profile, age, seed and year would develop or
// retire differently.
const DevelopmentVersion = 1

// The yearly step. Once a year every active player either retires or
// develops; age is the player's age in whole years at the step.
const (
	// FreeAgentRetirementAge: a player without a club retires from this age.
	FreeAgentRetirementAge = 31
	// RetirementAge: every player retires at this age at the latest, so no
	// active player is older.
	RetirementAge = 37
	// FormRange bounds a player's form for the year, which moves all of
	// their attributes together: -FormRange..FormRange points.
	FormRange = 2
)

// Growth returns the mean yearly change of an attribute at an age: young
// players improve, players in their late twenties hold level and older ones
// decline, pace, stamina and acceleration a point a year faster. Strength
// and positioning decline a point a year more slowly from 29 through 32,
// then like the rest.
func Growth(age int, a Attribute) int {
	var g int
	switch {
	case age <= 18:
		g = 4
	case age <= 20:
		g = 3
	case age <= 22:
		g = 2
	case age <= 24:
		g = 1
	case age <= 28:
		g = 0
	case age <= 30:
		g = -1
	case age <= 32:
		g = -2
	default:
		g = -3
	}
	switch {
	case age >= 29 && (a == Pace || a == Stamina || a == Acceleration):
		g--
	case age >= 29 && age <= 32 && (a == Strength || a == Positioning):
		g++
	}
	return g
}

// Develop returns a player's attributes after a year at age with neutral
// talent and no learning: each attribute changes by Growth, plus the
// player's form for the year (shared by every attribute), plus its own
// variation of -1..1 points, and stays within MinRating..MaxRating. Draws
// come from a stream keyed by the player and the year, so the result does
// not depend on when or in what order players are developed. See
// DevelopWith for the talent curve and learning.
func Develop(seed random.Seed, p Profile, age, year int) Attributes {
	out, err := DevelopWith(seed, p, Talent{}, Norms{}, age, year)
	if err != nil {
		panic(fmt.Sprintf("players: unreachable: %v", err)) // neutral talent is always valid
	}
	return out
}

// RetirementPermille is the chance, in permille, that a player with a club
// retires at age (before RetirementAge).
func RetirementPermille(age int) int {
	switch age {
	case 33:
		return 100
	case 34:
		return 250
	case 35:
		return 450
	case 36:
		return 700
	}
	return 0
}

// Retires reports whether a player retires at the yearly step: always from
// RetirementAge, from FreeAgentRetirementAge without a club, and otherwise
// with RetirementPermille, drawn from a stream keyed by the player and the
// year.
func Retires(seed random.Seed, player ids.PlayerID, age, year int, employed bool) bool {
	switch {
	case age >= RetirementAge:
		return true
	case !employed && age >= FreeAgentRetirementAge:
		return true
	}
	chance := RetirementPermille(age)
	if chance == 0 {
		return false
	}
	rng := random.Derive(seed, "players/retirement", DevelopmentVersion, uint64(player), uint64(year))
	return rng.IntN(1000) < chance
}
