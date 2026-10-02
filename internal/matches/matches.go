// Package matches defines the match-engine contract: detached match input,
// session controls, match-local events and the final outcome. Engines live
// in subpackages and keep their state and calculations private.
//
// A session never reads or writes world state. The application builds a
// MatchInput from module queries, runs a session, and later applies the
// finished outcome through the owning modules.
//
// All enum values are durable; never reorder them.
package matches

import (
	"errors"
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

const (
	StartersPerTeam   = 11
	HalfTimeMinute    = 45
	RegulationMinutes = 90
	MinRating         = 1
	MaxRating         = 100
	// MaxCondition is full fitness; condition is 1..100 in a match input.
	MaxCondition = 100
)

var (
	ErrInvalidInput   = errors.New("matches: invalid input")
	ErrInvalidRequest = errors.New("matches: invalid advance request")
	ErrInvalidCommand = errors.New("matches: invalid command")
	ErrMatchFinished  = errors.New("matches: match is finished")
	ErrUnsupported    = errors.New("matches: unsupported capability")
)

// Side identifies the home or away team.
type Side uint8

const (
	Home Side = 1
	Away Side = 2
)

func (s Side) Valid() bool { return s == Home || s == Away }

// Index returns 0 for Home and 1 for Away, for [2]-arrays.
func (s Side) Index() int { return int(s) - 1 }

func (s Side) Opponent() Side { return 3 - s }

func (s Side) String() string {
	switch s {
	case Home:
		return "home"
	case Away:
		return "away"
	}
	return fmt.Sprintf("Side(%d)", uint8(s))
}

// Role is the position a player fills in this match.
type Role uint8

const (
	Goalkeeper Role = 1
	Defender   Role = 2
	Midfielder Role = 3
	Forward    Role = 4
)

func (r Role) Valid() bool { return r >= Goalkeeper && r <= Forward }

// Ratings are a detached copy of a player's attributes on the 1..100 scale.
// Dribbling is keeping the ball in a duel and first touch; Pace is top speed
// and Acceleration the first steps of a sprint; Positioning is off-ball runs
// for attackers and marking for defenders.
type Ratings struct {
	Goalkeeping, Defending, Passing, Finishing, Pace, Stamina uint8
	Dribbling, Heading, Strength, Acceleration, Positioning   uint8
}

func (r Ratings) valid() bool {
	for _, v := range [...]uint8{
		r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace, r.Stamina,
		r.Dribbling, r.Heading, r.Strength, r.Acceleration, r.Positioning,
	} {
		if v < MinRating || v > MaxRating {
			return false
		}
	}
	return true
}

// PlayerInput is one selected player. Condition is their fitness at
// kickoff, 1..MaxCondition.
type PlayerInput struct {
	Player    ids.PlayerID
	Role      Role
	Ratings   Ratings
	Condition uint8
}

// Mentality is the supported tactical choice.
type Mentality uint8

const (
	Defensive Mentality = 1
	Balanced  Mentality = 2
	Attacking Mentality = 3
)

func (m Mentality) Valid() bool { return m >= Defensive && m <= Attacking }

func (m Mentality) String() string {
	switch m {
	case Defensive:
		return "defensive"
	case Balanced:
		return "balanced"
	case Attacking:
		return "attacking"
	}
	return fmt.Sprintf("Mentality(%d)", uint8(m))
}

type Tactics struct {
	Mentality Mentality
}

// TeamInput is one side's validated selection. Starters are in slot order.
type TeamInput struct {
	Team     ids.TeamID
	Tactics  Tactics
	Starters []PlayerInput
	Bench    []PlayerInput
}

// Rules are the competition's match rules. In a knockout match a draw after
// regulation is decided by a penalty shootout; it needs an engine with the
// Penalties capability.
type Rules struct {
	MaxSubstitutions uint8
	MaxBench         uint8
	Knockout         bool
}

// MatchInput is logically immutable input for one match. Engines copy what
// they need in Start and never retain references to its slices.
type MatchInput struct {
	Match ids.FixtureID
	Home  TeamInput
	Away  TeamInput
	Rules Rules
}

// Team returns the input for a side.
func (in *MatchInput) Team(s Side) *TeamInput {
	if s == Away {
		return &in.Away
	}
	return &in.Home
}

// Validate checks the input against the contract. maxBench is the engine's
// bench-size limit.
func (in *MatchInput) Validate(maxBench int) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrInvalidInput}, args...)...)
	}
	if in == nil {
		return fail("nil input")
	}
	if !in.Match.Valid() {
		return fail("match ID %d", in.Match)
	}
	if int(in.Rules.MaxBench) > maxBench || in.Rules.MaxSubstitutions > in.Rules.MaxBench {
		return fail("rules %+v exceed bench limit %d", in.Rules, maxBench)
	}
	if !in.Home.Team.Valid() || !in.Away.Team.Valid() || in.Home.Team == in.Away.Team {
		return fail("teams %d and %d", in.Home.Team, in.Away.Team)
	}
	seen := map[ids.PlayerID]bool{}
	for _, side := range [...]Side{Home, Away} {
		t := in.Team(side)
		if !t.Tactics.Mentality.Valid() {
			return fail("%s mentality %d", side, t.Tactics.Mentality)
		}
		if len(t.Starters) != StartersPerTeam {
			return fail("%s has %d starters, want %d", side, len(t.Starters), StartersPerTeam)
		}
		if len(t.Bench) > int(in.Rules.MaxBench) {
			return fail("%s bench has %d players, max %d", side, len(t.Bench), in.Rules.MaxBench)
		}
		keepers := 0
		for i, p := range append(t.Starters[:len(t.Starters):len(t.Starters)], t.Bench...) {
			if !p.Player.Valid() || seen[p.Player] {
				return fail("%s player ID %d invalid or duplicated", side, p.Player)
			}
			seen[p.Player] = true
			if !p.Role.Valid() || !p.Ratings.valid() || p.Condition == 0 || p.Condition > MaxCondition {
				return fail("%s player %d has invalid role, ratings or condition", side, p.Player)
			}
			if i < StartersPerTeam && p.Role == Goalkeeper {
				keepers++
			}
		}
		if keepers != 1 {
			return fail("%s starts %d goalkeepers, want 1", side, keepers)
		}
	}
	return nil
}

