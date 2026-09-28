package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/events"
)

func TestInboxReadCommandAndContinuation(t *testing.T) {
	w := userWorld(t, 42, userClub)
	readyBatch(t, w)
	messages := w.Inbox()
	before := w.Snapshot()
	if len(messages) == 0 || w.UnreadInboxCount() != len(messages) {
		t.Fatal("new messages not unread")
	}
	messages[0].Read = true
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("queries mutated state")
	}
	cmd := MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: messages[0].Event}
	result, err := w.MarkInboxRead(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != before.Revision+1 || w.UnreadInboxCount() != len(messages)-1 || !w.Inbox()[0].Read {
		t.Fatal("command did not mark exactly one message")
	}
	j := w.Events()
	e := j[len(j)-1]
	if len(j) != len(before.Events)+1 || e.Kind != events.KindInboxRead || e.InboxRead.Message != cmd.Message ||
		e.Cause != commandCause(cmd.ID) || e.Revision != uint64(result.Revision) || e.OccurredAt != w.Now() {
		t.Fatalf("read event %+v", e)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	loaded := roundTrip(t, w)
	for _, x := range []*World{w, loaded} {
		before := x.Snapshot()
		retry, err := x.MarkInboxRead(cmd)
		if err != nil || retry != result || !reflect.DeepEqual(before, x.Snapshot()) {
			t.Fatal("retry changed world")
		}
		if x.NextCommandID() != cmd.ID+1 {
			t.Fatal("command allocator lost read command")
		}
		other := cmd
		other.Message++
		if _, err := x.MarkInboxRead(other); !errors.Is(err, ErrCommandIDReused) {
			t.Fatalf("reused ID: %v", err)
		}
		if _, err := x.MarkInboxRead(MarkInboxRead{ID: x.NextCommandID(), ExpectedRevision: x.Revision(), Message: cmd.Message}); err != nil {
			t.Fatal("already-read message rejected:", err)
		}
		if x.UnreadInboxCount() != len(messages)-1 {
			t.Fatal("read twice changed unread count")
		}
		resolveNow(t, x)
		if x.UnreadInboxCount() != len(x.Inbox())-1 {
			t.Fatal("new arrivals did not stay unread")
		}
	}
	if !reflect.DeepEqual(w.Snapshot(), loaded.Snapshot()) {
		t.Fatal("save changed continuation")
	}
}

func TestInboxReadCommandRejectionsAreAtomic(t *testing.T) {
	w := userWorld(t, 42, userClub)
	readyBatch(t, w)
	resolveNow(t, w)
	before := w.Snapshot()
	command := MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event}
	var invisible events.ID
	for _, e := range w.Events() {
		if e.Kind == events.KindLedgerPosted {
			invisible = e.ID
			break
		}
	}
	if invisible == 0 {
		t.Fatal("need an event without an inbox message")
	}
	cases := []struct {
		name   string
		change func(*MarkInboxRead)
		want   error
	}{
		{"zero command", func(c *MarkInboxRead) { c.ID = 0 }, ErrInvalidCommand},
		{"stale", func(c *MarkInboxRead) { c.ExpectedRevision-- }, ErrStaleRevision},
		{"zero message", func(c *MarkInboxRead) { c.Message = 0 }, ErrNoSuchMessage},
		{"future message", func(c *MarkInboxRead) { c.Message = w.lastEvent + 1 }, ErrNoSuchMessage},
		{"invisible event", func(c *MarkInboxRead) { c.Message = invisible }, ErrNoSuchMessage},
		{"other command kind", func(c *MarkInboxRead) { c.ID = before.ResolveCommands[0].Request.ID }, ErrCommandIDReused},
	}
	for _, tc := range cases {
		c := command
		tc.change(&c)
		if _, err := w.MarkInboxRead(c); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(before, w.Snapshot()) {
			t.Fatalf("%s mutated world", tc.name)
		}
	}
	if _, err := w.MarkInboxRead(command); err != nil {
		t.Fatal("failed attempt reserved command ID:", err)
	}
}

