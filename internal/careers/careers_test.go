package careers

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// journal appends events with consecutive IDs, each 10 minutes per ID.
type journal []events.Event

func (j *journal) add(k events.Kind, set func(*events.Event)) {
	id := events.ID(len(*j) + 1)
	e := events.Event{ID: id, OccurredAt: 10 * sim.GameInstant(id), Revision: uint64(id), Sequence: 1,
		Cause: events.Cause{Kind: events.CauseTask, ID: 1}, Kind: k, SchemaVersion: events.SchemaVersion}
	set(&e)
	*j = append(*j, e)
}

// deal is a transfer of player from seller to buyer (team IDs equal to club
// IDs) for fee whole units.
func deal(player ids.PlayerID, seller, buyer ids.ClubID, fee int64) events.Deal {
	return events.Deal{Offer: 1, Player: player, Seller: seller, SellerTeam: ids.TeamID(seller), Buyer: buyer, BuyerTeam: ids.TeamID(buyer), Fee: money.Money(100 * fee)}
}

// start: players 1 and 2 at club 1, player 3 at club 2.
func start(t *testing.T) *Store {
	t.Helper()
	s, err := Start(0, []Employed{{Player: 3, Club: 2}, {Player: 1, Club: 1}, {Player: 2, Club: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// story: player 1 is sold to club 2, then sold back; player 2's contract
// expires and club 2 signs him; player 3 is released and retires a free
// agent; youth player 4 joins club 1 and retires from it.
func story() journal {
	var j journal
	j.add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: deal(1, 1, 2, 50), Expires: 1000, WeeklyWage: 5}
	})
	j.add(events.KindContractExpired, func(e *events.Event) {
		e.ContractExpired = &events.ContractExpired{Player: 2, Club: 1, Team: 1}
	})
	j.add(events.KindPlayerSigned, func(e *events.Event) {
		e.PlayerSigned = &events.PlayerSigned{Player: 2, Club: 2, Team: 2, Expires: 1000, WeeklyWage: 5}
	})
	j.add(events.KindPlayerReleased, func(e *events.Event) {
		e.PlayerReleased = &events.PlayerReleased{Player: 3, Club: 2, Team: 2}
	})
	j.add(events.KindYouthJoined, func(e *events.Event) {
		e.YouthJoined = &events.YouthJoined{Player: 4, Club: 1, Team: 1, Expires: 1000, WeeklyWage: 5}
	})
	j.add(events.KindPlayerRetired, func(e *events.Event) { e.PlayerRetired = &events.PlayerRetired{Player: 3, Age: 35} })
	j.add(events.KindContractRenewed, func(e *events.Event) {
		e.ContractRenewed = &events.ContractRenewed{Player: 2, Club: 2, Team: 2, Expires: 2000, WeeklyWage: 5}
	})
	j.add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: deal(1, 2, 1, 70), Expires: 1000, WeeklyWage: 5}
	})
	j.add(events.KindPlayerRetired, func(e *events.Event) {
		e.PlayerRetired = &events.PlayerRetired{Player: 4, Club: 1, Team: 1, Age: 18}
	})
	return j
}

func TestCareersFollowEmploymentEvents(t *testing.T) {
	s := start(t)
	if n, err := s.Apply(story()); err != nil || n != 9 || s.Offset() != 9 {
		t.Fatalf("Apply = %d, %v; offset %d", n, err, s.Offset())
	}
	want := []Career{
		{Player: 1, Spells: []Spell{
			{Club: 1, From: 0, Joined: JoinedAtStart, Until: 10, Left: LeftTransfer},
			{Club: 2, From: 10, Joined: JoinedTransfer, Fee: 5000, Until: 80, Left: LeftTransfer},
			{Club: 1, From: 80, Joined: JoinedTransfer, Fee: 7000},
		}},
		{Player: 2, Spells: []Spell{
			{Club: 1, From: 0, Joined: JoinedAtStart, Until: 20, Left: LeftExpired},
			{Club: 2, From: 30, Joined: JoinedFree},
		}},
		{Player: 3, Spells: []Spell{{Club: 2, From: 0, Joined: JoinedAtStart, Until: 40, Left: LeftReleased}}},
		{Player: 4, Spells: []Spell{{Club: 1, From: 50, Joined: JoinedYouth, Until: 90, Left: LeftRetired}}},
	}
	if got := s.Careers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("careers\n%+v\nwant\n%+v", got, want)
	}

	// The snapshot restores the same store, and the initial snapshot plus
	// the journal rebuilds it.
	restored, err := New(s.Snapshot())
	if err != nil || !reflect.DeepEqual(restored.Snapshot(), s.Snapshot()) {
		t.Fatalf("restore: %v", err)
	}
	rebuilt, err := New(s.Initial())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rebuilt.Apply(story()); err != nil || !reflect.DeepEqual(rebuilt.Snapshot(), s.Snapshot()) {
		t.Fatalf("rebuild: %v", err)
	}

	// Redelivery changes nothing; queries return copies.
	if n, err := s.Apply(story()); err != nil || n != 0 {
		t.Fatalf("redelivery = %d, %v", n, err)
	}
	spells, _ := s.Career(1)
	spells[0].Club = 9
	if again, _ := s.Career(1); again[0].Club != 1 {
		t.Fatal("Career exposes storage")
	}
	if _, ok := s.Career(5); ok {
		t.Fatal("career for a player who never had a club")
	}
}

