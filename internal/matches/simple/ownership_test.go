package simple

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// Start copies its input: mutating the caller's input afterwards changes
// nothing, and Start itself does not modify the input.
func TestSessionDoesNotRetainInput(t *testing.T) {
	in := input(11, 60, 60)
	pristine := input(11, 60, 60)
	s := start(t, in)
	if !reflect.DeepEqual(in, pristine) {
		t.Fatal("Start modified its input")
	}
	for i := range in.Home.Starters {
		in.Home.Starters[i].Ratings = matches.Ratings{Goalkeeping: 1, Defending: 1, Passing: 1, Finishing: 1, Pace: 1, Stamina: 1}
		in.Away.Starters[i].Player += 1000
	}
	in.Home.Bench[0].Role = matches.Forward
	in.Away.Tactics.Mentality = matches.Attacking
	in.Home.Starters = nil

	stops := []uint16{30, 45, 70, 90}
	got, gotFinal := run(t, s, stops, standardCommands)
	want, wantFinal := run(t, start(t, pristine), stops, standardCommands)
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotFinal, wantFinal) {
		t.Fatal("mutating the input after Start changed the match")
	}
}

// Advance reuses dst's buffers, replaces rather than accumulates their
// contents, and never aliases session memory.
func TestAdvanceOutputBufferOwnership(t *testing.T) {
	dst := matches.MatchStepResult{
		Events:  make([]matches.MatchEvent, 0, 64),
		Outcome: matches.MatchOutcome{Goals: make([]matches.Goal, 0, 32), Participants: make([]matches.Participation, 0, 32)},
	}
	eventsBuf, goalsBuf, partsBuf := &dst.Events[:1][0], &dst.Outcome.Goals[:1][0], &dst.Outcome.Participants[:1][0]

	s := start(t, input(12, 60, 60))
	advance(t, s, 45, &dst)
	advance(t, s, 90, &dst)
	final := cloneStep(dst)
	if len(dst.Events) > 0 && &dst.Events[0] != eventsBuf {
		t.Fatal("Events buffer was reallocated despite sufficient capacity")
	}
	if &dst.Outcome.Participants[0] != partsBuf || (len(dst.Outcome.Goals) > 0 && &dst.Outcome.Goals[0] != goalsBuf) {
		t.Fatal("outcome buffers were reallocated despite sufficient capacity")
	}

	// Scribbling over dst must not reach the session.
	for i := range dst.Outcome.Goals {
		dst.Outcome.Goals[i] = matches.Goal{}
	}
	for i := range dst.Outcome.Participants {
		dst.Outcome.Participants[i].OffMinute = 0
	}
	dst.View.OnPitch[0][0] = 0
	advance(t, s, 90, &dst)
	final.Events = nil
	again := cloneStep(dst)
	again.Events = nil
	if !reflect.DeepEqual(again, final) {
		t.Fatal("modifying dst changed the session's outcome")
	}

	// Reusing a dst that holds a completed outcome for a new, unfinished
	// session replaces everything: no stale result, events or participants.
	s2 := start(t, input(13, 60, 60))
	advance(t, s2, 10, &dst)
	if dst.Outcome.Status != matches.ResultPending || len(dst.Outcome.Goals) != 0 || len(dst.Outcome.Participants) != 0 ||
		dst.Outcome.Match != 13 || dst.Outcome.Score != [2]uint16{} {
		t.Fatalf("stale outcome after reuse: %+v", dst.Outcome)
	}
	for _, e := range dst.Events {
		if e.Minute > 10 {
			t.Fatalf("stale event after reuse: %+v", e)
		}
	}
}

// Events already returned are not repeated, and pending command events are
// delivered exactly once even through no-op Advance calls.
func TestCommandEventsDeliveredOnce(t *testing.T) {
	s := start(t, input(14, 60, 60))
	var dst matches.MatchStepResult
	advance(t, s, 45, &dst)
	if err := s.Apply(sub(matches.Home, 110, 115)); err != nil {
		t.Fatal(err)
	}
	advance(t, s, 45, &dst) // no minutes played
	if len(dst.Events) != 1 || dst.Events[0].Kind != matches.EventSubstitution || dst.Events[0].Minute != 45 {
		t.Fatalf("no-op Advance events = %+v", dst.Events)
	}
	advance(t, s, 45, &dst)
	if len(dst.Events) != 0 {
		t.Fatalf("command event delivered twice: %+v", dst.Events)
	}
}
