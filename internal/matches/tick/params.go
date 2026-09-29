package tick

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// ModelVersion identifies the behavior of DefaultParams and this package's
// calculations. Bump it whenever the same input, random state and commands
// would produce a different match, frames included.
const ModelVersion uint32 = 3

// The clock: a tick is one simulated instant.
const (
	TicksPerSecond = 5
	TicksPerMinute = 60 * TicksPerSecond
	tickMillis     = 1000 / TicksPerSecond
)

// Units: distances are centimetres, speeds centimetres per tick,
// probabilities parts per million (ppm) per tick unless noted, multipliers
// permille (1000 = x1). An effective rating is counted in hundredths of a
// point, so rating 100 at full readiness is 10,000. All arithmetic is
// integer so results are identical on every platform.
const (
	ppm      = 1_000_000
	permille = 1000
	per10k   = 10_000
	per100k  = 100_000
	// pointUnits is effective-rating units per rating point.
	pointUnits = 100
)

// Params are the model's tunable constants. How play works each tick:
//
//   - Effective rating = rating * readiness * (1 - fatigue) * home
//     advantage, as in the simple engine: readiness from condition at
//     kickoff, fatigue growing each minute on the pitch, less with more
//     Stamina. It is recomputed once a minute.
//   - Shape: each outfield player has a spot from his role's line depth
//     (deeper out of possession, shifted by mentality) and an even share of
//     the width among his line. The block follows the ball along the pitch
//     (Shift) and across it (Lateral). Players in possession stay level
//     with the last opponent defender or the ball, whichever is further.
//     Out of possession each outfield player marks the nearest free
//     opponent within MarkRadius of his spot, goal-side.
//   - The nearest defender presses the carrier and the next one covers;
//     in the carrier's own third, Pressers (by mentality) press. While a
//     pass is in flight only its receiver moves for the ball; a loose ball
//     is chased by the nearest player of each side.
//   - Movement: a player jogs towards his spot, or sprints when he is far
//     from it, chases a loose ball, presses the carrier or runs onto a pass.
//     Sprint speed grows with Pace.
//   - The carrier shoots more often the nearer he is to goal, passes more
//     often under pressure, and otherwise dribbles at goal. A pass goes to
//     the teammate with the best mix of progress and space (with noise); it
//     misses its aim by up to PassError of its length, less with Passing.
//     A pass is struck to arrive at PassArrivalSpeed. A shot aims inside
//     the posts and misses its aim by up to ShotError of its length, less
//     with Finishing.
//   - A loose ball slows by a constant deceleration each tick, faster on
//     the ground than in the air (shots). Any player within reach of
//     its path may try to control it, the first along the path first; the
//     chance falls with ball speed and distance and rises with Passing (or
//     Defending, for an interception). A goalkeeper in his area reaches
//     further and saves with Goalkeeping; a failed save may be parried,
//     and any other failed control may deflect the ball. Players arriving
//     on a ball at the same instant contest it evenly.
//   - A defender within TackleRadius of the carrier may tackle: Defending
//     against the carrier's Dribbling.
//   - The ball leaving the pitch gives a throw-in, corner or goal kick; a
//     goal gives a kickoff. Set pieces are taken by a pass.
//
// Arrays indexed by matches.Role (index 0 unused) or matches.Mentality.
type Params struct {
	Version uint32

	HomeAdvantagePermille                                            int64
	FatigueBasePer100k, FatigueStaminaStepPer100k, FatigueCapPer100k int64
	ConditionFloorPer10k                                             int64

	// Movement.
	MinSprint, MaxSprint         int64 // at effective Pace 0 and 100
	JogPermille, DribblePermille int64 // of sprint speed
	SprintDistance               int64 // farther than this from his spot, a player sprints

	// Shape: line depths from the own goal line, out of and in possession.
	DefendDepth, AttackDepth [5]int64
	MentalityDepth           [4]int64
	ShiftPermille            int64
	LateralPermille          int64
	MarkRadius, MarkDistance int64
	KeeperDepth              int64
	KeeperTrackPermille      int64

	// The ball.
	GroundDecel, AirDecel int64 // cm/tick lost per tick
	MinBallSpeed          int64

	// Control of a loose ball.
	ControlRadius, KeeperReach                          int64
	ControlPPM, ControlSkillPPM                         int64 // absolute, per attempt
	EasySpeed, ControlSpeedPenaltyPPM                   int64 // penalty per cm/tick above EasySpeed
	InterceptPermille                                   int64
	SavePPM, SaveSkillPPM, SaveSpeedPenaltyPPM          int64
	MinControlPPM, MaxControlPPM, ParryPPM, DeflectPPM  int64
	FirstTouchTicks, MissTicks, KickTicks, ProtectTicks uint32

	// Passing.
	PassPPM, PressuredPassPPM, PressureDistance int64
	MinPass, MaxPass, BlockedDistance           int64
	PassNoise                                   int64
	ProgressPermille                            [4]int64
	PassErrorPermille, PassArrivalSpeed         int64
	MinPassSpeed, MaxPassSpeed                  int64

	// Shooting.
	ShotRange, CertainShotRange int64
	ShotPPM                     int64 // at the goal line, falling to zero at ShotRange
	MentalityShotPermille       [4]int64
	MinShotSpeed, MaxShotSpeed  int64
	ShotErrorPermille           int64

	// Tackling.
	TackleRadius                            int64
	TackleAttemptPPM, TacklePPM, WinBallPPM int64
	BeatenTicks                             uint32
	Pressers                                [4]int // defenders pressing a carrier in his own third, by mentality

	// Restarts.
	CelebrationTicks, RestartTicks, RestartTimeoutTicks uint32
	RestartDistance                                     int64

	// Penalty shootout: ShootoutConversion plus ShootoutSkill per point of
	// taker Finishing above keeper Goalkeeping, clamped to a narrow range.
	ShootoutConversionPPM, MinShootoutPPM, MaxShootoutPPM int64
	ShootoutSkillPPM                                      int64 // per effective rating point
}

