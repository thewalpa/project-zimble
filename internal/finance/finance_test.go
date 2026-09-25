package finance

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
)

func opened(t *testing.T) *Store {
	t.Helper()
	s, err := New(Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.Plan(0, []Posting{{Club: 1, Kind: KindOpening, Amount: 1_000}, {Club: 2, Kind: KindOpening, Amount: 500}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	return s
}

// The balance is always the sum of the club's entries.
func TestBalanceIsTheSumOfEntries(t *testing.T) {
	s := opened(t)
	plan, err := s.Plan(10, []Posting{
		{Club: 1, Kind: KindWages, Amount: -300},
		{Club: 2, Kind: KindGate, Amount: 250, Fixture: 7},
		{Club: 1, Kind: KindWages, Amount: -900}, // may go negative
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Entries(); len(got) != 3 || got[0].ID != 3 || got[2].ID != 5 || got[1].At != 10 {
		t.Fatalf("planned %+v", got)
	}
	if b, _ := s.Balance(1); b != 1_000 {
		t.Fatal("planning changed a balance")
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	for _, club := range []ids.ClubID{1, 2} {
		var sum money.Money
		for _, e := range s.Entries(club) {
			sum += e.Amount
		}
		if b, ok := s.Balance(club); !ok || b != sum {
			t.Fatalf("club %d balance %d, entries sum %d", club, b, sum)
		}
	}
	if b, _ := s.Balance(1); b != -200 {
		t.Fatalf("club 1 balance %d, want -200", b)
	}
	if e, ok := s.Entry(4); !ok || e.Fixture != 7 {
		t.Fatalf("Entry(4) = %+v, %v", e, ok)
	}
	// Restoring the snapshot rebuilds the same balances.
	r, err := New(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Accounts(), s.Accounts()) || !reflect.DeepEqual(r.Snapshot(), s.Snapshot()) {
		t.Fatal("restore differs")
	}
	for _, c := range s.Accounts() {
		a, _ := s.Balance(c)
		b, _ := r.Balance(c)
		if a != b {
			t.Fatal("restored balance differs")
		}
	}
}

func TestPlanRejectsInvalidPostingsWithoutChange(t *testing.T) {
	s := opened(t)
	big, _ := s.Plan(1, []Posting{{Club: 1, Kind: KindGate, Amount: math.MaxInt64 - 1_000, Fixture: 1}})
	if err := s.Apply(big); err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	for name, ps := range map[string][]Posting{
		"unknown account":  {{Club: 9, Kind: KindWages, Amount: -1}},
		"second opening":   {{Club: 1, Kind: KindOpening, Amount: 1}},
		"positive wages":   {{Club: 1, Kind: KindWages, Amount: 1}},
		"zero wages":       {{Club: 1, Kind: KindWages}},
		"gate without fix": {{Club: 1, Kind: KindGate, Amount: 1}},
		"negative gate":    {{Club: 1, Kind: KindGate, Amount: -1, Fixture: 1}},
		"fixture on wages": {{Club: 1, Kind: KindWages, Amount: -1, Fixture: 1}},
		"unknown kind":     {{Club: 1, Kind: 9, Amount: -1}},
		"overflow":         {{Club: 1, Kind: KindGate, Amount: 1, Fixture: 2}},
		"later overflows":  {{Club: 2, Kind: KindWages, Amount: -1}, {Club: 1, Kind: KindGate, Amount: 1, Fixture: 3}},
	} {
		if _, err := s.Plan(2, ps); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := s.Plan(0, []Posting{{Club: 1, Kind: KindWages, Amount: -1}}); err == nil {
		t.Error("posting before the last entry accepted")
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("rejected plans changed the store")
	}
}

func TestStalePlanIsRejected(t *testing.T) {
	s := opened(t)
	a, _ := s.Plan(1, []Posting{{Club: 1, Kind: KindWages, Amount: -1}})
	b, _ := s.Plan(1, []Posting{{Club: 2, Kind: KindWages, Amount: -1}})
	if err := s.Apply(a); err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	for _, p := range []Plan{a, b} {
		if err := s.Apply(p); !errors.Is(err, ErrStalePlan) {
			t.Fatalf("err = %v", err)
		}
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("stale plans changed the store")
	}
}

func TestNewRejectsInvalidLedgers(t *testing.T) {
	good := opened(t).Snapshot()
	for name, mutate := range map[string]func(*Snapshot){
		"allocator low":    func(s *Snapshot) { s.LastEntry = 1 },
		"zero ID":          func(s *Snapshot) { s.Entries[0].ID = 0 },
		"out of order":     func(s *Snapshot) { s.Entries[0].ID, s.Entries[1].ID = 2, 1 },
		"no opening":       func(s *Snapshot) { s.Entries[1].Kind = KindWages; s.Entries[1].Amount = -5 },
		"time goes back":   func(s *Snapshot) { s.Entries[0].At = 5 },
		"negative opening": func(s *Snapshot) { s.Entries[1].Amount = -1 },
	} {
		s := Snapshot{Entries: append([]Entry(nil), good.Entries...), LastEntry: good.LastEntry}
		mutate(&s)
		if _, err := New(s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
