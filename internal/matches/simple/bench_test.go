package simple

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// BenchmarkCompleteMatch measures Start plus a full 90 minutes (two Advance
// calls, half time and full time) with a reused output buffer. Input
// construction is outside the timed loop. Baseline only.
func BenchmarkCompleteMatch(b *testing.B) {
	e := engine(b)
	in := input(1, 60, 60)
	random := rs(1)
	var dst matches.MatchStepResult
	b.ReportAllocs()
	for b.Loop() {
		s, err := e.Start(in, random)
		if err != nil {
			b.Fatal(err)
		}
		if err := s.Advance(matches.AdvanceRequest{ToMinute: 90}, &dst); err != nil {
			b.Fatal(err)
		}
		if err := s.Advance(matches.AdvanceRequest{ToMinute: 90}, &dst); err != nil {
			b.Fatal(err)
		}
	}
	if dst.Status != matches.MatchFinished {
		b.Fatal("match did not finish")
	}
}

// BenchmarkAdvanceOnly isolates the stepping loop from session creation.
func BenchmarkAdvanceOnly(b *testing.B) {
	e := engine(b)
	in := input(1, 60, 60)
	random := rs(1)
	var dst matches.MatchStepResult
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		s, err := e.Start(in, random)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		_ = s.Advance(matches.AdvanceRequest{ToMinute: 90}, &dst)
		_ = s.Advance(matches.AdvanceRequest{ToMinute: 90}, &dst)
	}
}
