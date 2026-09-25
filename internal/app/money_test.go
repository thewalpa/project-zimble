package app

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
)

const week = sim.GameInstant(sim.Week)

func balance(t *testing.T, w *World, club ids.ClubID) money.Money {
	t.Helper()
	b, ok := w.finance.Balance(club)
	if !ok {
		t.Fatalf("club %d has no account", club)
	}
	return b
}

// assertLedgersConsistent checks that every balance is the sum of its
// entries and that the world validates.
func assertLedgersConsistent(t *testing.T, w *World) {
	t.Helper()
	for _, c := range w.finance.Accounts() {
		var sum money.Money
		for _, e := range w.finance.Entries(c) {
			sum += e.Amount
		}
		if b := balance(t, w, c); b != sum {
			t.Fatalf("club %d balance %s, entries sum %s", c, b, sum)
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Every club pays its wage bill once a week from the first week on.
func TestWagesArePaidWeekly(t *testing.T) {
	w := newWorld(t, 42)
	opening := w.defs.Economy.OpeningBalance
	mustContinue(t, w, 5*week+day)
	for _, c := range w.registry.Clubs() {
		bill, _ := w.employment.WageBill(c.ID)
		entries := w.finance.Entries(c.ID)
		if len(entries) != 6 || entries[0].Kind != finance.KindOpening {
			t.Fatalf("club %d has %d entries", c.ID, len(entries))
		}
		for i, e := range entries[1:] {
			if e.Kind != finance.KindWages || e.At != sim.GameInstant(i+1)*week || e.Amount != -bill {
				t.Fatalf("club %d wage entry %+v, bill %s", c.ID, e, bill)
			}
		}
		if b := balance(t, w, c.ID); b != opening-5*bill {
			t.Fatalf("club %d balance %s", c.ID, b)
		}
	}
	ledger := 0
	for _, e := range w.Events() {
		if e.Kind == events.KindLedgerPosted {
			ledger++
			if len(e.LedgerPosted.Entries) != 8 || e.Cause.Kind != events.CauseTask {
				t.Fatalf("wage event %+v", e)
			}
		}
	}
	if ledger != 5 {
		t.Fatalf("%d wage events, want 5", ledger)
	}
	assertLedgersConsistent(t, w)
}

// Gate receipts are paid exactly once per home match, also across retries,
// and wages continue through the off-season into the next season.
func TestGateReceiptsAndSeasonRollover(t *testing.T) {
	w := newWorld(t, 42)
	resolved := playSeason(t, w)
	gate := w.defs.Economy.GatePerHomeMatch
	for _, c := range w.registry.Clubs() {
		receipts := 0
		for _, e := range w.finance.Entries(c.ID) {
			if e.Kind == finance.KindGate {
				receipts++
				if e.Amount != gate {
					t.Fatalf("gate %s", e.Amount)
				}
			}
		}
		if receipts != 7 {
			t.Fatalf("club %d had %d home receipts, want 7", c.ID, receipts)
		}
	}
	before := w.finance.Snapshot()
	for _, r := range w.Snapshot().ResolveCommands[:3] {
		if _, err := w.ResolveRounds(r.Request); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(w.finance.Snapshot(), before) {
		t.Fatal("retried commands paid again")
	}
	_ = resolved

	// Into season 2: the off-season still costs wages every week.
	endOfSeason := w.Now()
	playBatches(t, w, 2)
	var offSeason int
	for _, e := range w.finance.Entries(1) {
		if e.Kind == finance.KindWages && e.At > endOfSeason && e.At < w.competitions.Rounds(w.leagues[0].season)[0].Kickoff {
			offSeason++
		}
	}
	if offSeason < 35 {
		t.Fatalf("%d off-season wage runs", offSeason)
	}
	assertLedgersConsistent(t, w)
	roundTrip(t, w)
}

// A wage bill that cannot be computed fails the wage run without paying or
// rescheduling anything. (That midnight's recovery is a separate cohort that
// runs first and stays committed.)
func TestWageOverflowFailsTheRunAtomically(t *testing.T) {
	w := newWorld(t, 42)
	mustContinue(t, w, week-1)
	orig := w.employment
	var huge []employment.Assignment
	for _, a := range orig.Assignments() {
		a.Contract.WeeklyWage = math.MaxInt64 / 4
		huge = append(huge, a)
	}
	w.employment, _ = employment.New(huge)
	ledger, events := w.finance.Snapshot(), len(w.Events())
	if _, err := w.Continue(week); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
	var wageTask []sim.Task
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskWages {
			wageTask = append(wageTask, task)
		}
	}
	if !reflect.DeepEqual(w.finance.Snapshot(), ledger) || len(w.Events()) != events || len(wageTask) != 1 || wageTask[0].DueAt != week {
		t.Fatal("a failed wage run paid, emitted or rescheduled")
	}
	w.employment = orig
	mustContinue(t, w, week)
	if n := len(w.finance.Entries(1)); n != 2 {
		t.Fatalf("club 1 has %d entries after the retried run, want 2", n)
	}
	assertLedgersConsistent(t, w)
}

// The squad view shows each player's contract; contracts end on 1 July of
// the drawn year.
func TestContractsInTheSquad(t *testing.T) {
	w := newWorld(t, 42)
	squad, _ := w.Squad(1)
	years := map[int]bool{}
	for _, p := range squad {
		civil, _ := w.Calendar().Civil(p.Contract.Expires)
		if civil.Month != 7 || civil.Day != 1 || civil.Year < 2026 || civil.Year > 2029 || p.Contract.WeeklyWage <= 0 {
			t.Fatalf("player %d contract %+v (%v)", p.Player, p.Contract, civil)
		}
		years[civil.Year] = true
	}
	if len(years) < 3 {
		t.Fatalf("contract ends spread over only %v", years)
	}
	fin, ok := w.Finances(1)
	bill, _ := w.employment.WageBill(1)
	if !ok || fin.Balance != w.defs.Economy.OpeningBalance || fin.WeeklyWage != bill || len(fin.Entries) != 1 {
		t.Fatalf("finances %+v", fin)
	}
}

func TestRestoreRejectsInvalidFinance(t *testing.T) {
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playBatches(t, w, 3)
		readyBatch(t, w)
		return w.Snapshot()
	}
	find := func(s *WorldSnapshot, k finance.Kind) *finance.Entry {
		for i := range s.Finance.Entries {
			if s.Finance.Entries[i].Kind == k {
				return &s.Finance.Entries[i]
			}
		}
		t.Fatalf("no %s entry", k)
		return nil
	}
	appendEntry := func(s *WorldSnapshot, e finance.Entry) {
		s.Finance.LastEntry++
		e.ID = s.Finance.LastEntry
		e.At = s.Scheduler.Now
		s.Finance.Entries = append(s.Finance.Entries, e)
	}
	cases := map[string]func(*WorldSnapshot){
		"opening edited": func(s *WorldSnapshot) { s.Finance.Entries[0].Amount++ },
		"gate edited":    func(s *WorldSnapshot) { find(s, finance.KindGate).Amount++ },
		"wages repeated": func(s *WorldSnapshot) {
			w := *find(s, finance.KindWages)
			appendEntry(s, finance.Entry{Club: w.Club, Kind: finance.KindWages, Amount: w.Amount})
		},
		"gate paid twice":   func(s *WorldSnapshot) { g := *find(s, finance.KindGate); appendEntry(s, g) },
		"gate for unplayed": func(s *WorldSnapshot) { g := *find(s, finance.KindGate); g.Fixture = 40; appendEntry(s, g) },
		"gate missing": func(s *WorldSnapshot) {
			var kept []finance.Entry
			dropped := false
			for _, e := range s.Finance.Entries {
				if e.Kind == finance.KindGate && !dropped {
					dropped = true
					continue
				}
				kept = append(kept, e)
			}
			s.Finance.Entries = kept
		},
		"account for no club": func(s *WorldSnapshot) {
			appendEntry(s, finance.Entry{Club: 99, Kind: finance.KindOpening, Amount: 1})
		},
		"wage task missing": func(s *WorldSnapshot) {
			var kept []sim.Task
			for _, task := range s.Scheduler.Tasks {
				if task.Kind != taskWages {
					kept = append(kept, task)
				}
			}
			s.Scheduler.Tasks = kept
		},
		"wage task off schedule": func(s *WorldSnapshot) {
			for i := range s.Scheduler.Tasks {
				if s.Scheduler.Tasks[i].Kind == taskWages {
					s.Scheduler.Tasks[i].DueAt += day
				}
			}
		},
		"contract without wage": func(s *WorldSnapshot) { s.Employment[0].Contract.WeeklyWage = 0 },
		"ledger event edited": func(s *WorldSnapshot) {
			for i := range s.Events {
				if s.Events[i].Kind == events.KindLedgerPosted {
					s.Events[i].LedgerPosted.Entries[0].Balance++
					return
				}
			}
		},
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}
