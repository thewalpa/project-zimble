// Package events defines committed domain events: past-tense facts the
// application appends to its journal in the same commit as the change they
// describe. Read models such as the inbox consume them.
//
// Events are strongly typed: an Event is an envelope with exactly one
// payload, selected by Kind. The package depends only on core types, so any
// consumer can import it. Kind values are durable; never reorder them.
package events

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// SchemaVersion is the payload schema of every kind below. Changing a
// payload's meaning or shape needs a new version (and a save migration).
const SchemaVersion = 1

// ID identifies an event. IDs are allocated sequentially from 1 and never
// reused; they give the global order.
type ID uint64

type Kind uint16

const (
	KindRoundStarted    Kind = 1
	KindMatchCompleted  Kind = 2
	KindLineupSubmitted Kind = 3
	KindSeasonEnded     Kind = 4
	KindSeasonStarted   Kind = 5
	KindLedgerPosted    Kind = 6
)

func (k Kind) String() string {
	switch k {
	case KindRoundStarted:
		return "round started"
	case KindMatchCompleted:
		return "match completed"
	case KindLineupSubmitted:
		return "lineup submitted"
	case KindSeasonEnded:
		return "season ended"
	case KindSeasonStarted:
		return "season started"
	case KindLedgerPosted:
		return "ledger posted"
	}
	return fmt.Sprintf("Kind(%d)", uint16(k))
}

// CauseKind says what committed the change.
type CauseKind uint8

const (
	CauseCommand CauseKind = 1 // ID is the command ID
	CauseTask    CauseKind = 2 // ID is the scheduled task ID
)

type Cause struct {
	Kind CauseKind
	ID   uint64
}

// Pairing is one fixture of a round.
type Pairing struct {
	Fixture ids.FixtureID
	Home    ids.TeamID
	Away    ids.TeamID
}

// RoundStarted: a league round kicked off and awaits results.
type RoundStarted struct {
	Competition ids.CompetitionID
	Season      uint16
	Round       uint8
	Fixtures    []Pairing // ascending fixture ID
}

// MatchCompleted: a fixture's official result was recorded.
type MatchCompleted struct {
	Fixture     ids.FixtureID
	Competition ids.CompetitionID
	Season      uint16
	Round       uint8
	Home, Away  ids.TeamID
	HomeGoals   uint16
	AwayGoals   uint16
}

// LineupSubmitted: a team's manager submitted a lineup for a fixture.
type LineupSubmitted struct {
	Fixture ids.FixtureID
	Team    ids.TeamID
}

// SeasonEnded: a league season finished. Ranking is its final table, top
// first; Ranking[0] is the champion.
type SeasonEnded struct {
	Competition ids.CompetitionID
	Season      uint16
	Ranking     []ids.TeamID
}

// SeasonStarted: a league season was created and scheduled.
type SeasonStarted struct {
	Competition  ids.CompetitionID
	Season       uint16
	FirstKickoff sim.GameInstant
	Entrants     []ids.TeamID // ascending
}

// LedgerEntry is one posted ledger entry. Kind uses the finance module's
// durable entry kinds; Balance is the club's balance after the entry.
type LedgerEntry struct {
	Entry   uint64
	Club    ids.ClubID
	Kind    uint8
	Amount  money.Money
	Balance money.Money
	Fixture ids.FixtureID
}

// LedgerPosted: one commit posted entries to club ledgers (weekly wages,
// gate receipts), in entry order.
type LedgerPosted struct {
	Entries []LedgerEntry
}

// Event is one committed fact. Revision is the world revision that made it
// visible; Sequence orders the events of one commit from 1. Exactly the
// payload matching Kind is set.
type Event struct {
	ID            ID
	OccurredAt    sim.GameInstant
	Revision      uint64
	Sequence      uint32
	Cause         Cause
	Kind          Kind
	SchemaVersion uint16

	RoundStarted    *RoundStarted    `json:",omitempty"`
	MatchCompleted  *MatchCompleted  `json:",omitempty"`
	LineupSubmitted *LineupSubmitted `json:",omitempty"`
	SeasonEnded     *SeasonEnded     `json:",omitempty"`
	SeasonStarted   *SeasonStarted   `json:",omitempty"`
	LedgerPosted    *LedgerPosted    `json:",omitempty"`
}

