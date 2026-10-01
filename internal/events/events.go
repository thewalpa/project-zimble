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

// SchemaVersion versions existing payloads' meaning and required shape.
// Changing or removing an existing field requires a new version. New kinds
// and optional fields preserving existing meaning require only a storage
// schema bump; older saves are migrated or explicitly rejected there.
//
// Version 2: MatchCompleted carries Appeared and Scorers, required facts of
// every completed match.
const SchemaVersion = 2

// ID identifies an event. IDs are allocated sequentially from 1 and never
// reused; they give the global order.
type ID uint64

type Kind uint16

const (
	KindRoundStarted      Kind = 1
	KindMatchCompleted    Kind = 2
	KindLineupSubmitted   Kind = 3
	KindSeasonEnded       Kind = 4
	KindSeasonStarted     Kind = 5
	KindLedgerPosted      Kind = 6
	KindContractRenewed   Kind = 7
	KindContractExpired   Kind = 8
	KindPlayerSigned      Kind = 9
	KindPlayerRetired     Kind = 10
	KindYouthJoined       Kind = 11
	KindPlayersDeveloped  Kind = 12
	KindTransferOffered   Kind = 13
	KindTransferCompleted Kind = 14
	KindOfferClosed       Kind = 15
	KindPlayerReleased    Kind = 16
	KindPlayerListed      Kind = 17
	KindPlayerUnlisted    Kind = 18
	KindInboxRead         Kind = 19
	KindPlayerInjured     Kind = 20
	KindPlayerRecovered   Kind = 21
	KindTeamPlanSaved     Kind = 22
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
	case KindContractRenewed:
		return "contract renewed"
	case KindContractExpired:
		return "contract expired"
	case KindPlayerSigned:
		return "player signed"
	case KindPlayerRetired:
		return "player retired"
	case KindYouthJoined:
		return "youth joined"
	case KindPlayersDeveloped:
		return "players developed"
	case KindTransferOffered:
		return "transfer offered"
	case KindTransferCompleted:
		return "transfer completed"
	case KindOfferClosed:
		return "offer closed"
	case KindPlayerReleased:
		return "player released"
	case KindPlayerListed:
		return "player listed"
	case KindPlayerUnlisted:
		return "player unlisted"
	case KindInboxRead:
		return "inbox read"
	case KindPlayerInjured:
		return "player injured"
	case KindPlayerRecovered:
		return "player recovered"
	case KindTeamPlanSaved:
		return "team plan saved"
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
	// A knockout match level after regulation: the penalty shootout.
	HomePenalties uint16 `json:",omitempty"`
	AwayPenalties uint16 `json:",omitempty"`
	// Appeared are the players of both sides who were on the pitch:
	// starters and substitutes who came on (not unused substitutes), in
	// ascending ID order.
	Appeared []ids.PlayerID
	// Scorers name the scorer of each goal in HomeGoals and AwayGoals, in
	// match order, both sides mixed; shootout kicks are not goals. Every
	// scorer appeared.
	Scorers []ids.PlayerID `json:",omitempty"`
}

// LineupSubmitted: a team's manager submitted a lineup for a fixture.
type LineupSubmitted struct {
	Fixture ids.FixtureID
	Team    ids.TeamID
}

// SeasonEnded: a league season, cup edition or promotion play-off finished.
// Ranking is its final order, best first: a league's table; a cup's by the
// round each team reached (the champion, the runner-up, the semi-final
// losers, and so on); a play-off's tie winners first, in tie order. Champion
// is the top of that ranking, or 0 when the season has none: every play-off
// tie stands alone. A season with a champion ranks its entrants as placings;
// a play-off's ranking is not a placings list.
type SeasonEnded struct {
	Competition ids.CompetitionID
	Season      uint16
	Ranking     []ids.TeamID
	Champion    ids.TeamID `json:",omitempty"`
}

// SeasonStarted: a league season or cup edition was created and scheduled.
type SeasonStarted struct {
	Competition  ids.CompetitionID
	Season       uint16
	FirstKickoff sim.GameInstant
	Entrants     []ids.TeamID // a league's ascending, a cup's in bracket order
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
	Offer   ids.OfferID  `json:",omitempty"`
	Player  ids.PlayerID `json:",omitempty"`
}

// LedgerPosted: one commit posted entries to club ledgers (weekly wages,
// gate receipts, transfer fees, contract payoffs), in entry order.
type LedgerPosted struct {
	Entries []LedgerEntry
}

