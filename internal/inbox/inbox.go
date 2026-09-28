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
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// MaxMessages bounds the inbox; the oldest messages are dropped first.
const MaxMessages = 200

// Kind is a message type. Values are durable; never reorder.
type Kind uint8

const (
	KindMatchday      Kind = 1  // the team's round kicked off; a lineup may be submitted
	KindResult        Kind = 2  // the team's match result
	KindSeasonEnded   Kind = 3  // a league season finished, with its champion
	KindSeasonStarted Kind = 4  // a league season was scheduled
	KindRenewed       Kind = 5  // one of the team's players signed a new contract
	KindPlayerLeft    Kind = 6  // one of the team's players left as a free agent
	KindPlayerJoined  Kind = 7  // the team signed a free agent
	KindRetired       Kind = 8  // one of the team's players retired
	KindYouthJoined   Kind = 9  // a youth player joined the team
	KindDeveloped     Kind = 10 // the yearly development of the team's players
	KindBidReceived   Kind = 11 // another club bid for one of the team's players; answer before Deadline
	KindTransferIn    Kind = 12 // the team bought a player
	KindTransferOut   Kind = 13 // the team sold a player
	KindOfferClosed   Kind = 14 // a bid by or for the team ended without a transfer (Outcome)
	KindReleased      Kind = 15 // the team released one of its players (Compensation)
)

func (k Kind) Valid() bool { return k >= KindMatchday && k <= KindReleased }

// transfer reports whether messages of this kind are about a transfer
// offer.
func (k Kind) transfer() bool { return k >= KindBidReceived && k <= KindOfferClosed }

// competition reports whether messages of this kind belong to a league
// season; the others are about the team's players.
func (k Kind) competition() bool { return k <= KindSeasonStarted }

// aboutPlayer reports whether messages of this kind name one player.
func (k Kind) aboutPlayer() bool { return !k.competition() && k != KindDeveloped }

// Message is one inbox entry, derived from exactly one event, which is its
// identity. Read is derived from subsequent InboxRead events. Fields not
// used by a Kind are zero.
type Message struct {
	Read        bool `json:",omitempty"`
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
	Shootout    [2]uint16       // result of a knockout match level after regulation: penalties for, against
	Champion    ids.TeamID      // season ended
	Position    int             // season ended: the team's final position, 0 if it did not take part
	Kickoff     sim.GameInstant // season started: first kickoff
	Player      ids.PlayerID    // renewed, left, joined, retired, youth, transfers, released
	Expires     sim.GameInstant // renewed, joined, youth, transfer in: the contract's end
	WeeklyWage  money.Money     // renewed, joined, youth, transfer in
	Age         uint8           // retired
	Improved    int             // developed: players whose overall rose
	Declined    int             // developed: players whose overall fell
	// Transfers: the offer, the other club (the bidder, or the club the
	// team bid to), the fee, the deadline of a bid received, how a closed
	// offer ended (the transfers module's status) and whether the team was
	// selling.
	Offer    ids.OfferID     `json:",omitempty"`
	Club     ids.ClubID      `json:",omitempty"`
	Fee      money.Money     `json:",omitempty"`
	Deadline sim.GameInstant `json:",omitempty"`
	Outcome  uint8           `json:",omitempty"`
	Selling  bool            `json:",omitempty"`
	// Released: what the team paid to end the contract.
	Compensation money.Money `json:",omitempty"`
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
		case !m.Kind.Valid() || (m.Kind.competition() && (!m.Competition.Valid() || m.Season == 0)):
			return nil, fmt.Errorf("inbox: message %+v is invalid", m)
		case !m.Kind.competition() && (team == 0 || m.Kind.aboutPlayer() != m.Player.Valid()):
			return nil, fmt.Errorf("inbox: player message %+v without a team, or with the wrong player", m)
		case (m.Kind == KindMatchday || m.Kind == KindResult) && (team == 0 || !m.Fixture.Valid() || !m.Opponent.Valid()):
			return nil, fmt.Errorf("inbox: match message %+v without a team, fixture or opponent", m)
		case m.Kind.transfer() && (!m.Offer.Valid() || !m.Club.Valid() || m.Fee <= 0):
			return nil, fmt.Errorf("inbox: transfer message %+v without an offer, club or fee", m)
		case m.Compensation < 0 || (m.Compensation != 0 && m.Kind != KindReleased):
			return nil, fmt.Errorf("inbox: message %+v with a compensation", m)
		}
	}
	return &Inbox{team: team, offset: snap.Offset, messages: slices.Clone(snap.Messages)}, nil
}

func (b *Inbox) Offset() events.ID { return b.offset }

// Messages returns every message, oldest first.
func (b *Inbox) Messages() []Message { return slices.Clone(b.messages) }

// UnreadCount counts unread messages in the retained inbox.
func (b *Inbox) UnreadCount() int {
	n := 0
	for _, m := range b.messages {
		if !m.Read {
			n++
		}
	}
	return n
}

