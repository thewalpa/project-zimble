package simple

import (
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

// The shared fixtures: home team 1 (players 101-118), away team 2 (players
// 201-218). See enginetest.
var (
	input     = enginetest.Input
	run       = enginetest.Play
	cloneStep = enginetest.Clone
	sub       = enginetest.Sub
	mentality = enginetest.SetMentality
	fullMatch = enginetest.FullMatch
)

func engine(t testing.TB) *Engine {
	t.Helper()
	e, err := New(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func rs(fixture ids.FixtureID) matches.RandomState {
	return matches.FixtureRandom(enginetest.Seed, EngineID, ModelVersion, fixture)
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