// ContractRenewed: a player's contract with their club was replaced by a
// new one, which ends at Expires (exclusive).
type ContractRenewed struct {
	Player     ids.PlayerID
	Club       ids.ClubID
	Team       ids.TeamID
	Expires    sim.GameInstant
	WeeklyWage money.Money
}

// ContractExpired: a player's contract ended without renewal; they left the
// club (and Team) and became a free agent.
type ContractExpired struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
}

// PlayerSigned: a club signed a free agent to its Team on a contract ending
// at Expires (exclusive).
type PlayerSigned struct {
	Player     ids.PlayerID
	Club       ids.ClubID
	Team       ids.TeamID
	Expires    sim.GameInstant
	WeeklyWage money.Money
}

// PlayerRetired: a player retired at Age (whole years). Club and Team are
// the employer they left; both zero for a free agent.
type PlayerRetired struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
	Age    uint8
}

// YouthJoined: a new player joined a club's Team from its youth ranks, on a
// contract ending at Expires (exclusive).
type YouthJoined struct {
	Player     ids.PlayerID
	Club       ids.ClubID
	Team       ids.TeamID
	Expires    sim.GameInstant
	WeeklyWage money.Money
}

// Development is one player's overall (1..100) before and after a yearly
// development. Team is their team then, zero for a free agent.
type Development struct {
	Player ids.PlayerID
	Team   ids.TeamID
	Before uint8
	After  uint8
}

// PlayersDeveloped: the yearly development changed every active player's
// attributes. Players is ascending by player.
type PlayersDeveloped struct {
	Players []Development
}

// Deal names the parties of a transfer offer: the player, the selling club
// and its Team, the buying club and its Team, and the fee.
type Deal struct {
	Offer      ids.OfferID
	Player     ids.PlayerID
	Seller     ids.ClubID
	SellerTeam ids.TeamID
	Buyer      ids.ClubID
	BuyerTeam  ids.TeamID
	Fee        money.Money
}

func (d Deal) valid() bool {
	return d.Offer.Valid() && d.Player.Valid() && d.Seller.Valid() && d.SellerTeam.Valid() && d.Buyer.Valid() &&
		d.BuyerTeam.Valid() && d.Seller != d.Buyer && d.SellerTeam != d.BuyerTeam && d.Fee > 0
}

// TransferOffered: a club bid a fixed fee for another club's player. The
// selling club answers before Deadline (exclusive).
type TransferOffered struct {
	Deal
	Deadline sim.GameInstant
}

// TransferCompleted: an accepted offer completed. The player left the seller,
// joined the buyer on a contract ending at Expires (exclusive) at
// WeeklyWage, and the fee moved from the buyer's ledger to the seller's.
type TransferCompleted struct {
	Deal
	Expires    sim.GameInstant
	WeeklyWage money.Money
}

// OfferClosed: an offer closed without a transfer. Outcome is the transfers
// module's durable status: 3 rejected, 4 expired, 5 collapsed, 6 refused
// by the player.
type OfferClosed struct {
	Deal
	Outcome uint8
}

// PlayerReleased: a club released a player from his contract. He left the
// club (and Team) and became a free agent; the club paid Compensation, the
// rest of his contract (zero when no wage was still due).
type PlayerReleased struct {
	Player       ids.PlayerID
	Club         ids.ClubID
	Team         ids.TeamID
	Compensation money.Money
}

// PlayerListed: a club put a player of its Team on the transfer list, or
// changed his asking price: it offers him for sale at Asking until the
// transfer window closes. The listing ends without an event of its own when
// the player leaves the club or the window closes.
type PlayerListed struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
	Asking money.Money
}

// PlayerUnlisted: a club took a player of its Team off the transfer list.
type PlayerUnlisted struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
}

// PlayerInjured: a player of a club's Team was hurt in a match and is out for
// Days recovery days (the daily recovery that takes the last one makes him
// fit).
type PlayerInjured struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
	Days   uint16
}

// PlayerRecovered: an injured player is fit again. Club and Team are the
// employer he has now; both zero for a free agent.
type PlayerRecovered struct {
	Player ids.PlayerID
	Club   ids.ClubID
	Team   ids.TeamID
}

// TeamPlanSaved: a team's manager saved its team plan, the lineup it
// prefers when none is submitted for a fixture.
type TeamPlanSaved struct {
	Team ids.TeamID
}