// Period is a phase of regulation time.
type Period uint8

const (
	FirstHalf  Period = 1
	HalfTime   Period = 2
	SecondHalf Period = 3
	FullTime   Period = 4
)

// MatchPosition is where a session stands. Minute counts regulation minutes
// already simulated: 0 at kickoff, 45 at half time, 90 at full time.
type MatchPosition struct {
	Period Period
	Minute uint16
}

// AdvanceRequest asks a session to simulate up to and including ToMinute
// (current minute..90). The session stops earlier at mandatory boundaries.
// Frames asks for one Frame per simulated instant; only engines with the
// PositionalFrames capability accept it, others return ErrUnsupported.
// Asking for frames never changes the match.
type AdvanceRequest struct {
	ToMinute uint16
	Frames   bool
}

type MatchStatus uint8

const (
	MatchRunning          MatchStatus = 1 // stopped at the requested minute
	MatchDecisionRequired MatchStatus = 2 // stopped at a mandatory boundary
	MatchFinished         MatchStatus = 3
)

// StopReason says why Advance returned.
type StopReason uint8

const (
	StopTarget   StopReason = 1
	StopHalfTime StopReason = 2
	StopFullTime StopReason = 3
)

type CommandKind uint8

const (
	CommandSubstitute   CommandKind = 1 // uses Side, Out, In
	CommandSetMentality CommandKind = 2 // uses Side, Mentality
	CommandSetRoles     CommandKind = 3 // uses Side, Roles
)

// MatchCommand is a manager decision. It takes effect from the next
// simulated minute. Roles, for CommandSetRoles, are the new roles of the
// side's eleven players on the pitch in slot order (MatchView.OnPitch); see
// CheckRoles.
type MatchCommand struct {
	Kind      CommandKind
	Side      Side
	Out, In   ids.PlayerID
	Mentality Mentality
	Roles     [StartersPerTeam]Role
}

// CheckRoles validates a change of formation: every new role is valid, the
// goalkeeper's slot is the same (a goalkeeper stays in goal and no one else
// goes there), and at least one slot changes. current and next are in slot
// order. Engines apply it to CommandSetRoles; clients can use it to check a
// formation before sending it.
func CheckRoles(current, next [StartersPerTeam]Role) error {
	changed := false
	for slot, r := range next {
		if !r.Valid() {
			return fmt.Errorf("%w: slot %d has role %d", ErrInvalidCommand, slot, r)
		}
		if (r == Goalkeeper) != (current[slot] == Goalkeeper) {
			return fmt.Errorf("%w: slot %d: the goalkeeper keeps his slot and no one else takes it", ErrInvalidCommand, slot)
		}
		changed = changed || r != current[slot]
	}
	if !changed {
		return fmt.Errorf("%w: the roles are already those", ErrInvalidCommand)
	}
	return nil
}

type EventKind uint8

