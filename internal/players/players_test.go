package players

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func validProfile(id ids.PlayerID) Profile {
	return Profile{Player: id, Position: Midfielder, Attributes: Attributes{5, 10, 15, 8, 12, 20}}
}

func TestNewRejectsInvalidProfiles(t *testing.T) {
	low, high := validProfile(2), validProfile(2)
	low.Attributes[Pace] = 0
	high.Attributes[Stamina] = 21
	noPos := validProfile(2)
	noPos.Position = 0

	cases := map[string][]Profile{
		"zero ID":         {validProfile(0)},
		"duplicate ID":    {validProfile(1), validProfile(1)},
		"rating below 1":  {validProfile(1), low},
		"rating above 20": {validProfile(1), high},
		"no position":     {noPos},
	}
	for name, in := range cases {
		if _, err := New(in); err == nil {
			t.Errorf("%s: New succeeded, want error", name)
		}
	}
}

func TestStoreIsIsolatedFromCallers(t *testing.T) {
	in := []Profile{validProfile(2), validProfile(1)}
	s, err := New(in)
	if err != nil {
		t.Fatal(err)
	}
	in[0].Attributes[Pace] = 1
	got, _ := s.Profile(2)
	if got.Attributes[Pace] != 12 {
		t.Fatalf("store changed via input slice: pace = %d", got.Attributes[Pace])
	}
	idsOut := s.PlayerIDs()
	if idsOut[0] != 1 || idsOut[1] != 2 {
		t.Fatalf("PlayerIDs = %v, want ascending", idsOut)
	}
	idsOut[0] = 99
	if again := s.PlayerIDs(); again[0] != 1 {
		t.Fatal("store changed via returned slice")
	}
}

func TestOverallTenthsUsesPositionKeyAttributes(t *testing.T) {
	p := Profile{Player: 1, Position: Forward, Attributes: Attributes{1, 1, 10, 15, 12, 1}}
	// (15 + 12 + 10) / 3 = 12.33 -> 123 tenths.
	if got := p.OverallTenths(); got != 123 {
		t.Fatalf("OverallTenths = %d, want 123", got)
	}
}