// DefaultParams returns the constants of ModelVersion.
func DefaultParams() Params {
	return Params{
		Version: ModelVersion,

		HomeAdvantagePermille:     1030,
		FatigueBasePer100k:        300,
		FatigueStaminaStepPer100k: 2,
		FatigueCapPer100k:         30_000,
		ConditionFloorPer10k:      7000,

		MinSprint:       110, // 5.5 m/s
		MaxSprint:       170, // 8.5 m/s
		JogPermille:     550,
		DribblePermille: 800,
		SprintDistance:  1200,

		//                 -, GK, DF, MF, FW
		DefendDepth: [5]int64{0, 0, 1900, 3500, 4700},
		AttackDepth: [5]int64{0, 0, 3800, 5800, 7700},
		//                        -, def, bal, att
		MentalityDepth:      [4]int64{0, -250, 0, 250},
		ShiftPermille:       450,
		LateralPermille:     300,
		MarkRadius:          1500,
		MarkDistance:        150,
		KeeperDepth:         300,
		KeeperTrackPermille: 150,

		GroundDecel:  4, // 1 m/s²
		AirDecel:     2,
		MinBallSpeed: 8,

		ControlRadius:          110,
		KeeperReach:            300,
		ControlPPM:             950_000,
		ControlSkillPPM:        150_000,
		EasySpeed:              180,
		ControlSpeedPenaltyPPM: 1500,
		InterceptPermille:      500,
		SavePPM:                800_000,
		SaveSkillPPM:           250_000,
		SaveSpeedPenaltyPPM:    500,
		MinControlPPM:          50_000,
		MaxControlPPM:          980_000,
		ParryPPM:               600_000,
		DeflectPPM:             350_000,
		FirstTouchTicks:        1,
		MissTicks:              3,
		KickTicks:              3,
		ProtectTicks:           3,

		PassPPM:           50_000,
		PressuredPassPPM:  180_000,
		PressureDistance:  450,
		MinPass:           500,
		MaxPass:           4000,
		BlockedDistance:   300,
		PassNoise:         400,
		ProgressPermille:  [4]int64{0, 800, 900, 1050},
		PassErrorPermille: 250,
		PassArrivalSpeed:  200, // 10 m/s
		MinPassSpeed:      70,
		MaxPassSpeed:      420,

		ShotRange:             2600,
		CertainShotRange:      700,
		ShotPPM:               400_000,
		MentalityShotPermille: [4]int64{0, 850, 1000, 1150},
		MinShotSpeed:          380,
		MaxShotSpeed:          520,
		ShotErrorPermille:     1200,

		TackleRadius:     170,
		TackleAttemptPPM: 80_000,
		TacklePPM:        400_000,
		WinBallPPM:       500_000,
		BeatenTicks:      5,
		Pressers:         [4]int{0, 1, 1, 2},

		CelebrationTicks:    15 * TicksPerSecond,
		RestartTicks:        2 * TicksPerSecond,
		RestartTimeoutTicks: 30 * TicksPerSecond,
		RestartDistance:     915,

		ShootoutConversionPPM: 750_000,
		MinShootoutPPM:        600_000,
		MaxShootoutPPM:        900_000,
		ShootoutSkillPPM:      1_500,
	}
}