const (
	EventGoal            EventKind = 1 // Player scored
	EventSubstitution    EventKind = 2 // Player came on, Other went off
	EventMentalityChange EventKind = 3 // Mentality is the new setting
	EventPeriodEnd       EventKind = 4 // Period ended (FirstHalf or SecondHalf)
	EventFormationChange EventKind = 5 // Roles are the side's new roles, in slot order
)

// MatchEvent is a match-local football fact, not a world domain event.
// Seq starts at 1 and increases by one across the session.
type MatchEvent struct {
	Seq       uint32
	Minute    uint16
	Kind      EventKind
	Side      Side
	Player    ids.PlayerID
	Other     ids.PlayerID
	Mentality Mentality
	Period    Period
	Roles     [StartersPerTeam]Role
}

// Pitch geometry of positional frames, in centimetres. X runs along the
// pitch, Y across it; the goals are centred on Y = PitchWidth/2 at X = 0
// and X = PitchLength. The home side attacks towards X = PitchLength in
// the first half and towards X = 0 in the second.
//
// A side's flanks follow its own direction of play: the first slot of a
// line (in selection.Lineup.Starters order, or MatchInput's starter order)
// is on the side's left flank looking upfield from its own goal, and the
// last slot of the line on its right. For a side attacking towards
// X = PitchLength the left touchline is Y = 0; for one attacking towards
// X = 0 it is Y = PitchWidth. A pitch drawn with the attack at the top has
// each line's slots left to right in slot order for both sides.
const (
	PitchLength = 10_500
	PitchWidth  = 6_800
	GoalWidth   = 732
)

// PitchPoint is a position on the pitch, in centimetres.
type PitchPoint struct {
	X, Y int32
}

// Frame is the ball and the players on the pitch at one instant, from an
// engine with the PositionalFrames capability. Millis is the regulation
// clock: 0 at kickoff, 45*60_000 at half time. Players are indexed by
// Side.Index() and slot, as View.OnPitch of the same step: nobody changes
// slot during one Advance call. Carrier is zero while the ball is loose or
// out of play. A frame is a presentation of the match, not a result.
type Frame struct {
	Millis  uint32
	Ball    PitchPoint
	Carrier ids.PlayerID
	Players [2][StartersPerTeam]PitchPoint
}

// MatchView is the live state after a step. Fixed-size arrays, indexed by
// Side.Index(), so it never aliases session memory.
type MatchView struct {
	Score             [2]uint16
	Shootout          [2]uint16 // after a shootout: penalties scored
	Mentality         [2]Mentality
	Roles             [2][StartersPerTeam]Role // current roles, slot order
	SubstitutionsUsed [2]uint8
	OnPitch           [2][StartersPerTeam]ids.PlayerID // slot order
	Stats             MatchStats                       // so far
}

// TeamStats are one side's match statistics. A shot is on target when it
// scores or the goalkeeper saves it; a save is the goalkeeper stopping a
// shot on target (a shot blocked by an outfield player is neither). A pass
// is completed when a team-mate controls it. Tackles are challenges that
// take the ball off the carrier. Offsides are the times one of the side's
// players was caught offside. PossessionPermille is the side's share of the
// time a player had the ball under control.
type TeamStats struct {
	Shots, ShotsOnTarget    uint16
	Passes, PassesCompleted uint16
	Tackles, Saves          uint16
	Offsides                uint16
	PossessionPermille      uint16
}

// MatchStats are both sides' statistics, indexed by Side.Index(), from an
// engine with the DetailedStats capability. Without it Available is false
// and the statistics are unavailable, not zero.
type MatchStats struct {
	Available bool
	Teams     [2]TeamStats
}

// Validate checks that the statistics are consistent: none without
// Available; on-target shots within shots, completed passes within passes,
// saves within the opponents' shots on target, and possession shares that
// add up to 1000, or are both zero before anyone has had the ball.
func (s MatchStats) Validate() error {
	if !s.Available {
		if s.Teams != [2]TeamStats{} {
			return fmt.Errorf("%w: statistics %+v marked unavailable", ErrInvalidInput, s.Teams)
		}
		return nil
	}
	for i, t := range s.Teams {
		if t.ShotsOnTarget > t.Shots || t.PassesCompleted > t.Passes || t.Saves > s.Teams[1-i].ShotsOnTarget {
			return fmt.Errorf("%w: %s statistics %+v are inconsistent", ErrInvalidInput, Side(i+1), t)
		}
	}
	if p := int(s.Teams[0].PossessionPermille) + int(s.Teams[1].PossessionPermille); p != 1000 && p != 0 {
		return fmt.Errorf("%w: possession shares add up to %d permille", ErrInvalidInput, p)
	}
	return nil
}

