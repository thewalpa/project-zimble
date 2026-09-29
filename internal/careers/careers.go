// Package careers is the history of every player's clubs: a read model
// built only from committed domain events, starting from the employment a
// career begins with. Employment owns where a player plays now; this package
// remembers where he played before, which no module keeps once a contract
// ends and the journal is trimmed.
//
// Like the inbox it keeps a consumer offset (the last event ID it has
// consumed), so delivering an event twice changes nothing and a gap is
// detected rather than silently skipped. Given the same start and the same
// events, it always builds the same careers.
package careers

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// Joined says how a spell began. Values are durable; never reorder.
type Joined uint8

const (
	JoinedAtStart  Joined = 1 // on the club's books when the career began; From is the career's start
	JoinedYouth    Joined = 2 // came through the club's youth ranks
	JoinedFree     Joined = 3 // signed as a free agent
	JoinedTransfer Joined = 4 // bought from the club of the previous spell for Fee
)

func (j Joined) Valid() bool { return j >= JoinedAtStart && j <= JoinedTransfer }

// Left says how a spell ended. Values are durable; never reorder.
type Left uint8

const (
	LeftNot      Left = 0 // the spell is current
	LeftTransfer Left = 1 // sold to the club of the next spell
	LeftExpired  Left = 2 // the contract ran out
	LeftReleased Left = 3 // the club released him
	LeftRetired  Left = 4 // he retired
)

func (l Left) Valid() bool { return l <= LeftRetired }

// Spell is one stay at a club, from From until Until (both game instants).
// A current spell has Left == LeftNot and no Until.
type Spell struct {
	Club   ids.ClubID
	From   sim.GameInstant
	Joined Joined
	Fee    money.Money     `json:",omitempty"` // JoinedTransfer: what Club paid
	Until  sim.GameInstant `json:",omitempty"`
	Left   Left            `json:",omitempty"`
}

func (s Spell) Current() bool { return s.Left == LeftNot }

// Career is one player's spells, oldest first. Only the last may be current.
type Career struct {
	Player ids.PlayerID
	Spells []Spell
}

// Employed is a player's club when a career begins.
type Employed struct {
	Player ids.PlayerID
	Club   ids.ClubID
}

// Snapshot is the store's persisted state.
type Snapshot struct {
	Offset  events.ID
	Careers []Career // ascending Player
}

// Store holds every player's career. A player who has never had a club has
// no career.
type Store struct {
	offset  events.ID
	careers map[ids.PlayerID][]Spell
}

// Start returns the careers of a world that begins at `at` with the given
// employment, before any event: one current JoinedAtStart spell each.
func Start(at sim.GameInstant, employed []Employed) (*Store, error) {
	var snap Snapshot
	for _, e := range employed {
		snap.Careers = append(snap.Careers, Career{Player: e.Player, Spells: []Spell{{Club: e.Club, From: at, Joined: JoinedAtStart}}})
	}
	slices.SortFunc(snap.Careers, func(a, b Career) int { return cmp.Compare(a.Player, b.Player) })
	return New(snap)
}

// New restores a store from a snapshot, validating each career: ascending
// unique players, and spells that follow one another in time, with only the
// last current. A career begins with its only JoinedAtStart or JoinedYouth
// spell, if it has one, and ends with the only LeftRetired one, if any. A JoinedTransfer spell carries a fee and starts at
// another club at the instant the previous spell ended by transfer, and
// every LeftTransfer spell is followed by one.
func New(snap Snapshot) (*Store, error) {
	s := &Store{offset: snap.Offset, careers: make(map[ids.PlayerID][]Spell, len(snap.Careers))}
	for i, c := range snap.Careers {
		if !c.Player.Valid() || (i > 0 && c.Player <= snap.Careers[i-1].Player) {
			return nil, fmt.Errorf("careers: player %d invalid or out of order", c.Player)
		}
		if err := checkSpells(c.Spells); err != nil {
			return nil, fmt.Errorf("careers: player %d: %w", c.Player, err)
		}
		s.careers[c.Player] = slices.Clone(c.Spells)
	}
	return s, nil
}

func checkSpells(spells []Spell) error {
	if len(spells) == 0 {
		return fmt.Errorf("no spells")
	}
	for i, sp := range spells {
		var prev Spell
		if i > 0 {
			prev = spells[i-1]
		}
		last := i == len(spells)-1
		switch {
		case !sp.Club.Valid() || !sp.Joined.Valid() || !sp.Left.Valid() || !sp.From.Valid():
			return fmt.Errorf("invalid spell %+v", sp)
		case sp.Current() && (!last || sp.Until != 0):
			return fmt.Errorf("current spell %+v is not the last or has an end", sp)
		case !sp.Current() && sp.Until < sp.From:
			return fmt.Errorf("spell %+v ends before it starts", sp)
		case (sp.Joined == JoinedAtStart || sp.Joined == JoinedYouth) && i > 0:
			return fmt.Errorf("spell %+v is not the first", sp)
		case (sp.Joined == JoinedTransfer) != (sp.Fee > 0) || sp.Fee < 0:
			return fmt.Errorf("spell %+v has the wrong fee", sp)
		case i > 0 && sp.From < prev.Until:
			return fmt.Errorf("spell %+v starts before the previous one ends", sp)
		case sp.Joined == JoinedTransfer && (i == 0 || prev.Left != LeftTransfer || prev.Until != sp.From || prev.Club == sp.Club):
			return fmt.Errorf("transfer spell %+v does not follow a sale by another club", sp)
		case sp.Left == LeftTransfer && (last || spells[i+1].Joined != JoinedTransfer):
			return fmt.Errorf("spell %+v ends in a sale without a buyer", sp)
		case sp.Left == LeftRetired && !last:
			return fmt.Errorf("spell %+v ends in retirement but is not the last", sp)
		}
	}
	return nil
}

