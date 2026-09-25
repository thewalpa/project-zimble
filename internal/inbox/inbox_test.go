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
		"invalid kind": func(s *Snapshot) { s.Messages[0].Kind = 9 },
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