// match is a completed match between teams 1 and 2 (team IDs equal to club
// IDs) in which appeared played and scorers scored, one goal each.
func match(appeared []ids.PlayerID, scorers ...ids.PlayerID) func(*events.Event) {
	return func(e *events.Event) {
		e.Kind = events.KindMatchCompleted
		e.MatchCompleted = &events.MatchCompleted{Fixture: 1, Competition: 1, Season: 1, Round: 1, Home: 1, Away: 2,
			HomeGoals: uint16(len(scorers)), Appeared: appeared, Scorers: scorers}
	}
}

// Appearances and goals count at the club the player was at when he played:
// a transfer starts a new spell's count.
func TestCareersCountAppearancesAndGoals(t *testing.T) {
	var j journal
	j.add(events.KindMatchCompleted, match([]ids.PlayerID{1, 2, 3}, 1, 3, 1))
	j.add(events.KindTransferCompleted, func(e *events.Event) {
		e.TransferCompleted = &events.TransferCompleted{Deal: deal(1, 1, 2, 50), Expires: 1000, WeeklyWage: 5}
	})
	j.add(events.KindMatchCompleted, match([]ids.PlayerID{1, 3}, 1))
	j.add(events.KindMatchCompleted, match([]ids.PlayerID{2}))
	s := start(t)
	if _, err := s.Apply(j); err != nil {
		t.Fatal(err)
	}
	want := []Career{
		{Player: 1, Spells: []Spell{
			{Club: 1, From: 0, Joined: JoinedAtStart, Until: 20, Left: LeftTransfer, Appearances: 1, Goals: 2},
			{Club: 2, From: 20, Joined: JoinedTransfer, Fee: 5000, Appearances: 1, Goals: 1},
		}},
		{Player: 2, Spells: []Spell{{Club: 1, From: 0, Joined: JoinedAtStart, Appearances: 2}}},
		{Player: 3, Spells: []Spell{{Club: 2, From: 0, Joined: JoinedAtStart, Appearances: 2, Goals: 1}}},
	}
	if got := s.Careers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("careers\n%+v\nwant\n%+v", got, want)
	}
	rebuilt, err := New(s.Initial())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rebuilt.Apply(j); err != nil || !reflect.DeepEqual(rebuilt.Snapshot(), s.Snapshot()) {
		t.Fatalf("rebuild: %v", err)
	}
}

// Events that contradict the careers are rejected, and nothing changes.
func TestApplyRejectsContradictions(t *testing.T) {
	for name, set := range map[string]func(*events.Event){
		"sold by the wrong club": func(e *events.Event) {
			e.Kind = events.KindTransferCompleted
			e.TransferCompleted = &events.TransferCompleted{Deal: deal(1, 2, 3, 50), Expires: 1000, WeeklyWage: 5}
		},
		"expired at the wrong club": func(e *events.Event) {
			e.Kind = events.KindContractExpired
			e.ContractExpired = &events.ContractExpired{Player: 3, Club: 1, Team: 1}
		},
		"released without a club": func(e *events.Event) {
			e.Kind = events.KindPlayerReleased
			e.PlayerReleased = &events.PlayerReleased{Player: 9, Club: 1, Team: 1}
		},
		"signed while employed": func(e *events.Event) {
			e.Kind = events.KindPlayerSigned
			e.PlayerSigned = &events.PlayerSigned{Player: 1, Club: 2, Team: 2, Expires: 1000, WeeklyWage: 5}
		},
		"youth who already has a career": func(e *events.Event) {
			e.Kind = events.KindYouthJoined
			e.YouthJoined = &events.YouthJoined{Player: 2, Club: 1, Team: 1, Expires: 1000, WeeklyWage: 5}
		},
		"signed after retiring": func(e *events.Event) {
			e.Kind = events.KindPlayerSigned
			e.PlayerSigned = &events.PlayerSigned{Player: 3, Club: 1, Team: 1, Expires: 1000, WeeklyWage: 5}
		},
		"appeared without ever having a club": match([]ids.PlayerID{1, 9}),
		"appeared after his contract expired": match([]ids.PlayerID{1, 2}),
		"appeared after retiring":             match([]ids.PlayerID{3}),
		"retired as a free agent while employed": func(e *events.Event) {
			e.Kind = events.KindPlayerRetired
			e.PlayerRetired = &events.PlayerRetired{Player: 1, Age: 30}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := start(t)
			before := s.Snapshot()
			// Valid events first, so that the batch is partly applicable.
			var j journal
			j.add(events.KindContractExpired, func(e *events.Event) {
				e.ContractExpired = &events.ContractExpired{Player: 2, Club: 1, Team: 1}
			})
			j.add(events.KindPlayerRetired, func(e *events.Event) {
				e.PlayerRetired = &events.PlayerRetired{Player: 3, Club: 2, Team: 2, Age: 35}
			})
			j.add(0, set)
			if _, err := s.Apply(j); err == nil {
				t.Fatal("contradiction accepted")
			}
			if !reflect.DeepEqual(s.Snapshot(), before) {
				t.Fatal("rejected batch changed the store")
			}
		})
	}
}

