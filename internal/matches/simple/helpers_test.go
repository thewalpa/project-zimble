package simple

import (
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// Starters: GK, 4 DF, 4 MF, 2 FW. Bench: GK, DF, MF, FW, DF, MF, FW.
var (
	starterRoles = []matches.Role{1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4}
	benchRoles   = []matches.Role{1, 2, 3, 4, 2, 3, 4}
)

func clamp(v int) uint8 { return uint8(min(max(v, matches.MinRating), matches.MaxRating)) }

// ratings gives a role-appropriate profile around strength, varied by i.
func ratings(role matches.Role, strength, i int) matches.Ratings {
	v := i%5 - 2
	low := clamp(strength/3 + v)
	r := matches.Ratings{
		Goalkeeping: low, Defending: clamp(strength + v), Passing: clamp(strength - v),
		Finishing: clamp(strength + v), Pace: clamp(strength - v), Stamina: clamp(strength + 2*v),
	}
	switch role {
	case matches.Goalkeeper:
		r.Goalkeeping, r.Finishing = clamp(strength+v), low
	case matches.Defender:
		r.Finishing = clamp(strength - 4 + v)
	case matches.Forward:
		r.Defending = clamp(strength - 4 + v)
	}
	return r
}

func teamInput(team ids.TeamID, firstPlayer ids.PlayerID, strength int, m matches.Mentality) matches.TeamInput {
	t := matches.TeamInput{Team: team, Tactics: matches.Tactics{Mentality: m}}
	id := firstPlayer
	for i, role := range starterRoles {
		t.Starters = append(t.Starters, matches.PlayerInput{Player: id, Role: role, Ratings: ratings(role, strength, i), Condition: matches.MaxCondition})
		id++
	}
	for i, role := range benchRoles {
		t.Bench = append(t.Bench, matches.PlayerInput{Player: id, Role: role, Ratings: ratings(role, strength, i+11), Condition: matches.MaxCondition})
		id++
	}
	return t
}

// input returns a valid match: home team 1 (players 101-118), away team 2
// (players 201-218).
func input(fixture ids.FixtureID, homeStrength, awayStrength int) *matches.MatchInput {
	return &matches.MatchInput{
		Match: fixture,
		Home:  teamInput(1, 101, homeStrength, matches.Balanced),
		Away:  teamInput(2, 201, awayStrength, matches.Balanced),
		Rules: matches.Rules{MaxSubstitutions: 3, MaxBench: 7},
	}
}

func engine(t testing.TB) *Engine {
	t.Helper()
	e, err := New(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func rs(fixture ids.FixtureID) matches.RandomState {
	return matches.FixtureRandom(42, EngineID, ModelVersion, fixture)
}

func start(t testing.TB, in *matches.MatchInput) *session {
	t.Helper()
	s, err := engine(t).Start(in, rs(in.Match))
	if err != nil {
		t.Fatal(err)
	}
	return s.(*session)
}

func advance(t testing.TB, s matches.MatchSession, to uint16, dst *matches.MatchStepResult) {
	t.Helper()
	if err := s.Advance(matches.AdvanceRequest{ToMinute: to}, dst); err != nil {
		t.Fatalf("Advance(%d): %v", to, err)
	}
}

// timedCommand is a command applied when the session stops at Minute
// (and, for minute 45, at half time).
type timedCommand struct {
	Minute uint16
	Cmd    matches.MatchCommand
}

// run calls Advance once per stop, applies commands whose minute matches
// the position reached, and returns every event plus the final step. A stop
// beyond half time halts at 45 first, so plans must still reach full time.
func run(t testing.TB, s matches.MatchSession, stops []uint16, cmds []timedCommand) ([]matches.MatchEvent, matches.MatchStepResult) {
	t.Helper()
	var dst matches.MatchStepResult
	var events []matches.MatchEvent
	applied := map[int]bool{}
	for _, stop := range stops {
		advance(t, s, stop, &dst)
		events = append(events, dst.Events...)
		for i, c := range cmds {
			if !applied[i] && c.Minute == dst.Position.Minute {
				if err := s.Apply(c.Cmd); err != nil {
					t.Fatalf("Apply at %d: %v", c.Minute, err)
				}
				applied[i] = true
			}
		}
	}
	if dst.Status != matches.MatchFinished {
		t.Fatalf("run ended at %+v, not full time", dst.Position)
	}
	for i := range cmds {
		if !applied[i] {
			t.Fatalf("command %d at minute %d was never applied", i, cmds[i].Minute)
		}
	}
	return events, cloneStep(dst)
}

func cloneStep(r matches.MatchStepResult) matches.MatchStepResult {
	r.Events = slices.Clone(r.Events)
	r.Outcome.Goals = slices.Clone(r.Outcome.Goals)
	r.Outcome.Participants = slices.Clone(r.Outcome.Participants)
	return r
}

// sessionState is a deep copy of everything a session owns.
type sessionState struct {
	S       session
	RNG     random.Stream
	Goals   []matches.Goal
	Pending []matches.MatchEvent
}

func capture(s *session) sessionState {
	c := sessionState{S: *s, RNG: *s.rng, Goals: slices.Clone(s.goals), Pending: slices.Clone(s.pending)}
	c.S.rng, c.S.goals, c.S.pending = nil, nil, nil
	return c
}

func sub(side matches.Side, out, in ids.PlayerID) matches.MatchCommand {
	return matches.MatchCommand{Kind: matches.CommandSubstitute, Side: side, Out: out, In: in}
}

func mentality(side matches.Side, m matches.Mentality) matches.MatchCommand {
	return matches.MatchCommand{Kind: matches.CommandSetMentality, Side: side, Mentality: m}
}
