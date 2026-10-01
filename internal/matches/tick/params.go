package tick

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// ModelVersion identifies the behavior of DefaultParams and this package's
// calculations. Bump it whenever the same input, random state and commands
// would produce a different match, frames included.
const ModelVersion uint32 = 7

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
//   - Effective rating = rating * readiness * (1 - fatigue), plus
//     HomeAdvantage for the home side: readiness from condition at
//     kickoff, fatigue growing each minute on the pitch, less with more
//     Stamina. It is recomputed once a minute.
//   - Contests: a skill counts against the opposing skill it meets, never
//     on its own, so two equal sides play the same football at every level
//     and only the difference between them decides who scores. The contest
//     rating is ContestReference plus ContestPermille of the difference:
//     passing (a pass's accuracy, a teammate's control) against the
//     opponents' average Defending; Finishing against the opposing
//     keeper's Goalkeeping and a save against the shooter's Finishing; an
//     interception against the passers' average Passing, and a keeper
//     collecting any other ball against the same; Pace against the
//     opponents' average Pace, so play has the same tempo at every level.
//     Averages are over the outfield players on the pitch. Tackles and
//     penalty shootouts were contests already.
//   - Shape: each outfield player has a spot from his role's line depth
//     (deeper out of possession) and an even share of the width among his
//     line. The block follows the ball along the pitch (Shift) and across
//     it (Lateral). Players in possession drift around their spots, a new
//     offset of up to Drift every DriftTicks or so, and stop level with
//     the offside line: the second-last opponent, keeper included, unless
//     the ball or the halfway line is further.
//   - Runs in behind: while a team-mate has the ball, a forward may start
//     a run (RunPPM per tick, by mentality) to RunDepth behind the
//     opponents' line for RunTicks, and the carrier then looks to pass far
//     more often (RunPassPPM). Out of possession, while an opponent has the
//     ball, a defender drops at most LineHold behind his spot to follow a
//     runner: the back line holds, and a run that beats it is either
//     onside, through on goal, or caught offside.
//   - Mentality moves the lines: attacking pushes both higher, so a side
//     makes more chances and leaves more room behind; defensive drops both
//     and so makes and allows fewer. Out of possession the line moves
//     further than in possession, and a deeper back line catches fewer
//     opponents offside.
//   - The room behind a line is priced (MentalityConcedePermille, the shape
//     simple uses): against a high line the opponents start more runs in
//     behind, play the counter pass more often and take more of their
//     shots; against a deep block fewer. The counter is the cost of
//     attacking and the payoff of defensive.
//   - Marking: out of possession, defenders and opponents within
//     MarkRadius of the defender's spot pair up nearest first. A defender
//     stands goal-side of a man within TightMarkRadius of his spot, and
//     partway between his spot and the man farther out, so the block
//     neither ignores nor chases runners suddenly. Drift and gradual
//     marking keep results continuous in the line depths: without them,
//     a line moved by a metre or two could flip whole lines between marked
//     and free and double the goals.
//   - The nearest defender presses the carrier and the next one covers;
//     in the carrier's own third, Pressers (by mentality) press. While a
//     pass is in flight only its receiver moves for the ball; a loose ball
//     is chased by the nearest player of each side.
//   - Movement: a player jogs towards his spot, or sprints when he is far
//     from it, chases a loose ball, presses the carrier or runs onto a pass.
//     Sprint speed grows with contest Pace.
//   - The carrier shoots more often the nearer he is to goal, passes more
//     often under pressure, and otherwise dribbles at goal. A pass goes to
//     the teammate with the best mix of progress and space (with noise),
//     either to his feet or, as a through ball, ThroughBallLead ahead of him
//     into the space behind the opponents' line, for him to run onto when
//     he would beat the nearest opponent there (a runner already sprinting
//     has a RunStart head start). The passer commits before he strikes the
//     ball, so he overlooks a team-mate offside by up to OffsideVision. A
//     pass misses its aim by up to PassError of its length, less with
//     Passing, and is struck to arrive at PassArrivalSpeed. A shot aims
//     inside the posts and misses its aim by up to ShotError of its length,
//     less with Finishing.
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
//   - Offside: when a player passes or shoots, his team-mates beyond the
//     offside line are in an offside position. The intended receiver among
//     them is caught offside when he reaches the ball, and the others leave
//     it alone, until the next play by their side or possession by anyone;
//     a throw-in, corner or goal kick puts nobody offside. The opponents
//     take a free kick where he reached it.
//   - The ball leaving the pitch gives a throw-in, corner or goal kick; a
//     goal gives a kickoff. Set pieces are taken by a pass.
//
// Arrays indexed by matches.Role (index 0 unused) or matches.Mentality.
type Params struct {
	Version uint32

	HomeAdvantage                                                    int64 // effective-rating units
	FatigueBasePer100k, FatigueStaminaStepPer100k, FatigueCapPer100k int64
	ConditionFloorPer10k                                             int64
	ContestReference, ContestPermille                                int64

	// Movement.
	MinSprint, MaxSprint         int64 // at contest Pace 0 and 100
	JogPermille, DribblePermille int64 // of sprint speed
	SprintDistance               int64 // farther than this from his spot, a player sprints

	// Shape: line depths from the own goal line, out of and in possession,
	// each moved by the side's mentality. MentalityConcedePermille prices
	// the room a line leaves behind it: the opponents' runs in behind,
	// counter passes and shots against that mentality.
	DefendDepth, AttackDepth                   [5]int64
	MentalityDefendDepth, MentalityAttackDepth [4]int64
	MentalityConcedePermille                   [4]int64
	ShiftPermille                              int64
	LateralPermille                            int64
	DriftDepth, DriftWidth                     int64  // largest drift from a spot in possession
	DriftTicks                                 uint32 // a drift lasts DriftTicks to twice that
	MarkRadius, TightMarkRadius, MarkDistance  int64
	KeeperDepth                                int64
	KeeperTrackPermille                        int64

	// Runs in behind, through balls and offside.
	RunPPM          [4]int64 // a forward starts a run, per tick, by mentality
	RunTicks        uint32   // a run's length
	RunDepth        int64    // how far behind the opponents' line a run aims
	RunPassPPM      int64    // the carrier's chance to pass, per tick, during a run
	RunStart        int64    // a runner's head start in the race for a through ball
	LineHold        int64    // defenders follow a runner at most this far behind their spots
	ThroughBallLead int64    // a through ball's aim ahead of its receiver
	OffsideVision   int64    // a passer overlooks an offside by up to this

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

		HomeAdvantage:             300,
		FatigueBasePer100k:        300,
		FatigueStaminaStepPer100k: 2,
		FatigueCapPer100k:         30_000,
		ConditionFloorPer10k:      7000,
		ContestReference:          6000,
		ContestPermille:           340,

		MinSprint:       110, // 5.5 m/s
		MaxSprint:       170, // 8.5 m/s
		JogPermille:     550,
		DribblePermille: 800,
		SprintDistance:  1200,

		//                 -, GK, DF, MF, FW
		DefendDepth: [5]int64{0, 0, 2400, 3440, 4640},
		AttackDepth: [5]int64{0, 0, 3770, 5770, 7670},
		//                              -, def, bal, att
		MentalityDefendDepth:     [4]int64{0, -300, 0, 100},
		MentalityAttackDepth:     [4]int64{0, -40, 0, 30},
		MentalityConcedePermille: [4]int64{0, 825, 1000, 1250},
		ShiftPermille:            450,
		LateralPermille:          300,
		MarkRadius:               1800,
		MarkDistance:             150,
		TightMarkRadius:          800,
		DriftDepth:               250,
		DriftWidth:               250,
		DriftTicks:               5 * TicksPerSecond,
		KeeperDepth:              300,
		KeeperTrackPermille:      150,

		RunPPM:          [4]int64{0, 7_000, 8_000, 10_000},
		RunTicks:        3 * TicksPerSecond,
		RunDepth:        800,
		RunPassPPM:      300_000,
		RunStart:        500,
		LineHold:        300,
		ThroughBallLead: 800,
		OffsideVision:   200,

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
		ShotErrorPermille:     1600,

		TackleRadius:     170,
		TackleAttemptPPM: 80_000,
		TacklePPM:        400_000,
		WinBallPPM:       500_000,
		BeatenTicks:      5,
		Pressers:         [4]int{0, 1, 1, 1},

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
		p.PassPPM, p.PressuredPassPPM, p.ShotPPM, p.TackleAttemptPPM, p.TacklePPM, p.WinBallPPM, p.RunPassPPM,
	}
	for _, v := range probs {
		if v < 0 || v > ppm {
			return bad("probability outside [0, 1]")
		}
	}
	switch {
	case p.Version == 0:
		return bad("version")
	case p.HomeAdvantage < 0 || p.HomeAdvantage > per10k/10:
		return bad("home advantage")
	case p.ContestReference <= 0 || p.ContestReference >= per10k || p.ContestPermille < 0 || p.ContestPermille > 2*permille:
		return bad("contests")
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
	case p.DriftDepth < 0 || p.DriftWidth < 0 || p.DriftTicks == 0:
		return bad("drift")
	case p.OffsideVision < 0 || p.OffsideVision > matches.PitchLength/4 || p.LineHold < 0 || p.RunStart < 0 || p.RunTicks == 0:
		return bad("offside and runs")
	case p.ThroughBallLead < 0 || p.ThroughBallLead > p.MaxPass || p.RunDepth < 0 || p.RunDepth > matches.PitchLength/4:
		return bad("runs in behind")
	case p.MarkDistance < 0 || p.TightMarkRadius < 0 || p.MarkRadius <= p.TightMarkRadius:
		return bad("marking")
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
			p.MentalityConcedePermille[m] <= 0 || p.MentalityConcedePermille[m] > 4*permille ||
			p.Pressers[m] < 0 || p.Pressers[m] > matches.StartersPerTeam-1 || p.RunPPM[m] < 0 || p.RunPPM[m] > ppm {
			return bad("mentality settings")
		}
	}
	return nil
}