func (s *Store) Offset() events.ID { return s.offset }

// Career returns a player's spells, oldest first, or false if he has never
// had a club.
func (s *Store) Career(player ids.PlayerID) ([]Spell, bool) {
	spells, ok := s.careers[player]
	return slices.Clone(spells), ok
}

// Careers returns every career in ascending player order.
func (s *Store) Careers() []Career {
	out := make([]Career, 0, len(s.careers))
	for _, p := range slices.Sorted(maps.Keys(s.careers)) {
		out = append(out, Career{Player: p, Spells: slices.Clone(s.careers[p])})
	}
	return out
}

// Snapshot exports the store's state as a fresh copy.
func (s *Store) Snapshot() Snapshot { return Snapshot{Offset: s.offset, Careers: s.Careers()} }

// Initial returns the snapshot the store started from before its first
// event: each JoinedAtStart spell, still current. Replaying every event
// since the start onto it rebuilds the store.
func (s *Store) Initial() Snapshot {
	var snap Snapshot
	for _, c := range s.Careers() {
		if first := c.Spells[0]; first.Joined == JoinedAtStart {
			snap.Careers = append(snap.Careers, Career{Player: c.Player, Spells: []Spell{{Club: first.Club, From: first.From, Joined: JoinedAtStart}}})
		}
	}
	return snap
}

// Apply consumes events in ID order. Events at or before the offset were
// already consumed and are skipped; the rest must continue the offset
// without gaps and be valid. Each employment event must agree with the
// careers: a club can only lose a player whose current spell is with it, and
// only a player without a current spell (who has not retired) can join a
// club. It is
// all-or-nothing: on error nothing changes. It returns how many events were
// consumed.
func (s *Store) Apply(evs []events.Event) (int, error) {
	offset := s.offset
	staged := map[ids.PlayerID][]Spell{}
	spells := func(p ids.PlayerID) ([]Spell, bool) {
		if sp, ok := staged[p]; ok {
			return sp, true
		}
		sp, ok := s.careers[p]
		return slices.Clone(sp), ok
	}
	// leave ends the player's current spell at club.
	leave := func(e events.Event, p ids.PlayerID, club ids.ClubID, how Left) error {
		sp, _ := spells(p)
		if n := len(sp); n == 0 || !sp[n-1].Current() || sp[n-1].Club != club {
			return fmt.Errorf("careers: event %d (%s): player %d is not at club %d", e.ID, e.Kind, p, club)
		}
		sp[len(sp)-1].Until, sp[len(sp)-1].Left = e.OccurredAt, how
		staged[p] = sp
		return nil
	}
	// join starts a spell at club for a player without a current spell.
	join := func(e events.Event, p ids.PlayerID, next Spell) error {
		sp, known := spells(p)
		if n := len(sp); n > 0 && sp[n-1].Current() || known && next.Joined == JoinedYouth {
			return fmt.Errorf("careers: event %d (%s): player %d already has a club", e.ID, e.Kind, p)
		}
		next.From = e.OccurredAt
		staged[p] = append(sp, next)
		return nil
	}
	for _, e := range evs {
		if e.ID <= offset {
			continue
		}
		if e.ID != offset+1 {
			return 0, fmt.Errorf("careers: event %d after offset %d leaves a gap", e.ID, offset)
		}
		if err := e.Validate(); err != nil {
			return 0, err
		}
		var err error
		switch e.Kind {
		case events.KindYouthJoined:
			p := e.YouthJoined
			err = join(e, p.Player, Spell{Club: p.Club, Joined: JoinedYouth})
		case events.KindPlayerSigned:
			p := e.PlayerSigned
			err = join(e, p.Player, Spell{Club: p.Club, Joined: JoinedFree})
		case events.KindTransferCompleted:
			p := e.TransferCompleted
			if err = leave(e, p.Player, p.Seller, LeftTransfer); err == nil {
				err = join(e, p.Player, Spell{Club: p.Buyer, Joined: JoinedTransfer, Fee: p.Fee})
			}
		case events.KindContractExpired:
			p := e.ContractExpired
			err = leave(e, p.Player, p.Club, LeftExpired)
		case events.KindPlayerReleased:
			p := e.PlayerReleased
			err = leave(e, p.Player, p.Club, LeftReleased)
		case events.KindPlayerRetired:
			p := e.PlayerRetired
			if p.Club.Valid() {
				err = leave(e, p.Player, p.Club, LeftRetired)
			} else if sp, _ := spells(p.Player); len(sp) > 0 && sp[len(sp)-1].Current() {
				err = fmt.Errorf("careers: event %d: free agent %d retired from club %d", e.ID, p.Player, sp[len(sp)-1].Club)
			}
		}
		if err != nil {
			return 0, err
		}
		offset = e.ID
	}
	for p, sp := range staged {
		if err := checkSpells(sp); err != nil {
			return 0, fmt.Errorf("careers: player %d: %w", p, err)
		}
	}
	for p, sp := range staged {
		s.careers[p] = sp
	}
	consumed := int(offset - s.offset)
	s.offset = offset
	return consumed, nil
}
