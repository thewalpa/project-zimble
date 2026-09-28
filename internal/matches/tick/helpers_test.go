package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

var input = enginetest.Input

func engine(t testing.TB) *Engine {
	t.Helper()
	e, err := New(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func start(t testing.TB, in *matches.MatchInput) *session {
	t.Helper()
	return enginetest.Start(t, engine(t), in).(*session)
}

// playOut runs s to full time, optionally collecting frames.
func playOut(t testing.TB, s *session, frames bool) matches.MatchStepResult {
	t.Helper()
	var dst matches.MatchStepResult
	var all []matches.Frame
	for range 2 {
		if err := s.Advance(matches.AdvanceRequest{ToMinute: 90, Frames: frames}, &dst); err != nil {
			t.Fatal(err)
		}
		all = append(all, dst.Frames...)
	}
	dst.Frames = all
	return dst
}
