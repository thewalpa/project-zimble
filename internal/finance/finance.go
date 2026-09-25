// Package finance owns each club's ledger. A club's balance is derived from
// its entries and never assigned directly; other modules change money only
// by posting entries through the application.
//
// Changes are two-step so an application workflow can commit several
// modules atomically: Plan validates postings (known accounts, no overflow)
// and allocates entry IDs without touching the store; Apply commits a plan
// made from the store's current state and cannot fail otherwise.
package finance

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// EntryID identifies a ledger entry; allocated sequentially from 1.
type EntryID uint64

// Kind is why money moved. Values are durable; never reorder.
type Kind uint8

const (
	KindOpening Kind = 1 // opening balance; the first entry of every account
	KindWages   Kind = 2 // weekly wage bill (negative)
	KindGate    Kind = 3 // home league match receipts (positive); Fixture is set
)

func (k Kind) Valid() bool { return k >= KindOpening && k <= KindGate }

func (k Kind) String() string {
	switch k {
	case KindOpening:
		return "opening balance"
	case KindWages:
		return "wages"
	case KindGate:
		return "gate receipts"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Entry is one posting to a club's ledger.
type Entry struct {
	ID      EntryID
	Club    ids.ClubID
	At      sim.GameInstant
	Kind    Kind
	Amount  money.Money
	Fixture ids.FixtureID // gate receipts only
}

// Posting is a requested entry; Plan assigns its ID.
type Posting struct {
	Club    ids.ClubID
	Kind    Kind
	Amount  money.Money
	Fixture ids.FixtureID
}

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("finance: plan is stale")

// Snapshot is the store's authoritative state: every entry in ID order and
// the entry ID allocator. Balances are derived.
type Snapshot struct {
	Entries   []Entry
	LastEntry EntryID
}

// Store holds the ledgers. Queries return copies.
type Store struct {
	entries    []Entry // ascending ID
	lastEntry  EntryID
	balances   map[ids.ClubID]money.Money // derived; an account exists once opened
	generation uint64
}

// New validates a snapshot and rebuilds balances:
//
//   - entry IDs ascending, non-zero, at most LastEntry; times non-decreasing;
//   - each club's first entry is its only opening entry;
//   - valid kinds and signs (wages negative, gate positive with a fixture,
//     opening non-negative), and no balance overflows.
func New(snap Snapshot) (*Store, error) {
	s := &Store{lastEntry: snap.LastEntry, balances: map[ids.ClubID]money.Money{}}
	for i, e := range snap.Entries {
		if e.ID == 0 || e.ID > snap.LastEntry || (i > 0 && (e.ID <= snap.Entries[i-1].ID || e.At < snap.Entries[i-1].At)) {
			return nil, fmt.Errorf("finance: entry %d out of order or above allocator %d", e.ID, snap.LastEntry)
		}
		if err := s.post(e); err != nil {
			return nil, err
		}
	}
	s.entries = slices.Clone(snap.Entries)
	return s, nil
}

// check validates one entry against the balances so far and returns the new
// balance of its club.
func (s *Store) check(e Entry, balances map[ids.ClubID]money.Money) (money.Money, error) {
	bad := func(format string, args ...any) (money.Money, error) {
		return 0, fmt.Errorf("finance: entry %+v: "+format, append([]any{e}, args...)...)
	}
	if !e.Club.Valid() || !e.Kind.Valid() || !e.At.Valid() {
		return bad("invalid club, kind or time")
	}
	balance, open := balances[e.Club]
	switch {
	case e.Kind == KindOpening && (open || e.Amount < 0):
		return bad("second or negative opening balance")
	case e.Kind != KindOpening && !open:
		return bad("account not opened")
	case e.Kind == KindWages && e.Amount >= 0:
		return bad("wages must be negative")
	case e.Kind == KindGate && (e.Amount <= 0 || !e.Fixture.Valid()):
		return bad("gate receipts must be positive and name a fixture")
	case e.Kind != KindGate && e.Fixture != 0:
		return bad("only gate receipts name a fixture")
	}
	return balance.Add(e.Amount)
}

func (s *Store) post(e Entry) error {
	b, err := s.check(e, s.balances)
	if err != nil {
		return err
	}
	s.balances[e.Club] = b
	return nil
}

// Plan is a validated set of entries ready to apply.
type Plan struct {
	generation uint64
	entries    []Entry
}

// Entries returns the planned entries.
func (p Plan) Entries() []Entry { return slices.Clone(p.entries) }

// Plan validates postings made at instant at, in order, and assigns their
// entry IDs. It does not change the store.
func (s *Store) Plan(at sim.GameInstant, postings []Posting) (Plan, error) {
	if n := len(s.entries); n > 0 && at < s.entries[n-1].At {
		return Plan{}, fmt.Errorf("finance: posting at %d before the last entry at %d", at, s.entries[n-1].At)
	}
	balances := make(map[ids.ClubID]money.Money, len(s.balances))
	for c, b := range s.balances {
		balances[c] = b
	}
	plan := Plan{generation: s.generation}
	next := s.lastEntry
	for _, p := range postings {
		next++
		e := Entry{ID: next, Club: p.Club, At: at, Kind: p.Kind, Amount: p.Amount, Fixture: p.Fixture}
		b, err := s.check(e, balances)
		if err != nil {
			return Plan{}, err
		}
		balances[e.Club] = b
		plan.entries = append(plan.entries, e)
	}
	return plan, nil
}

// Apply commits a plan made from the store's current state. It fails only
// with ErrStalePlan; then nothing changes.
func (s *Store) Apply(p Plan) error {
	if p.generation != s.generation {
		return fmt.Errorf("%w: made at generation %d, store at %d", ErrStalePlan, p.generation, s.generation)
	}
	for _, e := range p.entries {
		s.balances[e.Club], _ = s.balances[e.Club].Add(e.Amount) // checked by Plan
		s.entries = append(s.entries, e)
		s.lastEntry = e.ID
	}
	s.generation++
	return nil
}

// Balance returns a club's balance and whether its account exists.
func (s *Store) Balance(club ids.ClubID) (money.Money, bool) {
	b, ok := s.balances[club]
	return b, ok
}

// Accounts returns every club with an account, ascending.
func (s *Store) Accounts() []ids.ClubID {
	out := make([]ids.ClubID, 0, len(s.balances))
	for c := range s.balances {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Entries returns a club's entries, oldest first.
func (s *Store) Entries(club ids.ClubID) []Entry {
	var out []Entry
	for _, e := range s.entries {
		if e.Club == club {
			out = append(out, e)
		}
	}
	return out
}

// Entry returns an entry by ID.
func (s *Store) Entry(id EntryID) (Entry, bool) {
	i, ok := slices.BinarySearchFunc(s.entries, id, func(e Entry, id EntryID) int { return cmp.Compare(e.ID, id) })
	if !ok {
		return Entry{}, false
	}
	return s.entries[i], true
}

// All returns every entry in ID order.
func (s *Store) All() []Entry { return slices.Clone(s.entries) }

// Snapshot exports the store's authoritative state as a fresh copy.
func (s *Store) Snapshot() Snapshot {
	return Snapshot{Entries: slices.Clone(s.entries), LastEntry: s.lastEntry}
}
