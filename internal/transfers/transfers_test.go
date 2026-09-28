package transfers

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

var terms = Terms{Years: 2, WeeklyWage: 1_000}

func bid(player uint64, seller, buyer uint64) Bid {
	return Bid{Player: ids.PlayerID(player), Seller: ids.ClubID(seller), Buyer: ids.ClubID(buyer), Fee: 50_000, Terms: terms, Deadline: 100}
}

func mustApply(t *testing.T, s *Store, at int64, c Changes) Plan {
	t.Helper()
	p, err := s.Plan(sim.GameInstant(at), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Offers are made open with sequential IDs, closed with a final status, and
// survive a snapshot.
func TestOffersOpenCloseAndRestore(t *testing.T) {
	s, err := New(Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	p := mustApply(t, s, 10, Changes{Bids: []Bid{bid(7, 1, 2), bid(8, 3, 2)}})
	made := p.Made()
	if len(made) != 2 || made[0].ID != 1 || made[1].ID != 2 || made[0].MadeAt != 10 || made[0].Status != StatusOpen {
		t.Fatalf("made %+v", made)
	}
	if len(s.Open()) != 2 || s.LastOffer() != 2 {
		t.Fatal("offers not open")
	}
	// Closing and bidding together: closures first, then new offers.
	p = mustApply(t, s, 20, Changes{
		Close: []Closure{{Offer: 1, Status: StatusCompleted}, {Offer: 2, Status: StatusRejected}},
		Bids:  []Bid{bid(9, 1, 3)},
	})
	if c := p.Closed(); len(c) != 2 || c[0].ClosedAt != 20 || c[1].Status != StatusRejected {
		t.Fatalf("closed %+v", c)
	}
	if o, _ := s.Offer(1); o.Status != StatusCompleted || o.ClosedAt != 20 {
		t.Fatalf("offer 1 %+v", o)
	}
	if open := s.Open(); len(open) != 1 || open[0].ID != 3 {
		t.Fatalf("open %+v", open)
	}
	r, err := New(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), s.Snapshot()) {
		t.Fatal("restore differs")
	}
	// The allocator continues after a restore; IDs are never reused.
	p = mustApply(t, r, 20, Changes{Bids: []Bid{bid(10, 2, 1)}})
	if p.Made()[0].ID != 4 {
		t.Fatalf("restored store issued %d", p.Made()[0].ID)
	}
}

func TestPlanRejectsInvalidChangesWithoutChange(t *testing.T) {
	s, _ := New(Snapshot{})
	mustApply(t, s, 10, Changes{Bids: []Bid{bid(7, 1, 2), bid(8, 1, 2)}})
	mustApply(t, s, 11, Changes{Close: []Closure{{Offer: 2, Status: StatusExpired}}})
	want := s.Snapshot()
	for name, c := range map[string]Changes{
		"unknown offer":    {Close: []Closure{{Offer: 9, Status: StatusRejected}}},
		"closed twice":     {Close: []Closure{{Offer: 1, Status: StatusRejected}, {Offer: 1, Status: StatusExpired}}},
		"already closed":   {Close: []Closure{{Offer: 2, Status: StatusRejected}}},
		"reopened":         {Close: []Closure{{Offer: 1, Status: StatusOpen}}},
		"unknown status":   {Close: []Closure{{Offer: 1, Status: 9}}},
		"own player":       {Bids: []Bid{bid(7, 2, 2)}},
		"no player":        {Bids: []Bid{bid(0, 1, 2)}},
		"no seller":        {Bids: []Bid{bid(7, 0, 2)}},
		"zero fee":         {Bids: []Bid{{Player: 7, Seller: 1, Buyer: 2, Terms: terms, Deadline: 100}}},
		"negative fee":     {Bids: []Bid{{Player: 7, Seller: 1, Buyer: 2, Fee: -1, Terms: terms, Deadline: 100}}},
		"no years":         {Bids: []Bid{{Player: 7, Seller: 1, Buyer: 2, Fee: 1, Terms: Terms{WeeklyWage: 1}, Deadline: 100}}},
		"no wage":          {Bids: []Bid{{Player: 7, Seller: 1, Buyer: 2, Fee: 1, Terms: Terms{Years: 1}, Deadline: 100}}},
		"deadline now":     {Bids: []Bid{{Player: 7, Seller: 1, Buyer: 2, Fee: 1, Terms: terms, Deadline: 12}}},
		"later bid is bad": {Bids: []Bid{bid(7, 1, 3), bid(7, 1, 1)}},
	} {
		if _, err := s.Plan(12, c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := s.Plan(9, Changes{Bids: []Bid{bid(7, 1, 3)}}); err == nil {
		t.Error("changes before the latest offer accepted")
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("rejected plans changed the store")
	}
}

func TestStalePlanIsRejected(t *testing.T) {
	s, _ := New(Snapshot{})
	a, _ := s.Plan(1, Changes{Bids: []Bid{bid(7, 1, 2)}})
	b, _ := s.Plan(1, Changes{Bids: []Bid{bid(8, 1, 2)}})
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

func TestNewRejectsInvalidSnapshots(t *testing.T) {
	s, _ := New(Snapshot{})
	mustApply(t, s, 10, Changes{Bids: []Bid{bid(7, 1, 2), bid(8, 3, 2)}})
	mustApply(t, s, 15, Changes{Close: []Closure{{Offer: 1, Status: StatusCompleted}}, Bids: []Bid{bid(9, 1, 3)}})
	good := s.Snapshot()
	for name, mutate := range map[string]func(*Snapshot){
		"allocator low":      func(s *Snapshot) { s.LastOffer = 2 },
		"zero ID":            func(s *Snapshot) { s.Offers[0].ID = 0 },
		"out of order":       func(s *Snapshot) { s.Offers[0].ID, s.Offers[1].ID = 2, 1 },
		"made back in time":  func(s *Snapshot) { s.Offers[2].MadeAt = 5; s.Offers[2].Deadline = 6 },
		"open with a close":  func(s *Snapshot) { s.Offers[1].ClosedAt = 12 },
		"closed before made": func(s *Snapshot) { s.Offers[0].ClosedAt = 9 },
		"unknown status":     func(s *Snapshot) { s.Offers[0].Status = 0 },
		"negative fee":       func(s *Snapshot) { s.Offers[1].Fee = money.Money(-5) },
		"self transfer":      func(s *Snapshot) { s.Offers[1].Buyer = s.Offers[1].Seller },
		"deadline passed":    func(s *Snapshot) { s.Offers[1].Deadline = s.Offers[1].MadeAt },
	} {
		snap := Snapshot{Offers: append([]Offer(nil), good.Offers...), LastOffer: good.LastOffer}
		mutate(&snap)
		if _, err := New(snap); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The store holds its own copy.
	snap := s.Snapshot()
	snap.Offers[0].Fee = 1
	if o, _ := s.Offer(1); o.Fee != 50_000 {
		t.Fatal("snapshot aliases the store")
	}
}