// InboxRead: the manager marked the inbox message identified by its source
// event as read. It does not create another inbox message.
type InboxRead struct {
	Message ID
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

	RoundStarted      *RoundStarted      `json:",omitempty"`
	MatchCompleted    *MatchCompleted    `json:",omitempty"`
	LineupSubmitted   *LineupSubmitted   `json:",omitempty"`
	SeasonEnded       *SeasonEnded       `json:",omitempty"`
	SeasonStarted     *SeasonStarted     `json:",omitempty"`
	LedgerPosted      *LedgerPosted      `json:",omitempty"`
	ContractRenewed   *ContractRenewed   `json:",omitempty"`
	ContractExpired   *ContractExpired   `json:",omitempty"`
	PlayerSigned      *PlayerSigned      `json:",omitempty"`
	PlayerRetired     *PlayerRetired     `json:",omitempty"`
	YouthJoined       *YouthJoined       `json:",omitempty"`
	PlayersDeveloped  *PlayersDeveloped  `json:",omitempty"`
	TransferOffered   *TransferOffered   `json:",omitempty"`
	TransferCompleted *TransferCompleted `json:",omitempty"`
	OfferClosed       *OfferClosed       `json:",omitempty"`
	PlayerReleased    *PlayerReleased    `json:",omitempty"`
	PlayerListed      *PlayerListed      `json:",omitempty"`
	PlayerUnlisted    *PlayerUnlisted    `json:",omitempty"`
	InboxRead         *InboxRead         `json:",omitempty"`
	PlayerInjured     *PlayerInjured     `json:",omitempty"`
	PlayerRecovered   *PlayerRecovered   `json:",omitempty"`
	TeamPlanSaved     *TeamPlanSaved     `json:",omitempty"`
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
		{KindContractRenewed, e.ContractRenewed != nil},
		{KindContractExpired, e.ContractExpired != nil},
		{KindPlayerSigned, e.PlayerSigned != nil},
		{KindPlayerRetired, e.PlayerRetired != nil},
		{KindYouthJoined, e.YouthJoined != nil},
		{KindPlayersDeveloped, e.PlayersDeveloped != nil},
		{KindTransferOffered, e.TransferOffered != nil},
		{KindTransferCompleted, e.TransferCompleted != nil},
		{KindOfferClosed, e.OfferClosed != nil},
		{KindPlayerReleased, e.PlayerReleased != nil},
		{KindPlayerListed, e.PlayerListed != nil},
		{KindPlayerUnlisted, e.PlayerUnlisted != nil},
		{KindInboxRead, e.InboxRead != nil},
		{KindPlayerInjured, e.PlayerInjured != nil},
		{KindPlayerRecovered, e.PlayerRecovered != nil},
		{KindTeamPlanSaved, e.TeamPlanSaved != nil},
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
	case KindInboxRead:
		if p := e.InboxRead; p.Message == 0 || p.Message >= e.ID || e.Cause.Kind != CauseCommand {
			return fail("invalid payload %+v or non-command cause", p)
		}
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
		if len(p.Appeared) == 0 {
			return fail("nobody appeared in fixture %d", p.Fixture)
		}
		for i, pl := range p.Appeared {
			if !pl.Valid() || (i > 0 && pl <= p.Appeared[i-1]) {
				return fail("appearances %v not ascending valid players", p.Appeared)
			}
		}
		if len(p.Scorers) != int(p.HomeGoals)+int(p.AwayGoals) {
			return fail("%d scorers for %d-%d", len(p.Scorers), p.HomeGoals, p.AwayGoals)
		}
		for _, pl := range p.Scorers {
			if _, ok := slices.BinarySearch(p.Appeared, pl); !ok {
				return fail("scorer %d did not appear", pl)
			}
		}
	case KindLineupSubmitted:
		if p := e.LineupSubmitted; !p.Fixture.Valid() || !p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindSeasonEnded:
		p := e.SeasonEnded
		if !p.Competition.Valid() || p.Season == 0 || len(p.Ranking) == 0 || slices.Contains(p.Ranking, 0) {
			return fail("invalid payload %+v", p)
		}
		if p.Champion != 0 && p.Champion != p.Ranking[0] {
			return fail("champion %d is not the top of the ranking", p.Champion)
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
	case KindContractRenewed:
		if p := e.ContractRenewed; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Expires <= e.OccurredAt || p.WeeklyWage <= 0 {
			return fail("invalid payload %+v", p)
		}
	case KindContractExpired:
		if p := e.ContractExpired; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerSigned:
		if p := e.PlayerSigned; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Expires <= e.OccurredAt || p.WeeklyWage <= 0 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerRetired:
		if p := e.PlayerRetired; !p.Player.Valid() || p.Club.Valid() != p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindYouthJoined:
		if p := e.YouthJoined; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Expires <= e.OccurredAt || p.WeeklyWage <= 0 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayersDeveloped:
		p := e.PlayersDeveloped
		if len(p.Players) == 0 {
			return fail("no developed players")
		}
		for i, d := range p.Players {
			if !d.Player.Valid() || d.Before < 1 || d.Before > 100 || d.After < 1 || d.After > 100 || (i > 0 && d.Player <= p.Players[i-1].Player) {
				return fail("invalid development %+v", d)
			}
		}
	case KindTransferOffered:
		if p := e.TransferOffered; !p.Deal.valid() || p.Deadline <= e.OccurredAt {
			return fail("invalid payload %+v", p)
		}
	case KindTransferCompleted:
		if p := e.TransferCompleted; !p.Deal.valid() || p.Expires <= e.OccurredAt || p.WeeklyWage <= 0 {
			return fail("invalid payload %+v", p)
		}
	case KindOfferClosed:
		if p := e.OfferClosed; !p.Deal.valid() || p.Outcome < 3 || p.Outcome > 6 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerReleased:
		if p := e.PlayerReleased; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Compensation < 0 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerListed:
		if p := e.PlayerListed; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Asking <= 0 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerUnlisted:
		if p := e.PlayerUnlisted; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerInjured:
		if p := e.PlayerInjured; !p.Player.Valid() || !p.Club.Valid() || !p.Team.Valid() || p.Days == 0 {
			return fail("invalid payload %+v", p)
		}
	case KindPlayerRecovered:
		if p := e.PlayerRecovered; !p.Player.Valid() || p.Club.Valid() != p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	case KindTeamPlanSaved:
		if p := e.TeamPlanSaved; !p.Team.Valid() {
			return fail("invalid payload %+v", p)
		}
	}
	return nil
}

