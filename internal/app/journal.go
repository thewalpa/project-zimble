package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/inbox"
)

// journalRetention is how many of the most recent events the journal keeps.
// Older events are dropped only after every consumer (the inbox) has
// consumed them; the inbox keeps its own bounded message history. About 240
// events are emitted per year with a managed club in an 8-team league (86
// match and season events, 52 weekly wage postings, 14 gate postings, about
// 50 contract events and about 35 retirements, youth arrivals and one
// development, plus a few dozen transfer events in a summer the manager
// trades), so this keeps roughly the last three to four years.
var journalRetention = 1000

// emit stages an event produced by a change that has already been applied
// to the owning modules. Staged events become visible with the commit's new
// revision when publish runs.
func (w *World) emit(at sim.GameInstant, cause events.Cause, e events.Event) {
	e.OccurredAt, e.Cause = at, cause
	w.outbox = append(w.outbox, e)
}

// publish completes a commit whose revision has just been incremented: it
// assigns event IDs and the revision to the staged events, appends them to
// the journal, lets the read models (the inbox and the careers) consume them
// and trims the journal. It cannot fail for events produced by this package;
// a failure is a broken invariant.
func (w *World) publish() {
	for i := range w.outbox {
		w.lastEvent++
		e := &w.outbox[i]
		e.ID, e.Revision, e.Sequence, e.SchemaVersion = w.lastEvent, uint64(w.revision), uint32(i+1), events.SchemaVersion
	}
	if _, err := w.inbox.Apply(w.outbox); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	if _, err := w.careers.Apply(w.outbox); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	w.journal = append(w.journal, w.outbox...)
	w.outbox = nil
	if n := len(w.journal) - journalRetention; n > 0 {
		// Every dropped event is at or before both read models' offsets.
		w.journal = slices.Delete(w.journal, 0, n)
	}
}

func commandCause(id CommandID) events.Cause {
	return events.Cause{Kind: events.CauseCommand, ID: uint64(id)}
}

func taskCause(id sim.TaskID) events.Cause {
	return events.Cause{Kind: events.CauseTask, ID: uint64(id)}
}

// Events returns the retained journal, oldest first, as copies. Read-only.
func (w *World) Events() []events.Event { return events.CloneAll(w.journal) }

// newInbox builds an empty inbox for the user club's senior team (or for no
// team).
func (w *World) newInbox() (*inbox.Inbox, error) {
	team, _ := w.userTeam()
	return inbox.New(team, inbox.Snapshot{})
}

