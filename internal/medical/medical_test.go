package medical

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func store(t *testing.T, conditions ...uint8) *Store {
	t.Helper()
	var recs []Record
	for i, c := range conditions {
		recs = append(recs, Record{Player: ids.PlayerID(i + 1), Condition: c})
	}
	s, err := New(DefaultParams(), recs)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRejectsInvalidState(t *testing.T) {
	for name, recs := range map[string][]Record{
		"zero player":        {{Player: 0, Condition: 1}},
		"duplicate player":   {{Player: 1, Condition: 1}, {Player: 1, Condition: 2}},
		"condition too high": {{Player: 1, Condition: MaxCondition + 1}},
		"below the floor":    {{Player: 1, Condition: DefaultParams().MinCondition - 1}},
	} {
		if _, err := New(DefaultParams(), recs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bad := DefaultParams()
	bad.MinCondition = 0
	if _, err := New(bad, nil); err == nil {
		t.Error("zero MinCondition accepted")
	}
	bad = DefaultParams()
	bad.RecoveryBase = 100*int(MaxCondition) + 1
	if _, err := New(bad, nil); err == nil {
		t.Error("recovery above MaxCondition per day accepted")
	}
}

// Exposure costs more for more minutes and for less stamina, never goes
// below MinCondition, and leaves unexposed players alone.
func TestExposureRules(t *testing.T) {
	p := DefaultParams()
	s := store(t, MaxCondition, MaxCondition, MaxCondition, 25, MaxCondition)
	plan, err := s.PlanExposure([]Exposure{
		{Player: 3, Minutes: 90, Stamina: 90},
		{Player: 1, Minutes: 90, Stamina: 30},
		{Player: 2, Minutes: 30, Stamina: 30},
		{Player: 4, Minutes: 90, Stamina: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	c := func(id ids.PlayerID) int { v, _ := s.Condition(id); return int(v) }
	full := int(MaxCondition)
	switch {
	case c(1) != full-p.Drain(90, 30) || c(3) != full-p.Drain(90, 90):
		t.Fatalf("drain: %d, %d", c(1), c(3))
	case !(c(1) < c(2) && c(2) < full) || !(c(1) < c(3)):
		t.Fatal("more minutes or less stamina must cost more")
	case c(4) != int(p.MinCondition):
		t.Fatalf("floor: %d, want %d", c(4), p.MinCondition)
	case c(5) != full:
		t.Fatal("unexposed player changed")
	}
}

func TestRecoveryRules(t *testing.T) {
	p := DefaultParams()
	s := store(t, 50, 50, MaxCondition-1)
	rest := []Rest{{Player: 3, Stamina: 50}, {Player: 1, Stamina: 30}, {Player: 2, Stamina: 90}}
	plan, err := s.PlanRecovery(rest)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	c := func(id ids.PlayerID) int { v, _ := s.Condition(id); return int(v) }
	if c(1) != 50+p.Recovery(30) || c(2) != 50+p.Recovery(90) || c(1) >= c(2) {
		t.Fatalf("recovery %d, %d", c(1), c(2))
	}
	if c(3) != int(MaxCondition) {
		t.Fatalf("recovery not capped: %d", c(3))
	}
}

func TestPlansValidateAndChangeNothingUntilApplied(t *testing.T) {
	s := store(t, 80, 80)
	want := s.Snapshot()
	for name, ex := range map[string][]Exposure{
		"unknown player": {{Player: 9, Minutes: 90, Stamina: 10}},
		"twice":          {{Player: 1, Minutes: 45, Stamina: 10}, {Player: 1, Minutes: 45, Stamina: 10}},
		"too long":       {{Player: 1, Minutes: MaxMinutes + 1, Stamina: 10}},
		"no stamina":     {{Player: 1, Minutes: 90, Stamina: 0}},
		"stamina 101":    {{Player: 1, Minutes: 90, Stamina: 101}},
	} {
		if _, err := s.PlanExposure(ex); err == nil {
			t.Errorf("exposure %s: accepted", name)
		}
	}
	for name, rest := range map[string][]Rest{
		"missing player": {{Player: 1, Stamina: 10}},
		"extra player":   {{Player: 1, Stamina: 10}, {Player: 2, Stamina: 10}, {Player: 3, Stamina: 10}},
		"wrong player":   {{Player: 1, Stamina: 10}, {Player: 3, Stamina: 10}},
		"bad stamina":    {{Player: 1, Stamina: 10}, {Player: 2, Stamina: 0}},
	} {
		if _, err := s.PlanRecovery(rest); err == nil {
			t.Errorf("recovery %s: accepted", name)
		}
	}
	if _, err := s.PlanExposure([]Exposure{{Player: 1, Minutes: 90, Stamina: 10}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("planning changed the store")
	}
}

func TestStalePlanIsRejected(t *testing.T) {
	s := store(t, 80, 80)
	a, _ := s.PlanExposure([]Exposure{{Player: 1, Minutes: 90, Stamina: 10}})
	b, _ := s.PlanExposure([]Exposure{{Player: 2, Minutes: 90, Stamina: 10}})
	if err := s.Apply(a); err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	if err := s.Apply(b); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale plan: err = %v", err)
	}
	if err := s.Apply(a); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("plan applied twice: err = %v", err)
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("rejected plans changed the store")
	}
}

func TestStoreSharesNoMemory(t *testing.T) {
	in := []Record{{Player: 1, Condition: 70}}
	s, _ := New(DefaultParams(), in)
	in[0].Condition = 1
	s.Records()[0].Condition = 2
	plan, _ := s.PlanExposure([]Exposure{{Player: 1, Minutes: 10, Stamina: 50}})
	plan.Changes()[0].Condition = 3
	if c, _ := s.Condition(1); c != 70 {
		t.Fatalf("condition %d; store shares memory", c)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Condition(1); c != 70-uint8(DefaultParams().Drain(10, 50)) {
		t.Fatalf("condition %d after apply", c)
	}
}

// Rates are finer than a point; each result is rounded half up once.
func TestRoundingOfWholePoints(t *testing.T) {
	p := DefaultParams()
	for _, c := range []struct {
		minutes uint16
		stamina uint8
		want    int
	}{
		{90, 30, 31}, // 90 * 0.34 = 30.6
		{90, 50, 27}, // 90 * 0.30 = 27.0
		{90, 75, 23}, // 90 * 0.25 = 22.5, half up
		{90, 90, 20}, // 90 * 0.22 = 19.8
		{1, 100, 0},  // 0.2
		{0, 1, 0},
	} {
		if got := p.Drain(c.minutes, c.stamina); got != c.want {
			t.Errorf("Drain(%d, %d) = %d, want %d", c.minutes, c.stamina, got, c.want)
		}
	}
	for stamina, want := range map[uint8]int{1: 3, 20: 3, 25: 4, 30: 4, 75: 5, 100: 5} {
		if got := p.Recovery(stamina); got != want {
			t.Errorf("Recovery(%d) = %d, want %d", stamina, got, want)
		}
	}
}
