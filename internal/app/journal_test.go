package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/selection"
)

// managedSeason plays season 1 with a submitted lineup for each user
// fixture, through the season end.
func managedSeason(t *testing.T) *World {
	t.Helper()
	w := userWorld(t, 42, userClub)
	playWithLineups(t, w, func(f ids.FixtureID) selection.Lineup { return changedLineup(t, w, f) })
	return w
}

// Every commit appends its events with the new revision; the journal agrees
// with the official state it describes.
func TestJournalRecordsEveryCommit(t *testing.T) {
	w := managedSeason(t)
	journal := w.Events()
	count := map[events.Kind]int{}
	for i, e := range journal {
		count[e.Kind]++
		if e.ID != events.ID(i+1) {
			t.Fatalf("event %d at position %d", e.ID, i)
		}
	}
	weeks := int(w.Now() / (7 * day)) // one wage run per elapsed week
	want := map[events.Kind]int{
		events.KindRoundStarted: 14, events.KindMatchCompleted: 56, events.KindLineupSubmitted: 14,
		events.KindSeasonEnded: 1, events.KindSeasonStarted: 1, events.KindLedgerPosted: 14 + weeks,
	}
	if !reflect.DeepEqual(count, want) {
		t.Fatalf("event counts %v, want %v", count, want)
	}
	if err := w.Validate(); err != nil { // includes validateJournal's cross-checks
		t.Fatal(err)
	}

	// Each ResolveRounds result corresponds to its MatchCompleted events
	// (same command cause, revision and instant, in fixture order), followed
	// by one LedgerPosted with a gate entry per match.
	for _, rec := range w.Snapshot().ResolveCommands {
		var got []ids.FixtureID
		ledger := 0
		for _, e := range journal {
			if e.Cause != commandCause(rec.Request.ID) {
				continue
			}
			if e.Revision != uint64(rec.Result.Revision) || e.OccurredAt != rec.Result.At {
				t.Fatalf("command %d event %+v", rec.Request.ID, e)
			}
			switch e.Kind {
			case events.KindMatchCompleted:
				got = append(got, e.MatchCompleted.Fixture)
			case events.KindLedgerPosted:
				ledger++
				if len(e.LedgerPosted.Entries) != len(rec.Result.Matches) {
					t.Fatalf("command %d posted %d gate entries", rec.Request.ID, len(e.LedgerPosted.Entries))
				}
			default:
				t.Fatalf("command %d caused a %s event", rec.Request.ID, e.Kind)
			}
		}
		if ledger != 1 {
			t.Fatalf("command %d has %d ledger events", rec.Request.ID, ledger)
		}
		var want []ids.FixtureID
		for _, m := range rec.Result.Matches {
			want = append(want, m.Fixture)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("command %d events for %v, matches %v", rec.Request.ID, got, want)
		}
	}
	var last []events.Event
	for _, e := range journal {
		if e.Kind == events.KindSeasonEnded || e.Kind == events.KindSeasonStarted {
			last = append(last, e)
		}
	}
	table, _ := w.Table(competitions.SeasonRef{Competition: 1, Season: 1})
	if last[0].Kind != events.KindSeasonEnded || last[0].SeasonEnded.Ranking[0] != table.Rows[0].Team ||
		last[1].Kind != events.KindSeasonStarted || last[1].SeasonStarted.Season != 2 || last[0].Cause != last[1].Cause || last[0].Cause.Kind != events.CauseTask {
		t.Fatalf("season events %+v", last)
	}
}

// Rejected or retried commands, reads and a paused Continue emit nothing.
func TestOnlyCommitsEmitEvents(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playBatches(t, w, 2)
	ready := readyBatch(t, w)
	before := w.Events()
	w.Pending()
	w.Inbox()
	w.SuggestLineup(ready.UserFixtures[0])
	mustContinue(t, w, w.Now()+day) // paused: returns the ready batch
	if _, err := w.ResolveRounds(commandFor(ready, w.NextCommandID()+5).withRevision(ready.Revision - 1)); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	bad := changedLineup(t, w, ready.UserFixtures[0])
	bad.Starters = bad.Starters[1:]
	if _, err := w.SubmitLineup(SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: ready.UserFixtures[0], Lineup: bad}); err == nil {
		t.Fatal("bad lineup accepted")
	}
	retry := w.Snapshot().ResolveCommands[0].Request
	if _, err := w.ResolveRounds(retry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.Events(), before) {
		t.Fatal("a read, paused Continue, rejected command or retry emitted events")
	}
}

func (c ResolveRounds) withRevision(r Revision) ResolveRounds { c.ExpectedRevision = r; return c }