// validateJournal checks the journal against the world:
//
//   - IDs are contiguous and end at the allocator; at most journalRetention
//     are kept; each event is valid;
//   - revisions and times never decrease and never exceed the world's; the
//     sequence restarts at 1 for each commit;
//   - causes name a recorded command or an allocated task;
//   - payloads agree with the owning modules (official results, rounds,
//     lineups, seasons);
//   - the inbox has consumed every event, and when the journal is complete
//     (it still starts at event 1) the inbox equals a rebuild from it.
func (w *World) validateJournal() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	if len(w.journal) > journalRetention {
		fail("journal holds %d events, retention is %d", len(w.journal), journalRetention)
	}
	// Trimming keeps the newest events, so the journal is empty only before
	// the first event.
	if n := len(w.journal); (n == 0) != (w.lastEvent == 0) || (n > 0 && w.journal[n-1].ID != w.lastEvent) {
		fail("journal does not end at the event allocator %d", w.lastEvent)
	}
	lastTask := w.scheduler.Snapshot().LastTaskID
	for i, e := range w.journal {
		if err := e.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if i > 0 {
			prev := w.journal[i-1]
			switch {
			case e.ID != prev.ID+1:
				fail("event %d follows event %d", e.ID, prev.ID)
			case e.Revision < prev.Revision || e.OccurredAt < prev.OccurredAt:
				fail("event %d goes back in revision or time", e.ID)
			case e.Revision == prev.Revision && e.Sequence != prev.Sequence+1, e.Revision != prev.Revision && e.Sequence != 1:
				fail("event %d has sequence %d in revision %d", e.ID, e.Sequence, e.Revision)
			}
		}
		if e.Revision > uint64(w.revision) || e.OccurredAt > w.Now() {
			fail("event %d is from the future (revision %d, at %d)", e.ID, e.Revision, e.OccurredAt)
		}
		switch e.Cause.Kind {
		case events.CauseCommand:
			if _, ok := w.commands[CommandID(e.Cause.ID)]; !ok {
				fail("event %d caused by unrecorded command %d", e.ID, e.Cause.ID)
			}
		case events.CauseTask:
			if sim.TaskID(e.Cause.ID) > lastTask {
				fail("event %d caused by unallocated task %d", e.ID, e.Cause.ID)
			}
		}
		if err := w.checkEventFacts(e); err != nil {
			fail("event %d (%s): %v", e.ID, e.Kind, err)
		}
	}

	if w.inbox.Offset() != w.lastEvent {
		fail("inbox consumed up to event %d, journal is at %d", w.inbox.Offset(), w.lastEvent)
	}
	if len(w.journal) == 0 && w.lastEvent == 0 || len(w.journal) > 0 && w.journal[0].ID == 1 {
		rebuilt, err := w.newInbox()
		if err == nil {
			_, err = rebuilt.Apply(w.journal)
		}
		if err != nil || !slices.Equal(rebuilt.Messages(), w.inbox.Messages()) {
			fail("inbox differs from a rebuild from the journal (%v)", err)
		}
	}
	errs = append(errs, w.validateInboxReads()...)
	return errs
}

// checkEventFacts compares an event's payload with the owning modules.
func (w *World) checkEventFacts(e events.Event) error {
	switch e.Kind {
	case events.KindInboxRead:
		rec := w.commands[CommandID(e.Cause.ID)].inboxRead
		if rec == nil || rec.Result.Message != e.InboxRead.Message ||
			uint64(rec.Result.Revision) != e.Revision || rec.Result.At != e.OccurredAt {
			return errors.New("differs from the recorded inbox read")
		}
	case events.KindRoundStarted:
		p := e.RoundStarted
		info, ok := w.competitions.Round(competitions.RoundRef{
			Season: competitions.SeasonRef{Competition: p.Competition, Season: competitions.Season(p.Season)},
			Round:  competitions.Round(p.Round),
		})
		if !ok || info.Status == competitions.RoundScheduled || info.Kickoff != e.OccurredAt || len(info.Fixtures) != len(p.Fixtures) {
			return errors.New("round does not match")
		}
		for i, pf := range p.Fixtures {
			if f, _ := w.competitions.Fixture(info.Fixtures[i]); f.ID != pf.Fixture || f.Home != pf.Home || f.Away != pf.Away {
				return fmt.Errorf("fixture %d does not match", pf.Fixture)
			}
		}
	case events.KindMatchCompleted:
		p := e.MatchCompleted
		r, ok := w.competitions.Result(p.Fixture)
		if !ok || r.Home != p.Home || r.Away != p.Away || r.HomeGoals != p.HomeGoals || r.AwayGoals != p.AwayGoals ||
			r.HomePenalties != p.HomePenalties || r.AwayPenalties != p.AwayPenalties ||
			r.Season.Competition != p.Competition || r.Season.Season != competitions.Season(p.Season) || r.Round != competitions.Round(p.Round) || r.RecordedAt != e.OccurredAt {
			return errors.New("differs from the official result")
		}
	case events.KindLineupSubmitted:
		p := e.LineupSubmitted
		f, ok := w.competitions.Fixture(p.Fixture)
		if !ok || (f.Home != p.Team && f.Away != p.Team) {
			return errors.New("team does not play the fixture")
		}
	case events.KindSeasonEnded:
		p := e.SeasonEnded
		ref := competitions.SeasonRef{Competition: p.Competition, Season: competitions.Season(p.Season)}
		if !w.competitions.SeasonCompleted(ref) || !slices.Equal(w.competitions.Ranking(ref), p.Ranking) {
			return errors.New("ranking differs from the final ranking")
		}
	case events.KindLedgerPosted:
		return w.checkLedgerEvent(e.LedgerPosted)
	case events.KindContractRenewed:
		p := e.ContractRenewed
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindContractExpired:
		p := e.ContractExpired
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindPlayerSigned:
		p := e.PlayerSigned
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindPlayerReleased:
		p := e.PlayerReleased
		if rec := w.commands[CommandID(e.Cause.ID)].release; e.Cause.Kind != events.CauseCommand || rec == nil ||
			rec.Result.Player != p.Player || rec.Result.Compensation != p.Compensation || p.Club != w.userClub {
			return errors.New("differs from the recorded release")
		}
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindPlayerRetired, events.KindYouthJoined, events.KindPlayersDeveloped:
		return w.checkLifecycleEvent(e)
	case events.KindTransferOffered, events.KindTransferCompleted, events.KindOfferClosed:
		return w.checkTransferEvent(e)
	case events.KindPlayerListed, events.KindPlayerUnlisted:
		return w.checkListingEvent(e)
	case events.KindPlayerInjured:
		p := e.PlayerInjured
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindPlayerRecovered:
		p := e.PlayerRecovered
		if p.Club.Valid() {
			return w.checkPlayerEvent(p.Player, p.Club, p.Team)
		}
		if _, ok := w.registry.Player(p.Player); !ok {
			return fmt.Errorf("unknown player %d", p.Player)
		}
	case events.KindSeasonStarted:
		p := e.SeasonStarted
		ref := competitions.SeasonRef{Competition: p.Competition, Season: competitions.Season(p.Season)}
		entrants, ok := w.competitions.Entrants(ref)
		if rounds := w.competitions.Rounds(ref); !ok || len(rounds) == 0 || rounds[0].Kickoff != p.FirstKickoff || !slices.Equal(entrants, p.Entrants) {
			return errors.New("season does not match")
		}
	}
	return nil
}