func TestApplyRejectsGapsAndInvalidEvents(t *testing.T) {
	s := start(t)
	j := story()
	if _, err := s.Apply(j[1:]); err == nil {
		t.Fatal("gap accepted")
	}
	j[0].TransferCompleted.Fee = 0
	if _, err := s.Apply(j[:1]); err == nil {
		t.Fatal("invalid event accepted")
	}
	if s.Offset() != 0 {
		t.Fatal("rejected events moved the offset")
	}
}

func TestNewRejectsMalformedSnapshots(t *testing.T) {
	at := func(club ids.ClubID, from sim.GameInstant, joined Joined, until sim.GameInstant, left Left) Spell {
		return Spell{Club: club, From: from, Joined: joined, Until: until, Left: left}
	}
	bought := func(club ids.ClubID, from sim.GameInstant) Spell {
		return Spell{Club: club, From: from, Joined: JoinedTransfer, Fee: 10}
	}
	for name, careers := range map[string][]Career{
		"player zero":               {{Player: 0, Spells: []Spell{at(1, 0, JoinedAtStart, 0, LeftNot)}}},
		"players out of order":      {{Player: 2, Spells: []Spell{at(1, 0, JoinedAtStart, 0, LeftNot)}}, {Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 0, LeftNot)}}},
		"no spells":                 {{Player: 1}},
		"club zero":                 {{Player: 1, Spells: []Spell{at(0, 0, JoinedAtStart, 0, LeftNot)}}},
		"unknown joined":            {{Player: 1, Spells: []Spell{at(1, 0, 9, 0, LeftNot)}}},
		"unknown left":              {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 5, 9)}}},
		"current not last":          {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 0, LeftNot), at(2, 5, JoinedFree, 0, LeftNot)}}},
		"current with an end":       {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 5, LeftNot)}}},
		"ends before start":         {{Player: 1, Spells: []Spell{at(1, 10, JoinedFree, 5, LeftExpired)}}},
		"overlap":                   {{Player: 1, Spells: []Spell{at(1, 0, JoinedFree, 10, LeftExpired), at(2, 5, JoinedFree, 0, LeftNot)}}},
		"youth later":               {{Player: 1, Spells: []Spell{at(1, 0, JoinedFree, 10, LeftExpired), at(2, 15, JoinedYouth, 0, LeftNot)}}},
		"free with a fee":           {{Player: 1, Spells: []Spell{{Club: 1, Joined: JoinedFree, Fee: 10}}}},
		"transfer first":            {{Player: 1, Spells: []Spell{bought(1, 0)}}},
		"transfer after expiry":     {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 10, LeftExpired), bought(2, 10)}}},
		"transfer later":            {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 10, LeftTransfer), bought(2, 20)}}},
		"transfer same club":        {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 10, LeftTransfer), bought(1, 10)}}},
		"retired then joined":       {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 10, LeftRetired), at(2, 20, JoinedFree, 0, LeftNot)}}},
		"sale without buyer":        {{Player: 1, Spells: []Spell{at(1, 0, JoinedAtStart, 10, LeftTransfer)}}},
		"goals without appearances": {{Player: 1, Spells: []Spell{{Club: 1, Joined: JoinedAtStart, Goals: 1}}}},
	} {
		if _, err := New(Snapshot{Careers: careers}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Start(0, []Employed{{Player: 1, Club: 1}, {Player: 1, Club: 2}}); err == nil {
		t.Error("duplicate starting player accepted")
	}
	if _, err := New(Snapshot{}); err != nil {
		t.Errorf("empty store: %v", err)
	}
}
