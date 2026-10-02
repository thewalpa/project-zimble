package main

import (
	"net/http"
	"slices"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/inbox"
)

type inboxView struct {
	Messages []messageView
	Unread   int
	Sort     SortState
}

func (s *server) inbox(r *http.Request) (string, any, error) {
	msgs := s.messages()
	slices.Reverse(msgs)
	sortState := newSortState(r, "when", "desc")
	sortInboxMessages(msgs, sortState.Col, sortState.Dir)
	return "inbox", inboxView{Messages: msgs, Unread: s.w.UnreadInboxCount(), Sort: sortState}, nil
}

// messages renders the inbox, oldest first.
func (s *server) messages() []messageView {
	cal := s.w.Calendar()
	var out []messageView
	for _, m := range s.w.Inbox() {
		out = append(out, messageView{Event: uint64(m.Event), When: cal.Format(m.At), at: m.At, Text: s.messageText(m), Read: m.Read})
	}
	return out
}

func (s *server) messageText(m app.InboxItem) string {
	msg := present.InboxMessage(s.w, s.club(), m)
	if m.Kind == inbox.KindBidReceived {
		msg.Text += " on the Transfers page"
	}
	if msg.Topic == "" {
		return present.Capitalize(msg.Text)
	}
	return present.Capitalize(msg.Topic) + ": " + msg.Text
}

type messageView struct {
	Event uint64
	When  string
	Text  string
	Read  bool
	at    sim.GameInstant // When, for sorting
}