// Snapshot exports the inbox's state as a fresh copy.
func (b *Inbox) Snapshot() Snapshot { return Snapshot{Offset: b.offset, Messages: b.Messages()} }

// Apply consumes events in ID order. Events at or before the offset were
// already consumed and are skipped; the rest must continue the offset
// without gaps and be valid. It is all-or-nothing: on error nothing changes.
// It returns how many events were consumed.
func (b *Inbox) Apply(evs []events.Event) (int, error) {
	offset := b.offset
	messages := slices.Clone(b.messages)
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
		if e.Kind == events.KindInboxRead {
			i := slices.IndexFunc(messages, func(m Message) bool { return m.Event == e.InboxRead.Message })
			if i < 0 {
				return 0, fmt.Errorf("inbox: read event %d names missing message %d", e.ID, e.InboxRead.Message)
			}
			messages[i].Read = true
		} else if m, ok := b.message(e); ok {
			messages = append(messages, m)
			if n := len(messages) - MaxMessages; n > 0 {
				messages = slices.Delete(messages, 0, n)
			}
		}
		offset = e.ID
	}
	consumed := int(offset - b.offset)
	b.messages = messages
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
			m.Goals, m.Shootout = [2]uint16{p.HomeGoals, p.AwayGoals}, [2]uint16{p.HomePenalties, p.AwayPenalties}
			if !m.Home {
				m.Goals, m.Shootout = [2]uint16{p.AwayGoals, p.HomeGoals}, [2]uint16{p.AwayPenalties, p.HomePenalties}
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
	case events.KindContractRenewed:
		if p := e.ContractRenewed; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player, m.Expires, m.WeeklyWage = KindRenewed, p.Player, p.Expires, p.WeeklyWage
			return m, true
		}
	case events.KindContractExpired:
		if p := e.ContractExpired; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player = KindPlayerLeft, p.Player
			return m, true
		}
	case events.KindPlayerSigned:
		if p := e.PlayerSigned; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player, m.Expires, m.WeeklyWage = KindPlayerJoined, p.Player, p.Expires, p.WeeklyWage
			return m, true
		}
	case events.KindPlayerRetired:
		if p := e.PlayerRetired; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player, m.Age = KindRetired, p.Player, p.Age
			return m, true
		}
	case events.KindYouthJoined:
		if p := e.YouthJoined; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player, m.Expires, m.WeeklyWage = KindYouthJoined, p.Player, p.Expires, p.WeeklyWage
			return m, true
		}
	case events.KindPlayersDeveloped:
		m.Kind = KindDeveloped
		found := false
		for _, d := range e.PlayersDeveloped.Players {
			if b.team == 0 || d.Team != b.team {
				continue
			}
			found = true
			switch {
			case d.After > d.Before:
				m.Improved++
			case d.After < d.Before:
				m.Declined++
			}
		}
		return m, found
	case events.KindTransferOffered:
		// Only bids for the team's players: the team knows its own bids.
		if p := e.TransferOffered; b.team != 0 && p.SellerTeam == b.team {
			m.Kind, m.Deadline, m.Selling = KindBidReceived, p.Deadline, true
			return b.deal(m, p.Deal), true
		}
	case events.KindTransferCompleted:
		p := e.TransferCompleted
		switch {
		case b.team == 0:
		case p.BuyerTeam == b.team:
			m.Kind, m.Expires, m.WeeklyWage = KindTransferIn, p.Expires, p.WeeklyWage
			return b.deal(m, p.Deal), true
		case p.SellerTeam == b.team:
			m.Kind, m.Selling = KindTransferOut, true
			return b.deal(m, p.Deal), true
		}
	case events.KindOfferClosed:
		if p := e.OfferClosed; b.team != 0 && (p.SellerTeam == b.team || p.BuyerTeam == b.team) {
			m.Kind, m.Outcome, m.Selling = KindOfferClosed, p.Outcome, p.SellerTeam == b.team
			return b.deal(m, p.Deal), true
		}
	case events.KindPlayerReleased:
		if p := e.PlayerReleased; b.team != 0 && p.Team == b.team {
			m.Kind, m.Player, m.Compensation = KindReleased, p.Player, p.Compensation
			return m, true
		}
	}
	return Message{}, false
}

// deal fills a transfer message's player, offer, fee and other club.
func (b *Inbox) deal(m Message, d events.Deal) Message {
	m.Player, m.Offer, m.Fee, m.Club = d.Player, d.Offer, d.Fee, d.Buyer
	if d.BuyerTeam == b.team {
		m.Club = d.Seller
	}
	return m
}

// side returns whether the team is at home, and its opponent.
func (b *Inbox) side(home, away ids.TeamID) (bool, ids.TeamID) {
	if home == b.team {
		return true, away
	}
	return false, home
}
