package inbox

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/events"
)

func readEvent(id, message events.ID) events.Event {
	e := env(id, events.KindInboxRead)
	e.Cause.Kind = events.CauseCommand
	e.InboxRead = &events.InboxRead{Message: message}
	return e
}

func TestReadEventsReplayAndRollback(t *testing.T) {
	j := append(journal(), readEvent(7, 3), readEvent(8, 1), readEvent(9, 3))
	batch, single := mustNew(t, 3), mustNew(t, 3)
	if _, err := batch.Apply(j); err != nil {
		t.Fatal(err)
	}
	for _, e := range j {
		if _, err := single.Apply([]events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(batch.Snapshot(), single.Snapshot()) {
		t.Fatal("batch differs from individual replay")
	}
	if batch.UnreadCount() != 2 || len(batch.Messages()) != 4 || !batch.Messages()[0].Read || !batch.Messages()[1].Read {
		t.Fatalf("read state %+v", batch.Snapshot())
	}
	before := batch.Snapshot()
	if n, err := batch.Apply(j); n != 0 || err != nil || !reflect.DeepEqual(before, batch.Snapshot()) {
		t.Fatal("duplicate delivery changed inbox")
	}
	for _, bad := range []events.Event{readEvent(11, 4), readEvent(12, 6)} {
		if _, err := batch.Apply([]events.Event{readEvent(10, 5), bad}); err == nil {
			t.Fatal("invalid batch accepted")
		}
		if !reflect.DeepEqual(before, batch.Snapshot()) {
			t.Fatal("failed batch leaked a read flag")
		}
	}
	restored, err := New(3, before)
	if err != nil {
		t.Fatal(err)
	}
	before.Messages[0].Read = false
	if !restored.Messages()[0].Read {
		t.Fatal("restore shares read flags")
	}
	copy := restored.Messages()
	copy[0].Read = false
	if !restored.Messages()[0].Read {
		t.Fatal("query exposes read flags")
	}
}

func TestReadRetentionIsIndependentOfBatching(t *testing.T) {
	var j []events.Event
	next := func() events.ID { return events.ID(len(j) + 1) }
	for i := 0; i < MaxMessages+2; i++ {
		e := env(next(), events.KindSeasonStarted)
		e.SeasonStarted = journal()[5].SeasonStarted
		j = append(j, e, readEvent(e.ID+1, e.ID))
	}
	batch, single := mustNew(t, 0), mustNew(t, 0)
	if _, err := batch.Apply(j); err != nil {
		t.Fatal(err)
	}
	for _, e := range j {
		if _, err := single.Apply([]events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(batch.Snapshot(), single.Snapshot()) || batch.UnreadCount() != 0 || len(batch.Messages()) != MaxMessages {
		t.Fatal("retention changed with batching")
	}
	before := batch.Snapshot()
	if _, err := batch.Apply([]events.Event{readEvent(next(), 1)}); err == nil {
		t.Fatal("evicted message accepted")
	}
	if !reflect.DeepEqual(before, batch.Snapshot()) {
		t.Fatal("failed read changed inbox")
	}
}
