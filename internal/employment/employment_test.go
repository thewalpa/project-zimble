package employment

import (
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func TestNewRejectsInvalidAssignments(t *testing.T) {
	cases := map[string][]Assignment{
		"zero player":     {{Player: 0, Club: 1, Team: 1}},
		"zero club":       {{Player: 1, Club: 0, Team: 1}},
		"zero team":       {{Player: 1, Club: 1, Team: 0}},
		"two assignments": {{Player: 1, Club: 1, Team: 1}, {Player: 1, Club: 2, Team: 2}},
	}
	for name, in := range cases {
		if _, err := New(in); err == nil {
			t.Errorf("%s: New succeeded, want error", name)
		}
	}
}

func TestSquadIsSortedAndFiltered(t *testing.T) {
	s, err := New([]Assignment{
		{Player: 5, Club: 1, Team: 1},
		{Player: 2, Club: 2, Team: 2},
		{Player: 3, Club: 1, Team: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Squad(1)
	if want := []ids.PlayerID{3, 5}; !slices.Equal(got, want) {
		t.Fatalf("Squad(1) = %v, want %v", got, want)
	}
	got[0] = 99
	if again := s.Squad(1); again[0] != 3 {
		t.Fatal("store changed via returned slice")
	}
}