func TestInboxReadsSurviveJournalRetention(t *testing.T) {
	defer func(n int) { journalRetention = n }(journalRetention)
	journalRetention = 1
	w := userWorld(t, 42, userClub)
	readyBatch(t, w)
	cmd := MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event}
	result, err := w.MarkInboxRead(cmd)
	if err != nil {
		t.Fatal(err)
	}
	resolveNow(t, w) // drops both the message's source event and its read event
	loaded := roundTrip(t, w)
	if !loaded.Inbox()[0].Read || loaded.UnreadInboxCount() != len(loaded.Inbox())-1 {
		t.Fatal("retention lost read state")
	}
	retry, err := loaded.MarkInboxRead(cmd)
	if err != nil || retry != result {
		t.Fatal("retention lost retry result")
	}
	snap := loaded.Snapshot()
	snap.Inbox.Messages[0].Read = false
	if _, err := Restore(snap); !errors.Is(err, ErrInvalidSave) {
		t.Fatalf("tampered read flag: %v", err)
	}
}

func TestRestoreRejectsInvalidInboxRead(t *testing.T) {
	w := userWorld(t, 42, userClub)
	readyBatch(t, w)
	resolveNow(t, w)
	_, err := w.MarkInboxRead(MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*WorldSnapshot){
		"missing command":         func(s *WorldSnapshot) { s.InboxReadCommands = nil },
		"duplicate command":       func(s *WorldSnapshot) { s.InboxReadCommands = append(s.InboxReadCommands, s.InboxReadCommands[0]) },
		"zero command":            func(s *WorldSnapshot) { s.InboxReadCommands[0].Request.ID = 0 },
		"wrong result command":    func(s *WorldSnapshot) { s.InboxReadCommands[0].Result.Command++ },
		"wrong result target":     func(s *WorldSnapshot) { s.InboxReadCommands[0].Result.Message++ },
		"future result revision":  func(s *WorldSnapshot) { s.InboxReadCommands[0].Result.Revision++ },
		"wrong expected revision": func(s *WorldSnapshot) { s.InboxReadCommands[0].Request.ExpectedRevision-- },
		"future time":             func(s *WorldSnapshot) { s.InboxReadCommands[0].Result.At++ },
		"read flag removed":       func(s *WorldSnapshot) { s.Inbox.Messages[0].Read = false },
		"read flag added":         func(s *WorldSnapshot) { s.Inbox.Messages[1].Read = true },
		"wrong event target":      func(s *WorldSnapshot) { s.Events[len(s.Events)-1].InboxRead.Message = s.Inbox.Messages[1].Event },
		"wrong event cause":       func(s *WorldSnapshot) { s.Events[len(s.Events)-1].Cause.ID = uint64(s.ResolveCommands[0].Request.ID) },
	}
	for name, mutate := range cases {
		snap := w.Snapshot()
		mutate(&snap)
		if _, err := Restore(snap); !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: %v", name, err)
		}
	}
	snapshot := w.Snapshot()
	snapshot.InboxReadCommands[0].Result.Message = 999
	snapshot.Events[len(snapshot.Events)-1].InboxRead.Message = 999
	if _, err := Restore(w.Snapshot()); err != nil {
		t.Fatal("snapshot shared read records:", err)
	}
}

func TestInboxReadWithoutManagedClub(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	if len(w.Inbox()) == 0 {
		t.Fatal("no league-wide messages")
	}
	_, err := w.MarkInboxRead(MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, w)
}

func TestInboxReadRetryAfterMessageEviction(t *testing.T) {
	w := userWorld(t, 42, userClub)
	readyBatch(t, w)
	cmd := MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event}
	result, err := w.MarkInboxRead(cmd)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		playSeason(t, w)
		if w.Inbox()[0].Event > cmd.Message {
			break
		}
	}
	if w.Inbox()[0].Event <= cmd.Message {
		t.Fatal("message was not evicted")
	}
	w = roundTrip(t, w)
	before := w.Snapshot()
	if retry, err := w.MarkInboxRead(cmd); err != nil || retry != result {
		t.Fatalf("retry after eviction: %+v, %v", retry, err)
	}
	if _, err := w.MarkInboxRead(MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: cmd.Message}); !errors.Is(err, ErrNoSuchMessage) {
		t.Fatalf("new command for evicted message: %v", err)
	}
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("retry or rejected command changed world")
	}
}
