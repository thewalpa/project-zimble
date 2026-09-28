package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

// BenchmarkCompleteMatch measures Start plus a full 90 minutes (two Advance
// calls) with a reused output buffer and no frames.
func BenchmarkCompleteMatch(b *testing.B) {
	e := engine(b)
	in := input(1, 60, 60)
	random := enginetest.Random(e, 1)
	var dst matches.MatchStepResult
	b.ReportAllocs()
	for b.Loop() {
		s, err := e.Start(in, random)
		if err != nil {
			b.Fatal(err)
		}
		for range 2 {
			if err := s.Advance(matches.AdvanceRequest{ToMinute: 90}, &dst); err != nil {
				b.Fatal(err)
			}
		}
	}
	if dst.Status != matches.MatchFinished {
		b.Fatal("match did not finish")
	}
}
