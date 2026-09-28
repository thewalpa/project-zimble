package inbox

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

func env(id events.ID, k events.Kind) events.Event {
	return events.Event{ID: id, OccurredAt: 10 * sim.GameInstant(id), Revision: uint64(id), Sequence: 1,
		Cause: events.Cause{Kind: events.CauseTask, ID: 1}, Kind: k, SchemaVersion: events.SchemaVersion}
}

// journal: team 3 plays fixture 2 (away to 4, loses 2-1 is home 2 away 1),
// fixture 1 is 1 v 2; then the season ends (3 second of 4) and restarts.
func journal() []events.Event {
	a := env(1, events.KindRoundStarted)
	a.RoundStarted = &events.RoundStarted{Competition: 1, Season: 1, Round: 1,
		Fixtures: []events.Pairing{{Fixture: 1, Home: 1, Away: 2}, {Fixture: 2, Home: 4, Away: 3}}}
	b := env(2, events.KindMatchCompleted)
	b.MatchCompleted = &events.MatchCompleted{Fixture: 1, Competition: 1, Season: 1, Round: 1, Home: 1, Away: 2, HomeGoals: 1}
	c := env(3, events.KindMatchCompleted)
	c.MatchCompleted = &events.MatchCompleted{Fixture: 2, Competition: 1, Season: 1, Round: 1, Home: 4, Away: 3, HomeGoals: 2, AwayGoals: 1}
	d := env(4, events.KindLineupSubmitted)
	d.LineupSubmitted = &events.LineupSubmitted{Fixture: 2, Team: 3}
	e := env(5, events.KindSeasonEnded)
	e.SeasonEnded = &events.SeasonEnded{Competition: 1, Season: 1, Ranking: []ids.TeamID{4, 3, 1, 2}}
	f := env(6, events.KindSeasonStarted)
	f.SeasonStarted = &events.SeasonStarted{Competition: 1, Season: 2, FirstKickoff: 500, Entrants: []ids.TeamID{1, 2, 3, 4}}
	return []events.Event{a, b, c, d, e, f}
}

