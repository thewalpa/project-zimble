package enginetest

import (
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// CareerRatings gives a specialist profile around strength (1..100): each
// role is strong at its own trade and weak at the others', the way generated
// career squads are. At strength 60 the starters average, per role, about
// the attributes of a built-in career league's squads (goalkeeper
// Goalkeeping 85; defender Defending 75; midfielder Passing 76; forward
// Finishing 78 and Pace 69), measured over a seeded career. Ratings, the
// flat profile, keeps the contract suite and the trend tests simple; a goal
// level meant for careers is calibrated on this one.
func CareerRatings(role matches.Role, strength, i int) matches.Ratings {
	r := Ratings(role, strength, i)
	v := (i%5 - 2) * 3
	at := func(offset int) uint8 { return clamp(strength + offset + v) }
	switch role {
	case matches.Goalkeeper:
		r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace = at(25), at(-29), at(-14), at(-40), at(-27)
	case matches.Defender:
		r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace = at(-43), at(15), at(-12), at(-31), at(-5)
	case matches.Midfielder:
		r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace = at(-43), at(-14), at(16), at(-15), at(-6)
	case matches.Forward:
		r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace = at(-44), at(-33), at(-11), at(18), at(9)
	}
	return r
}

// CareerInput is Input with CareerRatings for both squads.
func CareerInput(fixture ids.FixtureID, homeStrength, awayStrength int) *matches.MatchInput {
	in := Input(fixture, homeStrength, awayStrength)
	for _, t := range []*matches.TeamInput{&in.Home, &in.Away} {
		strength := homeStrength
		if t == &in.Away {
			strength = awayStrength
		}
		for i := range t.Starters {
			t.Starters[i].Ratings = CareerRatings(t.Starters[i].Role, strength, i)
		}
		for i := range t.Bench {
			t.Bench[i].Ratings = CareerRatings(t.Bench[i].Role, strength, i+len(t.Starters))
		}
	}
	return in
}
