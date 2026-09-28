package app

import (
	"errors"
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

var ErrNoSuchMessage = errors.New("app: inbox message not found")

// MarkInboxRead marks one retained message as read. Message is its source
// event ID, as returned by Inbox. Reading the inbox itself changes nothing.
// It also works without a managed club, for league-wide messages.
type MarkInboxRead struct {
	ID               CommandID
	ExpectedRevision Revision
	Message          events.ID
}

// InboxRead is the recorded acknowledgement, returned unchanged on retries.
type InboxRead struct {
	Command  CommandID
	Revision Revision
	At       sim.GameInstant
	Message  events.ID
}

type InboxReadRecord struct {
	Request MarkInboxRead
	Result  InboxRead
}

// MarkInboxRead commits one acknowledgement and its event. A new command
// for an already-read message succeeds; retrying a recorded command emits
// nothing, even after the message has aged out of the inbox.
func (w *World) MarkInboxRead(cmd MarkInboxRead) (InboxRead, error) {
	if !validCommandID(cmd.ID) {
		return InboxRead{}, fmt.Errorf("%w: zero command ID", ErrInvalidCommand)
	}
	if rec, ok := w.commands[cmd.ID]; ok {
		if rec.inboxRead == nil || rec.inboxRead.Request != cmd {
			return InboxRead{}, ErrCommandIDReused
		}
		return rec.inboxRead.Result, nil
	}
	if cmd.ExpectedRevision != w.revision {
		return InboxRead{}, ErrStaleRevision
	}
	found := false
	for _, m := range w.inbox.Messages() {
		if m.Event == cmd.Message {
			found = true
			break
		}
	}
	if !found {
		return InboxRead{}, ErrNoSuchMessage
	}
	res := InboxRead{Command: cmd.ID, Revision: w.revision + 1, At: w.Now(), Message: cmd.Message}
	w.commands[cmd.ID] = commandRecord{inboxRead: &InboxReadRecord{Request: cmd, Result: res}}
	w.revision = res.Revision
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindInboxRead, InboxRead: &events.InboxRead{Message: cmd.Message}})
	w.publish()
	return res, nil
}

// UnreadInboxCount returns the number of unread retained messages. Read-only.
func (w *World) UnreadInboxCount() int { return w.inbox.UnreadCount() }

func (w *World) restoreInboxRead(c InboxReadRecord) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if q.ExpectedRevision >= r.Revision || r.Revision-q.ExpectedRevision != 1 || r.Revision > w.revision ||
		r.At < 0 || r.At > w.Now() || q.Message == 0 || q.Message != r.Message || q.Message >= w.lastEvent {
		return fmt.Errorf("invalid inbox read record %+v", c)
	}
	return nil
}

// Command records outlive the bounded journal. Cross-check retained read
// flags against them even when a full event replay is no longer possible.
func (w *World) validateInboxReads() []error {
	read := map[events.ID]bool{}
	for _, rec := range w.commands {
		if rec.inboxRead != nil {
			read[rec.inboxRead.Result.Message] = true
		}
	}
	var errs []error
	for _, m := range w.inbox.Messages() {
		if m.Read != read[m.Event] {
			errs = append(errs, fmt.Errorf("app: inbox message %d read flag differs from command log", m.Event))
		}
	}
	return errs
}