// The inbox holds the manager's matchdays and results and every season
// message, and equals a rebuild from the journal delivered in any chunks.
func TestInboxIsAProjectionOfTheJournal(t *testing.T) {
	w := managedSeason(t)
	team := mustUserTeam(t, w)
	count := map[inbox.Kind]int{}
	for _, m := range w.Inbox() {
		count[m.Kind]++
		switch m.Kind {
		case inbox.KindResult:
			r, _ := w.competitions.Result(m.Fixture)
			goals := [2]uint16{r.HomeGoals, r.AwayGoals}
			if r.Away == team {
				goals = [2]uint16{r.AwayGoals, r.HomeGoals}
			}
			if (r.Home != team && r.Away != team) || m.Goals != goals || m.Home != (r.Home == team) || m.OpponentLabel.Team != m.Opponent {
				t.Fatalf("result message %+v for %+v", m, r)
			}
		case inbox.KindSeasonEnded:
			table, _ := w.Table(competitions.SeasonRef{Competition: 1, Season: 1})
			pos := slices.IndexFunc(table.Rows, func(r TableRow) bool { return r.Team == team }) + 1
			if m.ChampionLabel != table.Rows[0].Label || m.Position != pos || m.CompetitionName != "Founders League" {
				t.Fatalf("season message %+v, position %d", m, pos)
			}
		}
	}
	if want := map[inbox.Kind]int{inbox.KindMatchday: 14, inbox.KindResult: 14, inbox.KindSeasonEnded: 1, inbox.KindSeasonStarted: 1}; !reflect.DeepEqual(count, want) {
		t.Fatalf("message counts %v, want %v", count, want)
	}

	journal := w.Events()
	for _, chunk := range []int{1, 7, 50, len(journal)} {
		rebuilt, _ := inbox.New(team, inbox.Snapshot{})
		for i := 0; i < len(journal); i += chunk {
			if _, err := rebuilt.Apply(journal[:min(i+chunk, len(journal))]); err != nil { // re-delivers the prefix
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(rebuilt.Snapshot(), w.inbox.Snapshot()) {
			t.Fatalf("rebuild in chunks of %d differs", chunk)
		}
	}
	plain := newWorld(t, 42)
	playSeason(t, plain)
	for _, m := range plain.Inbox() {
		if m.Kind == inbox.KindMatchday || m.Kind == inbox.KindResult {
			t.Fatal("a world without a user club has match messages")
		}
	}
}

// Saving at any point neither loses nor duplicates events or messages.
func TestJournalAndInboxSurviveSaves(t *testing.T) {
	straight := managedSeason(t)
	pick := func(w *World) func(ids.FixtureID) selection.Lineup {
		return func(f ids.FixtureID) selection.Lineup { return changedLineup(t, w, f) }
	}
	w := userWorld(t, 42, userClub)
	for w.lastEvent < 40 {
		ready := readyBatch(t, w)
		submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
		w = roundTrip(t, w) // saved between the lineup and the result
		resolveNow(t, w)
		w = roundTrip(t, w)
	}
	playWithLineups(t, w, pick(w))
	if !reflect.DeepEqual(w.Events(), straight.Events()) || !reflect.DeepEqual(w.Inbox(), straight.Inbox()) {
		t.Fatal("saves changed the journal or inbox")
	}
}

// Old events are dropped once consumed; the world stays valid and loadable.
func TestJournalRetention(t *testing.T) {
	defer func(n int) { journalRetention = n }(journalRetention)
	journalRetention = 30
	w := managedSeason(t)
	j := w.Events()
	if len(j) != 30 || j[len(j)-1].ID != w.lastEvent || w.lastEvent < 100 || w.inbox.Offset() != w.lastEvent {
		t.Fatalf("journal %d..%d (%d events), allocator %d", j[0].ID, j[len(j)-1].ID, len(j), w.lastEvent)
	}
	if len(w.Inbox()) != 30 {
		t.Fatalf("inbox lost messages when the journal was trimmed: %d", len(w.Inbox()))
	}
	roundTrip(t, w)
}

func TestRestoreRejectsInvalidJournal(t *testing.T) {
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		for range 3 {
			ready := readyBatch(t, w)
			submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
			resolveNow(t, w)
		}
		readyBatch(t, w)
		return w.Snapshot()
	}
	find := func(s *WorldSnapshot, k events.Kind) *events.Event {
		for i := range s.Events {
			if s.Events[i].Kind == k {
				return &s.Events[i]
			}
		}
		t.Fatalf("no %s event", k)
		return nil
	}
	cases := map[string]func(*WorldSnapshot){
		"event missing":         func(s *WorldSnapshot) { s.Events = slices.Delete(s.Events, 3, 4) },
		"allocator ahead":       func(s *WorldSnapshot) { s.LastEvent++ },
		"unknown kind":          func(s *WorldSnapshot) { s.Events[0].Kind = 99 },
		"payload of other kind": func(s *WorldSnapshot) { find(s, events.KindMatchCompleted).Kind = events.KindLineupSubmitted },
		"score edited":          func(s *WorldSnapshot) { find(s, events.KindMatchCompleted).MatchCompleted.HomeGoals += 1 },
		"round fixtures edited": func(s *WorldSnapshot) { find(s, events.KindRoundStarted).RoundStarted.Fixtures[0].Home = 99 },
		"lineup for other team": func(s *WorldSnapshot) { find(s, events.KindLineupSubmitted).LineupSubmitted.Team = 99 },
		"unknown command cause": func(s *WorldSnapshot) { find(s, events.KindMatchCompleted).Cause.ID = 999 },
		"unallocated task":      func(s *WorldSnapshot) { find(s, events.KindRoundStarted).Cause.ID = 9999 },
		"future revision":       func(s *WorldSnapshot) { s.Events[len(s.Events)-1].Revision = uint64(s.Revision) + 1 },
		"sequence broken":       func(s *WorldSnapshot) { s.Events[2].Sequence = 7 },
		"inbox behind":          func(s *WorldSnapshot) { s.Inbox.Offset-- },
		"inbox message edited":  func(s *WorldSnapshot) { s.Inbox.Messages[1].Goals[0] += 1 },
		"inbox message dropped": func(s *WorldSnapshot) { s.Inbox.Messages = s.Inbox.Messages[1:] },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}