type ResultStatus uint8

const (
	ResultPending   ResultStatus = 1 // session not finished; no result yet
	ResultCompleted ResultStatus = 2 // regulation played to full time
	// 3 (abandoned) and 4 (awarded) are reserved; no engine produces them yet.
)

// Resolution says which play the score includes.
type Resolution uint8

const (
	ResolutionRegulation Resolution = 1 // 90 minutes only
	// 2 (extra time) is reserved.
	ResolutionPenalties Resolution = 3 // level after 90 minutes, then a penalty shootout
)

type Goal struct {
	Minute uint16
	Side   Side
	Scorer ids.PlayerID
}

// Participation is one player's time on the pitch: minutes
// (OnMinute, OffMinute], so Minutes = OffMinute - OnMinute.
type Participation struct {
	Player    ids.PlayerID
	Side      Side
	Started   bool
	OnMinute  uint16
	OffMinute uint16
}

func (p Participation) Minutes() uint16 { return p.OffMinute - p.OnMinute }

// MatchOutcome is the result. Only a ResultCompleted outcome is a result;
// score, goals, participants and stats are empty while ResultPending. Goals
// are in match order; participants are home then away, starters in slot
// order, then substitutes in the order they came on. The live statistics
// are in MatchView.
type MatchOutcome struct {
	Match         ids.FixtureID
	Status        ResultStatus
	Resolution    Resolution
	EngineID      string
	EngineVersion uint32
	Score         [2]uint16
	Shootout      [2]uint16 // shootout penalties scored; zero unless Resolution is ResolutionPenalties
	Goals         []Goal
	Participants  []Participation
	Stats         MatchStats
}

// Winner returns the side that won a completed outcome: by score, else by
// shootout. ok is false for a draw.
func (o MatchOutcome) Winner() (Side, bool) {
	for _, pair := range [][2]uint16{o.Score, o.Shootout} {
		switch {
		case pair[0] > pair[1]:
			return Home, true
		case pair[1] > pair[0]:
			return Away, true
		}
	}
	return 0, false
}

// MatchStepResult is caller-owned output for Advance. Advance resets the
// lengths of Events, Frames, Outcome.Goals and Outcome.Participants to
// zero, keeps their capacity and appends copies. Nothing in it aliases session memory;
// it stays valid until the caller passes it to Advance again.
type MatchStepResult struct {
	Position MatchPosition
	Status   MatchStatus
	Stop     StopReason
	View     MatchView
	Events   []MatchEvent
	Frames   []Frame // only when requested; see AdvanceRequest
	Outcome  MatchOutcome
}

// RandomState is the versioned representation of a session's random stream.
type RandomState struct {
	Words     [4]uint64
	Algorithm uint16
	Version   uint16
}

// AlgorithmSplitMix64 uses Words[0] as the core/random SplitMix64 state.
const AlgorithmSplitMix64 uint16 = 1

// FixtureRandom derives the dedicated stream for one fixture:
// random.Derive(worldSeed, "matches/"+engineID, engineVersion, fixture).
func FixtureRandom(seed random.Seed, engineID string, engineVersion uint32, fixture ids.FixtureID) RandomState {
	s := random.Derive(seed, "matches/"+engineID, uint64(engineVersion), uint64(fixture))
	return RandomState{Words: [4]uint64{s.State()}, Algorithm: AlgorithmSplitMix64, Version: random.Version}
}

type MatchCheckpoint struct {
	EngineID      string
	EngineVersion uint32
	SchemaVersion uint16
	Data          []byte
}

// Capabilities advertises optional behavior. Unsupported features are
// "unavailable", not zero: callers must not read absence as "none happened".
type Capabilities struct {
	Substitutions    bool
	Mentality        bool
	Formations       bool // CommandSetRoles
	ExtraTime        bool
	Penalties        bool
	Injuries         bool
	Cards            bool
	Checkpoints      bool
	PositionalFrames bool
	DetailedStats    bool
}

type Engine interface {
	ID() string
	Version() uint32
	Capabilities() Capabilities
	Start(input *MatchInput, random RandomState) (MatchSession, error)
	Restore(checkpoint MatchCheckpoint) (MatchSession, error)
}

type MatchSession interface {
	// Advance simulates toward request.ToMinute and writes the step into
	// dst. On error, dst and the session are unchanged.
	Advance(request AdvanceRequest, dst *MatchStepResult) error
	// Apply validates and applies a command at the current position. On
	// error the session is unchanged.
	Apply(command MatchCommand) error
	Checkpoint() (MatchCheckpoint, error)
}
