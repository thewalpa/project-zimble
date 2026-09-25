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
type Ratings struct {
	Goalkeeping, Defending, Passing, Finishing, Pace, Stamina uint8
}

func (r Ratings) valid() bool {
	for _, v := range [...]uint8{r.Goalkeeping, r.Defending, r.Passing, r.Finishing, r.Pace, r.Stamina} {
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

// Rules are the competition's match rules.
type Rules struct {
	MaxSubstitutions uint8
	MaxBench         uint8
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
type AdvanceRequest struct {
	ToMinute uint16
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
)

// MatchCommand is a manager decision. It takes effect from the next
// simulated minute.
type MatchCommand struct {
	Kind      CommandKind
	Side      Side
	Out, In   ids.PlayerID
	Mentality Mentality
}

type EventKind uint8

const (
	EventGoal            EventKind = 1 // Player scored
	EventSubstitution    EventKind = 2 // Player came on, Other went off
	EventMentalityChange EventKind = 3 // Mentality is the new setting
	EventPeriodEnd       EventKind = 4 // Period ended (FirstHalf or SecondHalf)
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
}

// MatchView is the live state after a step. Fixed-size arrays, indexed by
// Side.Index(), so it never aliases session memory.
type MatchView struct {
	Score             [2]uint16
	Mentality         [2]Mentality
	SubstitutionsUsed [2]uint8
	OnPitch           [2][StartersPerTeam]ids.PlayerID // slot order
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
	// 2 (extra time) and 3 (penalties) are reserved.
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
// score, goals and participants are empty while ResultPending. Goals are in
// match order; participants are home then away, starters in slot order,
// then substitutes in the order they came on.
type MatchOutcome struct {
	Match         ids.FixtureID
	Status        ResultStatus
	Resolution    Resolution
	EngineID      string
	EngineVersion uint32
	Score         [2]uint16
	Goals         []Goal
	Participants  []Participation
}

// MatchStepResult is caller-owned output for Advance. Advance resets the
// lengths of Events, Outcome.Goals and Outcome.Participants to zero, keeps
// their capacity and appends copies. Nothing in it aliases session memory;
// it stays valid until the caller passes it to Advance again.
type MatchStepResult struct {
	Position MatchPosition
	Status   MatchStatus
	Stop     StopReason
	View     MatchView
	Events   []MatchEvent
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
