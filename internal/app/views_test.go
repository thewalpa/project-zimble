package app

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
)

// The promotion views follow the pinned links: the first divisions send their
// bottom places down, the second divisions their top places up, and a cup or
// an unlinked league none.
func TestPromotionViews(t *testing.T) {
	w := newWorld(t, 42)
	for _, l := range w.Promotions() {
		if up, down := w.PromotionPlaces(l.Upper); up != 0 || down != l.Places {
			t.Fatalf("upper league %d: up %d, down %d, want 0 and %d", l.Upper, up, down, l.Places)
		}
		if up, down := w.PromotionPlaces(l.Lower); up != l.Places || down != 0 {
			t.Fatalf("lower league %d: up %d, down %d, want %d and 0", l.Lower, up, down, l.Places)
		}
		upper := competitions.SeasonRef{Competition: l.Upper, Season: 1}
		lower := competitions.SeasonRef{Competition: l.Lower, Season: 1}
		table, _ := w.Table(upper)
		n := len(table.Rows)
		for pos := 1; pos <= n; pos++ {
			promoted, relegated := w.SeasonMove(upper, pos)
			if promoted || relegated != (pos > n-l.Places) {
				t.Fatalf("upper position %d: promoted %v, relegated %v", pos, promoted, relegated)
			}
			promoted, relegated = w.SeasonMove(lower, pos)
			if relegated || promoted != (pos <= l.Places) {
				t.Fatalf("lower position %d: promoted %v, relegated %v", pos, promoted, relegated)
			}
		}
	}
	if up, down := w.PromotionPlaces(3); up != 0 || down != 0 { // the Continental Cup
		t.Fatalf("cup: up %d, down %d", up, down)
	}
	if p, r := w.SeasonMove(competitions.SeasonRef{Competition: 1, Season: 1}, 0); p || r {
		t.Fatal("position 0 moved")
	}
}