// Validate rejects parameters that could divide by zero, overflow, leave a
// probability outside [0, 1] or stop play from moving.
func (p Params) Validate() error {
	bad := func(what string) error { return fmt.Errorf("tick: invalid params: %s", what) }
	probs := []int64{
		p.ControlPPM, p.ControlSkillPPM, p.SavePPM, p.SaveSkillPPM, p.MinControlPPM, p.MaxControlPPM, p.ParryPPM, p.DeflectPPM,
		p.PassPPM, p.PressuredPassPPM, p.ShotPPM, p.TackleAttemptPPM, p.TacklePPM, p.WinBallPPM,
	}
	for _, v := range probs {
		if v < 0 || v > ppm {
			return bad("probability outside [0, 1]")
		}
	}
	switch {
	case p.Version == 0:
		return bad("version")
	case p.HomeAdvantagePermille <= 0 || p.HomeAdvantagePermille > 2*permille:
		return bad("home advantage")
	case p.FatigueBasePer100k < 0 || p.FatigueStaminaStepPer100k < 0 || p.FatigueCapPer100k < 0 || p.FatigueCapPer100k >= per100k:
		return bad("fatigue")
	case p.ConditionFloorPer10k <= 0 || p.ConditionFloorPer10k > per10k:
		return bad("condition floor")
	case p.MinSprint <= 0 || p.MaxSprint < p.MinSprint || p.MaxSprint > 400:
		return bad("sprint speeds")
	case p.JogPermille <= 0 || p.JogPermille > permille || p.DribblePermille <= 0 || p.DribblePermille > permille:
		return bad("jog or dribble speed")
	case p.GroundDecel <= 0 || p.AirDecel <= 0 || p.GroundDecel > 100 || p.AirDecel > 100:
		return bad("deceleration")
	case p.MinBallSpeed <= 0 || p.ControlRadius <= 0 || p.KeeperReach < p.ControlRadius:
		return bad("ball speed or reach")
	case p.MinControlPPM <= 0 || p.MaxControlPPM < p.MinControlPPM:
		return bad("control range")
	case p.MinPass <= 0 || p.MaxPass <= p.MinPass || p.MaxPass > matches.PitchLength || p.PassNoise <= 0:
		return bad("pass range")
	case p.MinPassSpeed <= 0 || p.MaxPassSpeed < p.MinPassSpeed || p.MinShotSpeed <= 0 || p.MaxShotSpeed < p.MinShotSpeed:
		return bad("kick speeds")
	case p.ShotRange <= 0 || p.CertainShotRange < 0 || p.CertainShotRange > p.ShotRange:
		return bad("shot range")
	case p.PassErrorPermille < 0 || p.ShotErrorPermille < 0 || p.PassArrivalSpeed < 0:
		return bad("kick errors")
	case p.TackleRadius <= 0 || p.RestartDistance < 0 || p.RestartTimeoutTicks == 0 || p.KickTicks == 0:
		return bad("tackle or restart")
	case !(0 < p.MinShootoutPPM && p.MinShootoutPPM <= p.MaxShootoutPPM && p.MaxShootoutPPM < ppm &&
		0 < p.ShootoutConversionPPM && p.ShootoutConversionPPM <= ppm && 0 <= p.ShootoutSkillPPM && p.ShootoutSkillPPM <= ppm/matches.MaxRating):
		return bad("shootout")
	}
	for r := matches.Defender; r <= matches.Forward; r++ {
		for _, d := range [...]int64{p.DefendDepth[r], p.AttackDepth[r]} {
			if d <= 0 || d >= matches.PitchLength {
				return bad("line depths")
			}
		}
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if p.ProgressPermille[m] < 0 || p.MentalityShotPermille[m] <= 0 || p.MentalityShotPermille[m] > 4*permille ||
			p.Pressers[m] < 0 || p.Pressers[m] > matches.StartersPerTeam-1 {
			return bad("mentality settings")
		}
	}
	return nil
}
