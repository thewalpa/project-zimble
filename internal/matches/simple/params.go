package simple

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// ModelVersion identifies the behavior of DefaultParams and this package's
// calculations. Bump it whenever the same input, random state and commands
// would produce a different match.
const ModelVersion uint32 = 1

// Units: probabilities are parts per million (ppm), multipliers are permille
// (1000 = x1), fatigue is per 10,000 of a rating, ratings inside the model
// are tenths (rating 12 = 120). All arithmetic is integer so results are
// identical on every platform.
const (
	ppm      = 1_000_000
	permille = 1000
	per10k   = 10_000
)

// Weights are integer weights of three attributes; only their ratio matters.
type Weights struct{ A, B, C int64 }

func (w Weights) sum() int64 { return w.A + w.B + w.C }

// Params are the model's tunable constants. How attributes affect play:
//
//   - Effective rating = rating * (1 - fatigue). Fatigue grows each minute a
//     player is on the pitch by FatigueBase - Stamina*FatigueStaminaStep
//     (per 10,000, floored at 0), capped at FatigueCap. Substitutes start
//     fresh. Stamina has no other effect.
//   - Team attack = role-weighted mean over outfield players of
//     Finishing:Passing:Pace in AttackWeights; role weights AttackShare.
//   - Team defense = role-weighted mean over outfield players of
//     Defending:Pace:Passing in DefenseWeights; role weights DefenseShare.
//   - Chance per minute = BaseChance * 2*A/(A+D_opp), times the side's own
//     MentalityOwn, the opponent's MentalityConcede and, for the home side,
//     HomeAdvantage; clamped to [MinChance, MaxChance].
//   - Shooter: weighted by ShotShare[role] * effective Finishing.
//   - Conversion = BaseConversion * (Finishing + ConversionOffset) /
//     (opponent Goalkeeping + ConversionOffset), clamped. Goalkeeping has no
//     other effect.
//
// Arrays indexed by matches.Role (index 0 unused) or matches.Mentality.
type Params struct {
	Version uint32

	BaseChancePPM, MinChancePPM, MaxChancePPM             int64
	BaseConversionPPM, MinConversionPPM, MaxConversionPPM int64
	ConversionOffset                                      int64 // tenths
	HomeAdvantagePermille                                 int64
	MentalityOwnPermille                                  [4]int64
	MentalityConcedePermille                              [4]int64

	FatigueBasePer10k, FatigueStaminaStepPer10k, FatigueCapPer10k int64

	AttackWeights  Weights // Finishing, Passing, Pace
	DefenseWeights Weights // Defending, Pace, Passing
	AttackShare    [5]int64
	DefenseShare   [5]int64
	ShotShare      [5]int64
}

// DefaultParams returns the constants of ModelVersion. Equal teams produce
// about 2.7 goals per match.
func DefaultParams() Params {
	return Params{
		Version: ModelVersion,

		BaseChancePPM:         110_000, // ~10 chances per team per match
		MinChancePPM:          20_000,
		MaxChancePPM:          400_000,
		BaseConversionPPM:     125_000,
		MinConversionPPM:      30_000,
		MaxConversionPPM:      450_000,
		ConversionOffset:      100,
		HomeAdvantagePermille: 1050,
		//                        -, defensive, balanced, attacking
		MentalityOwnPermille:     [4]int64{0, 800, 1000, 1200},
		MentalityConcedePermille: [4]int64{0, 850, 1000, 1150},

		FatigueBasePer10k:        30, // stamina 1: 26% by 90'; stamina 20: 9%
		FatigueStaminaStepPer10k: 1,
		FatigueCapPer10k:         3000,

		AttackWeights:  Weights{4, 4, 2},
		DefenseWeights: Weights{6, 2, 2},
		//             -, GK, DF, MF, FW
		AttackShare:  [5]int64{0, 0, 1, 3, 5},
		DefenseShare: [5]int64{0, 0, 5, 3, 1},
		ShotShare:    [5]int64{0, 0, 1, 3, 6},
	}
}

// Validate rejects parameters that could divide by zero, overflow or leave
// a probability outside [0, 1].
func (p Params) Validate() error {
	bad := func(what string) error { return fmt.Errorf("simple: invalid params: %s", what) }
	switch {
	case p.Version == 0:
		return bad("version")
	case !(0 <= p.MinChancePPM && p.MinChancePPM <= p.MaxChancePPM && p.MaxChancePPM <= ppm && p.BaseChancePPM > 0):
		return bad("chance range")
	case !(0 <= p.MinConversionPPM && p.MinConversionPPM <= p.MaxConversionPPM && p.MaxConversionPPM <= ppm && p.BaseConversionPPM > 0):
		return bad("conversion range")
	case p.BaseChancePPM > ppm || p.BaseConversionPPM > ppm:
		return bad("base probability above 1")
	case p.ConversionOffset <= 0 || p.HomeAdvantagePermille <= 0 || p.HomeAdvantagePermille > 4*permille:
		return bad("offset or home advantage")
	case p.FatigueBasePer10k < 0 || p.FatigueStaminaStepPer10k < 0 || p.FatigueCapPer10k < 0 || p.FatigueCapPer10k >= per10k:
		return bad("fatigue")
	case p.AttackWeights.sum() <= 0 || p.DefenseWeights.sum() <= 0 || !nonNegative(p.AttackWeights.A, p.AttackWeights.B, p.AttackWeights.C, p.DefenseWeights.A, p.DefenseWeights.B, p.DefenseWeights.C):
		return bad("attribute weights")
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if p.MentalityOwnPermille[m] <= 0 || p.MentalityOwnPermille[m] > 4*permille ||
			p.MentalityConcedePermille[m] <= 0 || p.MentalityConcedePermille[m] > 4*permille {
			return bad("mentality multipliers")
		}
	}
	// Every outfield role needs positive attack, defense and shot shares so
	// the weighted means and shooter draw never divide by zero.
	for r := matches.Defender; r <= matches.Forward; r++ {
		if p.AttackShare[r] <= 0 || p.DefenseShare[r] <= 0 || p.ShotShare[r] <= 0 {
			return bad("role shares")
		}
	}
	if !nonNegative(p.AttackShare[matches.Goalkeeper], p.DefenseShare[matches.Goalkeeper], p.ShotShare[matches.Goalkeeper]) {
		return bad("goalkeeper shares")
	}
	return nil
}

func nonNegative(vs ...int64) bool {
	for _, v := range vs {
		if v < 0 {
			return false
		}
	}
	return true
}
