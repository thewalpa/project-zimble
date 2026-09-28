package tick

import (
	"fmt"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// render draws a frame as text, one character per metre along the pitch
// and four across it: home players A-K and away players a-k by slot, the
// ball o, and the carrier's letter in brackets beside the frame's clock.
func render(f matches.Frame, onPitch [2][matches.StartersPerTeam]uint64) string {
	const cols, rows = matches.PitchLength/100 + 1, matches.PitchWidth/400 + 1
	var grid [rows][cols]byte
	for r := range grid {
		for c := range grid[r] {
			grid[r][c] = ' '
			if c == 0 || c == cols-1 || c == cols/2 {
				grid[r][c] = '|'
			}
		}
	}
	for _, r := range []int{rows/2 - 2, rows/2 - 1, rows / 2, rows/2 + 1, rows/2 + 2} {
		grid[r][0], grid[r][cols-1] = '#', '#'
	}
	put := func(p matches.PitchPoint, ch byte) { grid[p.Y/400][p.X/100] = ch }
	carrier := "-"
	for side, first := range []byte{'A', 'a'} {
		for slot, p := range f.Players[side] {
			put(p, first+byte(slot))
			if onPitch[side][slot] == uint64(f.Carrier) && f.Carrier != 0 {
				carrier = string(first + byte(slot))
			}
		}
	}
	put(f.Ball, 'o')
	var b strings.Builder
	fmt.Fprintf(&b, "%02d:%04.1f carrier %s\n", f.Millis/60_000, float64(f.Millis%60_000)/1000, carrier)
	b.WriteString("+" + strings.Repeat("-", cols) + "+\n")
	for _, row := range grid {
		b.WriteString("|" + string(row[:]) + "|\n")
	}
	b.WriteString("+" + strings.Repeat("-", cols) + "+\n")
	return b.String()
}

// TestWatch prints a stretch of play for eyeballing the model:
//
//	go test ./internal/matches/tick -run TestWatch -v
func TestWatch(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("prints frames; run with -v")
	}
	s := start(t, input(1, 60, 60))
	var dst matches.MatchStepResult
	if err := s.Advance(matches.AdvanceRequest{ToMinute: 1, Frames: true}, &dst); err != nil {
		t.Fatal(err)
	}
	var ids [2][matches.StartersPerTeam]uint64
	for side := range ids {
		for slot, id := range dst.View.OnPitch[side] {
			ids[side][slot] = uint64(id)
		}
	}
	for i, f := range dst.Frames {
		if i%5 == 4 { // one a second
			t.Log("\n" + render(f, ids))
		}
	}
}