func mustNew(t *testing.T, team ids.TeamID) *Inbox {
	t.Helper()
	b, err := New(team, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMessagesForTheManagedTeam(t *testing.T) {
	b := mustNew(t, 3)
	if n, err := b.Apply(journal()); err != nil || n != 6 {
		t.Fatalf("Apply = %d, %v", n, err)
	}
	want := []Message{
		{Event: 1, At: 10, Kind: KindMatchday, Competition: 1, Season: 1, Round: 1, Fixture: 2, Opponent: 4},
		{Event: 3, At: 30, Kind: KindResult, Competition: 1, Season: 1, Round: 1, Fixture: 2, Opponent: 4, Goals: [2]uint16{1, 2}},
		{Event: 5, At: 50, Kind: KindSeasonEnded, Competition: 1, Season: 1, Champion: 4, Position: 2},
		{Event: 6, At: 60, Kind: KindSeasonStarted, Competition: 1, Season: 2, Kickoff: 500},
	}
	if got := b.Messages(); !reflect.DeepEqual(got, want) || b.Offset() != 6 {
		t.Fatalf("messages %+v, offset %d", got, b.Offset())
	}
	// Without a team only league-wide messages remain.
	none := mustNew(t, 0)
	none.Apply(journal())
	if got := none.Messages(); len(got) != 2 || got[0].Kind != KindSeasonEnded || got[0].Position != 0 || got[1].Kind != KindSeasonStarted {
		t.Fatalf("unmanaged inbox %+v", got)
	}
}

// Contract events become messages only for the managed team's players.
func TestPlayerMessages(t *testing.T) {
	j := journal()
	add := func(k events.Kind, set func(*events.Event)) {
		e := env(events.ID(len(j)+1), k)
		set(&e)
		j = append(j, e)
	}
	add(events.KindContractRenewed, func(e *events.Event) {
		e.ContractRenewed = &events.ContractRenewed{Player: 7, Club: 3, Team: 3, Expires: 900, WeeklyWage: 50}
	})
	add(events.KindContractRenewed, func(e *events.Event) {
		e.ContractRenewed = &events.ContractRenewed{Player: 8, Club: 4, Team: 4, Expires: 900, WeeklyWage: 50}
	})
	add(events.KindContractExpired, func(e *events.Event) {
		e.ContractExpired = &events.ContractExpired{Player: 9, Club: 3, Team: 3}
	})
	add(events.KindPlayerSigned, func(e *events.Event) {
		e.PlayerSigned = &events.PlayerSigned{Player: 9, Club: 4, Team: 4, Expires: 900, WeeklyWage: 60}
	})
	add(events.KindPlayerSigned, func(e *events.Event) {
		e.PlayerSigned = &events.PlayerSigned{Player: 10, Club: 3, Team: 3, Expires: 800, WeeklyWage: 70}
	})
	b := mustNew(t, 3)
	if _, err := b.Apply(j); err != nil {
		t.Fatal(err)
	}
	got := b.Messages()[4:]
	want := []Message{
		{Event: 7, At: 70, Kind: KindRenewed, Player: 7, Expires: 900, WeeklyWage: 50},
		{Event: 9, At: 90, Kind: KindPlayerLeft, Player: 9},
		{Event: 11, At: 110, Kind: KindPlayerJoined, Player: 10, Expires: 800, WeeklyWage: 70},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("player messages %+v", got)
	}
	if _, err := New(3, b.Snapshot()); err != nil {
		t.Fatalf("snapshot with player messages rejected: %v", err)
	}
	snap := b.Snapshot()
	snap.Messages[4].Player = 0
	if _, err := New(3, snap); err == nil {
		t.Fatal("player message without a player accepted")
	}
	none := mustNew(t, 0)
	none.Apply(j)
	if len(none.Messages()) != 2 {
		t.Fatal("an unmanaged inbox kept player messages")
	}
}

// Retirements and youth arrivals become messages for the managed team's
// players; a development becomes one summary for the team.
func TestLifecycleMessages(t *testing.T) {
	j := journal()
	add := func(k events.Kind, set func(*events.Event)) {
		e := env(events.ID(len(j)+1), k)
		set(&e)
		j = append(j, e)
	}
	add(events.KindPlayerRetired, func(e *events.Event) {
		e.PlayerRetired = &events.PlayerRetired{Player: 7, Club: 3, Team: 3, Age: 36}
	})
	add(events.KindPlayerRetired, func(e *events.Event) { e.PlayerRetired = &events.PlayerRetired{Player: 8, Age: 31} })
	add(events.KindYouthJoined, func(e *events.Event) {
		e.YouthJoined = &events.YouthJoined{Player: 161, Club: 3, Team: 3, Expires: 900, WeeklyWage: 40}
	})
	add(events.KindYouthJoined, func(e *events.Event) {
		e.YouthJoined = &events.YouthJoined{Player: 162, Club: 4, Team: 4, Expires: 900, WeeklyWage: 40}
	})
	add(events.KindPlayersDeveloped, func(e *events.Event) {
		e.PlayersDeveloped = &events.PlayersDeveloped{Players: []events.Development{
			{Player: 1, Team: 3, Before: 50, After: 53}, {Player: 2, Team: 3, Before: 60, After: 59},
			{Player: 3, Team: 3, Before: 60, After: 60}, {Player: 4, Team: 3, Before: 40, After: 44},
			{Player: 5, Team: 4, Before: 40, After: 30}, {Player: 6, Before: 40, After: 30},
		}}
	})
	add(events.KindPlayersDeveloped, func(e *events.Event) {
		e.PlayersDeveloped = &events.PlayersDeveloped{Players: []events.Development{{Player: 5, Team: 4, Before: 40, After: 30}}}
	})
	b := mustNew(t, 3)
	if _, err := b.Apply(j); err != nil {
		t.Fatal(err)
	}
	got := b.Messages()[4:]
	want := []Message{
		{Event: 7, At: 70, Kind: KindRetired, Player: 7, Age: 36},
		{Event: 9, At: 90, Kind: KindYouthJoined, Player: 161, Expires: 900, WeeklyWage: 40},
		{Event: 11, At: 110, Kind: KindDeveloped, Improved: 2, Declined: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle messages %+v", got)
	}
	if _, err := New(3, b.Snapshot()); err != nil {
		t.Fatalf("snapshot rejected: %v", err)
	}
	snap := b.Snapshot()
	snap.Messages[6].Player = 1
	if _, err := New(3, snap); err == nil {
		t.Fatal("development message naming a player accepted")
	}
	none := mustNew(t, 0)
	none.Apply(j)
	if len(none.Messages()) != 2 {
		t.Fatal("an unmanaged inbox kept lifecycle messages")
	}
}

// Transfer events become messages when the managed team sells or buys: bids
// received, completed transfers either way, and offers by or for the team
// that closed without a transfer. The team's own bids and other clubs'
// business give no message.
func TestTransferMessages(t *testing.T) {
	j := journal()
	add := func(k events.Kind, set func(*events.Event)) {
		e := env(events.ID(len(j)+1), k)
		set(&e)
		j = append(j, e)
	}
	sell := events.Deal{Offer: 1, Player: 7, Seller: 3, SellerTeam: 3, Buyer: 4, BuyerTeam: 4, Fee: 900}
	buy := events.Deal{Offer: 2, Player: 8, Seller: 1, SellerTeam: 1, Buyer: 3, BuyerTeam: 3, Fee: 700}
	other := events.Deal{Offer: 3, Player: 9, Seller: 1, SellerTeam: 1, Buyer: 2, BuyerTeam: 2, Fee: 500}
	add(events.KindTransferOffered, func(e *events.Event) { e.TransferOffered = &events.TransferOffered{Deal: sell, Deadline: 200} })
	add(events.KindTransferOffered, func(e *events.Event) { e.TransferOffered = &events.TransferOffered{Deal: buy, Deadline: 200} })
	add(events.KindTransferOffered, func(e *events.Event) { e.TransferOffered = &events.TransferOffered{Deal: other, Deadline: 200} })
	add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: buy, Expires: 900, WeeklyWage: 60}
	})
	add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: sell, Expires: 900, WeeklyWage: 60}
	})
	add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: other, Expires: 900, WeeklyWage: 60}
	})
	add(events.KindOfferClosed, func(e *events.Event) { e.OfferClosed = &events.OfferClosed{Deal: buy, Outcome: 3} })
	add(events.KindOfferClosed, func(e *events.Event) { e.OfferClosed = &events.OfferClosed{Deal: sell, Outcome: 4} })
	add(events.KindOfferClosed, func(e *events.Event) { e.OfferClosed = &events.OfferClosed{Deal: other, Outcome: 5} })
	b := mustNew(t, 3)
	if _, err := b.Apply(j); err != nil {
		t.Fatal(err)
	}
	got := b.Messages()[4:]
	want := []Message{
		{Event: 7, At: 70, Kind: KindBidReceived, Player: 7, Offer: 1, Club: 4, Fee: 900, Deadline: 200, Selling: true},
		{Event: 10, At: 100, Kind: KindTransferIn, Player: 8, Offer: 2, Club: 1, Fee: 700, Expires: 900, WeeklyWage: 60},
		{Event: 11, At: 110, Kind: KindTransferOut, Player: 7, Offer: 1, Club: 4, Fee: 900, Selling: true},
		{Event: 13, At: 130, Kind: KindOfferClosed, Player: 8, Offer: 2, Club: 1, Fee: 700, Outcome: 3},
		{Event: 14, At: 140, Kind: KindOfferClosed, Player: 7, Offer: 1, Club: 4, Fee: 900, Outcome: 4, Selling: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transfer messages %+v", got)
	}
	if _, err := New(3, b.Snapshot()); err != nil {
		t.Fatalf("snapshot rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Message){
		"no offer": func(m *Message) { m.Offer = 0 },
		"no club":  func(m *Message) { m.Club = 0 },
		"no fee":   func(m *Message) { m.Fee = 0 },
	} {
		snap := b.Snapshot()
		mutate(&snap.Messages[5])
		if _, err := New(3, snap); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	none := mustNew(t, 0)
	none.Apply(j)
	if len(none.Messages()) != 2 {
		t.Fatal("an unmanaged inbox kept transfer messages")
	}
}

// Duplicates are skipped, chunking does not matter, gaps and invalid events
// are rejected without change.
func TestApplyIsIdempotentAndRejectsGaps(t *testing.T) {
	whole := mustNew(t, 3)
	whole.Apply(journal())
	chunked := mustNew(t, 3)
	j := journal()
	for _, part := range [][]events.Event{j[:2], j[:4], j[1:5], j, j[5:]} { // overlapping, repeated
		if _, err := chunked.Apply(part); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(chunked.Snapshot(), whole.Snapshot()) {
		t.Fatal("chunked or repeated delivery differs from one delivery")
	}
	if n, _ := chunked.Apply(journal()); n != 0 {
		t.Fatalf("redelivery consumed %d events", n)
	}

	b := mustNew(t, 3)
	b.Apply(journal()[:2])
	before := b.Snapshot()
	bad := journal()
	bad[3].LineupSubmitted = nil
	for name, evs := range map[string][]events.Event{
		"gap":           journal()[3:],
		"invalid event": bad[2:4],
	} {
		if _, err := b.Apply(evs); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if !reflect.DeepEqual(b.Snapshot(), before) {
			t.Fatalf("%s: rejected delivery changed the inbox", name)
		}
	}
}

func TestInboxIsBounded(t *testing.T) {
	b := mustNew(t, 3)
	var evs []events.Event
	for i := range MaxMessages + 5 {
		e := env(events.ID(i+1), events.KindSeasonStarted)
		e.SeasonStarted = &events.SeasonStarted{Competition: 1, Season: uint16(i + 1), Entrants: []ids.TeamID{3}}
		evs = append(evs, e)
	}
	b.Apply(evs)
	got := b.Messages()
	if len(got) != MaxMessages || got[0].Event != 6 || got[len(got)-1].Event != MaxMessages+5 {
		t.Fatalf("%d messages from event %d", len(got), got[0].Event)
	}
}

func TestNewValidatesAndCopies(t *testing.T) {
	b := mustNew(t, 3)
	b.Apply(journal())
	snap := b.Snapshot()
	for name, mutate := range map[string]func(*Snapshot){
		"after offset": func(s *Snapshot) { s.Offset = 4 },
		"out of order": func(s *Snapshot) { s.Messages[1].Event = 1 },
		"invalid kind": func(s *Snapshot) { s.Messages[0].Kind = 99 },
		"no fixture":   func(s *Snapshot) { s.Messages[0].Fixture = 0 },
		"no season":    func(s *Snapshot) { s.Messages[2].Season = 0 },
		"too many":     func(s *Snapshot) { s.Messages = make([]Message, MaxMessages+1) },
	} {
		s := b.Snapshot()
		mutate(&s)
		if _, err := New(3, s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := New(0, snap); err == nil {
		t.Error("match messages accepted without a team")
	}
	r, err := New(3, snap)
	if err != nil {
		t.Fatal(err)
	}
	snap.Messages[0].Opponent = 99
	r.Messages()[1].Opponent = 99
	if r.Messages()[0].Opponent != 4 || r.Messages()[1].Opponent != 4 {
		t.Fatal("inbox shares memory with its snapshot or callers")
	}
}
