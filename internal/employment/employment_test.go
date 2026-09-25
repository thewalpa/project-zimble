package employment

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
)

var terms = Contract{Expires: 1000, WeeklyWage: 50_000}

func TestNewRejectsInvalidAssignments(t *testing.T) {
	cases := map[string][]Assignment{
		"zero player":     {{Player: 0, Club: 1, Team: 1, Contract: terms}},
		"zero club":       {{Player: 1, Club: 0, Team: 1, Contract: terms}},
		"zero team":       {{Player: 1, Club: 1, Team: 0, Contract: terms}},
		"two assignments": {{Player: 1, Club: 1, Team: 1, Contract: terms}, {Player: 1, Club: 2, Team: 2, Contract: terms}},
		"no contract":     {{Player: 1, Club: 1, Team: 1}},
		"no wage":         {{Player: 1, Club: 1, Team: 1, Contract: Contract{Expires: 1000}}},
		"negative wage":   {{Player: 1, Club: 1, Team: 1, Contract: Contract{Expires: 1000, WeeklyWage: -1}}},
		"expired at 0":    {{Player: 1, Club: 1, Team: 1, Contract: Contract{WeeklyWage: 1}}},
	}
	for name, in := range cases {
		if _, err := New(in); err == nil {
			t.Errorf("%s: New succeeded, want error", name)
		}
	}
}

func TestSquadIsSortedAndFiltered(t *testing.T) {
	s, err := New([]Assignment{
		{Player: 5, Club: 1, Team: 1, Contract: terms},
		{Player: 2, Club: 2, Team: 2, Contract: Contract{Expires: 5, WeeklyWage: 7}},
		{Player: 3, Club: 1, Team: 1, Contract: Contract{Expires: 9, WeeklyWage: 25_000}},
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

	if bill, err := s.WageBill(1); err != nil || bill != 75_000 {
		t.Fatalf("WageBill(1) = %d, %v", bill, err)
	}
	if bill, _ := s.WageBill(9); bill != 0 {
		t.Fatalf("unknown club bill %d", bill)
	}
}

func TestWageBillOverflow(t *testing.T) {
	huge := Contract{Expires: 1, WeeklyWage: math.MaxInt64 / 2}
	s, err := New([]Assignment{{Player: 1, Club: 1, Team: 1, Contract: huge}, {Player: 2, Club: 1, Team: 1, Contract: huge}, {Player: 3, Club: 1, Team: 1, Contract: huge}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WageBill(1); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
}
