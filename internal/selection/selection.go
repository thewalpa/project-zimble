// Package selection owns managers' submitted match selections: the starting
// eleven, bench and tactics a team will field in one fixture.
//
// It validates each lineup's own shape. Whether the players belong to the
// team's squad, the bench fits the competition's rules and the fixture may
// still take a lineup is checked by the application, which can read the
// owning modules. Lineups use the match contract's roles and tactics, so the
// application can hand them to an engine without translation.
package selection

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// Slot is one starting position: the player and the role they play.
type Slot struct {
	Player ids.PlayerID
	Role   matches.Role
}

// Lineup is a team's selection for one match. Bench players play in their
// natural role when they come on, so only their IDs are chosen.
type Lineup struct {
	Starters []Slot // exactly matches.StartersPerTeam, in slot order
	Bench    []ids.PlayerID
	Tactics  matches.Tactics
}

// Validate checks the lineup's shape: eleven starters with valid roles and
// exactly one goalkeeper, non-zero player IDs used once across starters and
// bench, and a valid mentality. A player may start out of position.
func (l Lineup) Validate() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("selection: invalid lineup: "+format, args...)
	}
	if len(l.Starters) != matches.StartersPerTeam {
		return fail("%d starters, want %d", len(l.Starters), matches.StartersPerTeam)
	}
	if !l.Tactics.Mentality.Valid() {
		return fail("mentality %d", l.Tactics.Mentality)
	}
	seen := make(map[ids.PlayerID]bool, len(l.Starters)+len(l.Bench))
	use := func(p ids.PlayerID) error {
		if !p.Valid() || seen[p] {
			return fail("player ID %d invalid or selected twice", p)
		}
		seen[p] = true
		return nil
	}
	keepers := 0
	for _, s := range l.Starters {
		if err := use(s.Player); err != nil {
			return err
		}
		if !s.Role.Valid() {
			return fail("player %d has role %d", s.Player, s.Role)
		}
		if s.Role == matches.Goalkeeper {
			keepers++
		}
	}
	if keepers != 1 {
		return fail("%d starting goalkeepers, want 1", keepers)
	}
	for _, p := range l.Bench {
		if err := use(p); err != nil {
			return err
		}
	}
	return nil
}

// Clone returns a copy that shares no memory with l.
func (l Lineup) Clone() Lineup {
	l.Starters = slices.Clone(l.Starters)
	l.Bench = slices.Clone(l.Bench)
	return l
}

// Equal reports whether two lineups select the same players, roles, order
// and tactics.
func (l Lineup) Equal(o Lineup) bool {
	return l.Tactics == o.Tactics && slices.Equal(l.Starters, o.Starters) && slices.Equal(l.Bench, o.Bench)
}

// Players returns every selected player: starters in slot order, then the
// bench.
func (l Lineup) Players() []ids.PlayerID {
	out := make([]ids.PlayerID, 0, len(l.Starters)+len(l.Bench))
	for _, s := range l.Starters {
		out = append(out, s.Player)
	}
	return append(out, l.Bench...)
}

// Entry is the lineup a team has submitted for a fixture.
type Entry struct {
	Fixture ids.FixtureID
	Team    ids.TeamID
	Lineup  Lineup
}

func (e Entry) validate() error {
	if !e.Fixture.Valid() || !e.Team.Valid() {
		return fmt.Errorf("selection: entry for fixture %d team %d has invalid IDs", e.Fixture, e.Team)
	}
	if err := e.Lineup.Validate(); err != nil {
		return fmt.Errorf("fixture %d team %d: %w", e.Fixture, e.Team, err)
	}
	return nil
}

func compareKeys(a, b Entry) int {
	return cmp.Or(cmp.Compare(a.Fixture, b.Fixture), cmp.Compare(a.Team, b.Team))
}

func cloneEntry(e Entry) Entry {
	e.Lineup = e.Lineup.Clone()
	return e
}

// Store holds at most one lineup per (fixture, team). Queries return copies.
type Store struct {
	entries []Entry // sorted by (Fixture, Team)
}

// New validates the entries and returns a store holding its own copy.
// New(Snapshot()) restores an equivalent store.
func New(entries []Entry) (*Store, error) {
	rows := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return nil, err
		}
		rows = append(rows, cloneEntry(e))
	}
	slices.SortFunc(rows, compareKeys)
	for i := 1; i < len(rows); i++ {
		if compareKeys(rows[i-1], rows[i]) == 0 {
			return nil, fmt.Errorf("selection: two lineups for fixture %d team %d", rows[i].Fixture, rows[i].Team)
		}
	}
	return &Store{entries: rows}, nil
}

// Submit validates e and stores a copy, replacing any earlier lineup for the
// same fixture and team. On error the store is unchanged.
func (s *Store) Submit(e Entry) error {
	if err := e.validate(); err != nil {
		return err
	}
	i, found := slices.BinarySearchFunc(s.entries, e, compareKeys)
	if found {
		s.entries[i] = cloneEntry(e)
	} else {
		s.entries = slices.Insert(s.entries, i, cloneEntry(e))
	}
	return nil
}

// Lineup returns a copy of the lineup team submitted for fixture.
func (s *Store) Lineup(fixture ids.FixtureID, team ids.TeamID) (Lineup, bool) {
	i, found := slices.BinarySearchFunc(s.entries, Entry{Fixture: fixture, Team: team}, compareKeys)
	if !found {
		return Lineup{}, false
	}
	return s.entries[i].Lineup.Clone(), true
}

// Entries returns copies of every entry in (fixture, team) order.
func (s *Store) Entries() []Entry {
	out := make([]Entry, len(s.entries))
	for i, e := range s.entries {
		out[i] = cloneEntry(e)
	}
	return out
}

// Snapshot exports the store's authoritative state as a fresh copy.
func (s *Store) Snapshot() []Entry { return s.Entries() }