// Clone returns a copy that shares no memory with e.
func (e Event) Clone() Event {
	if p := e.InboxRead; p != nil {
		c := *p
		e.InboxRead = &c
	}
	if p := e.RoundStarted; p != nil {
		c := *p
		c.Fixtures = slices.Clone(p.Fixtures)
		e.RoundStarted = &c
	}
	if p := e.MatchCompleted; p != nil {
		c := *p
		c.Appeared, c.Scorers = slices.Clone(p.Appeared), slices.Clone(p.Scorers)
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
	if p := e.ContractRenewed; p != nil {
		c := *p
		e.ContractRenewed = &c
	}
	if p := e.ContractExpired; p != nil {
		c := *p
		e.ContractExpired = &c
	}
	if p := e.PlayerSigned; p != nil {
		c := *p
		e.PlayerSigned = &c
	}
	if p := e.PlayerRetired; p != nil {
		c := *p
		e.PlayerRetired = &c
	}
	if p := e.YouthJoined; p != nil {
		c := *p
		e.YouthJoined = &c
	}
	if p := e.PlayersDeveloped; p != nil {
		c := *p
		c.Players = slices.Clone(p.Players)
		e.PlayersDeveloped = &c
	}
	if p := e.TransferOffered; p != nil {
		c := *p
		e.TransferOffered = &c
	}
	if p := e.TransferCompleted; p != nil {
		c := *p
		e.TransferCompleted = &c
	}
	if p := e.OfferClosed; p != nil {
		c := *p
		e.OfferClosed = &c
	}
	if p := e.PlayerReleased; p != nil {
		c := *p
		e.PlayerReleased = &c
	}
	if p := e.PlayerListed; p != nil {
		c := *p
		e.PlayerListed = &c
	}
	if p := e.PlayerUnlisted; p != nil {
		c := *p
		e.PlayerUnlisted = &c
	}
	if p := e.PlayerInjured; p != nil {
		c := *p
		e.PlayerInjured = &c
	}
	if p := e.PlayerRecovered; p != nil {
		c := *p
		e.PlayerRecovered = &c
	}
	if p := e.TeamPlanSaved; p != nil {
		c := *p
		e.TeamPlanSaved = &c
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