// InboxItem is an inbox message with display names resolved.
type InboxItem struct {
	inbox.Message
	CompetitionName string
	Cup             bool      // the competition is a cup
	RoundName       string    // matchday, result: see RoundName
	Stage           string    // season ended, cup: "winner", or the round the team went out in, e.g. "semi-final"
	OpponentLabel   TeamLabel // matchday, result
	ChampionLabel   TeamLabel // season ended
	PlayerName      string    // renewed, left, joined, retired, youth, transfers, released
	ClubName        string    // transfers: the other club
}

// Inbox returns the manager's messages, oldest first. Read-only.
func (w *World) Inbox() []InboxItem {
	var out []InboxItem
	for _, m := range w.inbox.Messages() {
		item := InboxItem{Message: m}
		if m.Competition != 0 {
			item.CompetitionName = w.competitionName(m.Competition)
			_, item.Cup = w.cupIndex(m.Competition)
		}
		ref := competitions.SeasonRef{Competition: m.Competition, Season: competitions.Season(m.Season)}
		if m.Round != 0 {
			item.RoundName = w.RoundName(competitions.RoundRef{Season: ref, Round: competitions.Round(m.Round)})
		}
		if item.Cup && m.Kind == inbox.KindSeasonEnded && m.Position > 0 {
			item.Stage = w.cupStage(ref, m.Position)
		}
		if m.Opponent != 0 {
			item.OpponentLabel = w.teamLabel(m.Opponent)
		}
		if m.Champion != 0 {
			item.ChampionLabel = w.teamLabel(m.Champion)
		}
		if p, ok := w.registry.Player(m.Player); ok {
			item.PlayerName = p.FullName()
		}
		if l, ok := w.ClubLabel(m.Club); ok {
			item.ClubName = l.ClubName
		}
		out = append(out, item)
	}
	return out
}
