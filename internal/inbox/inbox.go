// Package inbox is the manager's message list: a read model built only from
// committed domain events. It keeps a consumer offset (the last event ID it
// has consumed), so delivering an event twice changes nothing and a gap is
// detected rather than silently skipped. Given the same events from the
// start, it always builds the same messages.
package inbox

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// MaxMessages bounds the inbox; the oldest messages are dropped first.
const MaxMessages = 200

// Kind is a message type. Values are durable; never reorder.
type Kind uint8

const (
	KindMatchday      Kind = 1 // the team's round kicked off; a lineup may be submitted
	KindResult        Kind = 2 // the team's match result
	KindSeasonEnded   Kind = 3 // a league season finished, with its champion
	KindSeasonStarted Kind = 4 // a league season was scheduled
)

func (k Kind) Valid() bool { return k >= KindMatchday && k <= KindSeasonStarted }

// Message is one inbox entry, derived from exactly one event, which is its
// identity. Fields not used by a Kind are zero.
type Message struct {
	Event       events.ID
	At          sim.GameInstant
	Kind        Kind
	Competition ids.CompetitionID
	Season      uint16
	Round       uint8           // matchday, result
	Fixture     ids.FixtureID   // matchday, result
	Opponent    ids.TeamID      // matchday, result
	Home        bool            // matchday, result: the team plays at home
	Goals       [2]uint16       // result: for, against
	Champion    ids.TeamID      // season ended
	Position    int             // season ended: the team's final position, 0 if it did not take part
	Kickoff     sim.GameInstant // season started: first kickoff
}

// Snapshot is the inbox's persisted state.
type Snapshot struct {
	Offset   events.ID
	Messages []Message // ascending Event
}

// Inbox holds the messages for one team's manager. Team 0 means no managed
// team: only league-wide messages are kept.
type Inbox struct {
	team     ids.TeamID
	offset   events.ID
	messages []Message
}

// New restores an inbox for team from a snapshot, validating it: messages
// in ascending event order, none after the offset, valid kinds, at most
// MaxMessages, and team messages only when there is a team.
func New(team ids.TeamID, snap Snapshot) (*Inbox, error) {
	if len(snap.Messages) > MaxMessages {
		return nil, fmt.Errorf("inbox: %d messages, max %d", len(snap.Messages), MaxMessages)
	}
	for i, m := range snap.Messages {
		switch {
		case m.Event == 0 || m.Event > snap.Offset || (i > 0 && m.Event <= snap.Messages[i-1].Event):
			return nil, fmt.Errorf("inbox: message for event %d out of order or after offset %d", m.Event, snap.Offset)
		case !m.Kind.Valid() || !m.Competition.Valid() || m.Season == 0:
			return nil, fmt.Errorf("inbox: message %+v is invalid", m)
		case (m.Kind == KindMatchday || m.Kind == KindResult) && (team == 0 || !m.Fixture.Valid() || !m.Opponent.Valid()):
			return nil, fmt.Errorf("inbox: match message %+v without a team, fixture or opponent", m)
		}
	}
	return &Inbox{team: team, offset: snap.Offset, messages: slices.Clone(snap.Messages)}, nil
}

func (b *Inbox) Offset() events.ID { return b.offset }

// Messages returns every message, oldest first.
func (b *Inbox) Messages() []Message { return slices.Clone(b.messages) }

// Snapshot exports the inbox's state as a fresh copy.
func (b *Inbox) Snapshot() Snapshot { return Snapshot{Offset: b.offset, Messages: b.Messages()} }

// Apply consumes events in ID order. Events at or before the offset were
// already consumed and are skipped; the rest must continue the offset
// without gaps and be valid. It is all-or-nothing: on error nothing changes.
// It returns how many events were consumed.
func (b *Inbox) Apply(evs []events.Event) (int, error) {
	offset := b.offset
	var add []Message
	for _, e := range evs {
		if e.ID <= offset {
			continue
		}
		if e.ID != offset+1 {
			return 0, fmt.Errorf("inbox: event %d after offset %d leaves a gap", e.ID, offset)
		}
		if err := e.Validate(); err != nil {
			return 0, err
		}
		if m, ok := b.message(e); ok {
			add = append(add, m)
		}
		offset = e.ID
	}
	consumed := int(offset - b.offset)
	b.messages = append(b.messages, add...)
	if n := len(b.messages) - MaxMessages; n > 0 {
		b.messages = slices.Delete(b.messages, 0, n)
	}
	b.offset = offset
	return consumed, nil
}

// message derives the team's message for an event, if the event concerns it.
func (b *Inbox) message(e events.Event) (Message, bool) {
	m := Message{Event: e.ID, At: e.OccurredAt}
	switch e.Kind {
	case events.KindRoundStarted:
		p := e.RoundStarted
		for _, f := range p.Fixtures {
			if b.team != 0 && (f.Home == b.team || f.Away == b.team) {
				m.Kind, m.Competition, m.Season, m.Round, m.Fixture = KindMatchday, p.Competition, p.Season, p.Round, f.Fixture
				m.Home, m.Opponent = b.side(f.Home, f.Away)
				return m, true
			}
		}
	case events.KindMatchCompleted:
		p := e.MatchCompleted
		if b.team != 0 && (p.Home == b.team || p.Away == b.team) {
			m.Kind, m.Competition, m.Season, m.Round, m.Fixture = KindResult, p.Competition, p.Season, p.Round, p.Fixture
			m.Home, m.Opponent = b.side(p.Home, p.Away)
			m.Goals = [2]uint16{p.HomeGoals, p.AwayGoals}
			if !m.Home {
				m.Goals = [2]uint16{p.AwayGoals, p.HomeGoals}
			}
			return m, true
		}
	case events.KindSeasonEnded:
		p := e.SeasonEnded
		m.Kind, m.Competition, m.Season, m.Champion = KindSeasonEnded, p.Competition, p.Season, p.Ranking[0]
		if i := slices.Index(p.Ranking, b.team); b.team != 0 && i >= 0 {
			m.Position = i + 1
		}
		return m, true
	case events.KindSeasonStarted:
		p := e.SeasonStarted
		m.Kind, m.Competition, m.Season, m.Kickoff = KindSeasonStarted, p.Competition, p.Season, p.FirstKickoff
		return m, true
	}
	return Message{}, false
}

// side returns whether the team is at home, and its opponent.
func (b *Inbox) side(home, away ids.TeamID) (bool, ids.TeamID) {
	if home == b.team {
		return true, away
	}
	return false, home
}