// payloads returns how many payloads are set and whether the one matching
// Kind is among them.
func (e Event) payloads() (set int, match bool) {
	for _, p := range []struct {
		kind Kind
		set  bool
	}{
		{KindRoundStarted, e.RoundStarted != nil},
		{KindMatchCompleted, e.MatchCompleted != nil},
		{KindLineupSubmitted, e.LineupSubmitted != nil},
		{KindSeasonEnded, e.SeasonEnded != nil},
		{KindSeasonStarted, e.SeasonStarted != nil},
		{KindLedgerPosted, e.LedgerPosted != nil},
	} {
		if p.set {
			set++
			match = match || p.kind == e.Kind
		}
	}
	return set, match
}

// Validate checks the envelope and that exactly the payload for Kind is set
// with non-zero identities. It cannot check references to world state.
func (e Event) Validate() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("events: event %d: "+format, append([]any{e.ID}, args...)...)
	}
	if e.ID == 0 || e.Revision == 0 || e.Sequence == 0 || e.SchemaVersion != SchemaVersion {
		return fail("invalid envelope %+v", e)
	}
	if (e.Cause.Kind != CauseCommand && e.Cause.Kind != CauseTask) || e.Cause.ID == 0 {
		return fail("invalid cause %+v", e.Cause)
	}
	if set, match := e.payloads(); set != 1 || !match {
		return fail("kind %s with %d payloads (matching: %v)", e.Kind, set, match)
	}
	switch e.Kind {
	case KindRoundStarted:
		p := e.RoundStarted
		if !p.Competition.Valid() || p.Season == 0 || p.Round == 0 || len(p.Fixtures) == 0 {
			return fail("invalid payload %+v", p)
		}
		for i, f := range p.Fixtures {
			if !f.Fixture.Valid() || !f.Home.Valid() || !f.Away.Valid() || f.Home == f.Away || (i > 0 && f.Fixture <= p.Fixtures[i-1].Fixture) {
				return fail("invalid pairing %+v", f)
			}
		}
	case KindMatchCompleted:
		p := e.MatchCompleted
		if !p.Fixture.Valid() || !p.Competition.Valid() || p.Season == 0 || p.Round == 0 || !p.Home.Valid() || !p.Away.Valid() || p.Home == p.Away {
			return fail("invalid payload %+v", p)
		}
	case KindLineupSubmitted:
		if p := e.LineupSubmitted; !p.Fixture.Valid() || !p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindSeasonEnded:
		if p := e.SeasonEnded; !p.Competition.Valid() || p.Season == 0 || len(p.Ranking) == 0 || slices.Contains(p.Ranking, 0) {
			return fail("invalid payload %+v", p)
		}
	case KindSeasonStarted:
		if p := e.SeasonStarted; !p.Competition.Valid() || p.Season == 0 || len(p.Entrants) == 0 || slices.Contains(p.Entrants, 0) {
			return fail("invalid payload %+v", p)
		}
	case KindLedgerPosted:
		p := e.LedgerPosted
		if len(p.Entries) == 0 {
			return fail("no ledger entries")
		}
		for i, le := range p.Entries {
			if le.Entry == 0 || !le.Club.Valid() || le.Kind == 0 || (i > 0 && le.Entry <= p.Entries[i-1].Entry) {
				return fail("invalid ledger entry %+v", le)
			}
		}
	}
	return nil
}

// Clone returns a copy that shares no memory with e.
func (e Event) Clone() Event {
	if p := e.RoundStarted; p != nil {
		c := *p
		c.Fixtures = slices.Clone(p.Fixtures)
		e.RoundStarted = &c
	}
	if p := e.MatchCompleted; p != nil {
		c := *p
		e.MatchCompleted = &c
	}
	if p := e.LineupSubmitted; p != nil {
		c := *p
		e.LineupSubmitted = &c
	}
	if p := e.SeasonEnded; p != nil {
		c := *p
		c.Ranking = slices.Clone(p.Ranking)
		e.SeasonEnded = &c
	}
	if p := e.SeasonStarted; p != nil {
		c := *p
		c.Entrants = slices.Clone(p.Entrants)
		e.SeasonStarted = &c
	}
	if p := e.LedgerPosted; p != nil {
		c := *p
		c.Entries = slices.Clone(p.Entries)
		e.LedgerPosted = &c
	}
	return e
}

// CloneAll clones every event.
func CloneAll(evs []Event) []Event {
	if evs == nil {
		return nil
	}
	out := make([]Event, len(evs))
	for i, e := range evs {
		out[i] = e.Clone()
	}
	return out
}
