package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// The season review is the manager's pause between the last league match of
// a season and the season's end. A season's end is due at its last kickoff
// (see scheduleSeasonEnd), so with no pause the next season exists, and the
// table is history, the moment the final whistle is recorded.
//
// A caller that wants the pause asks for it: ContinueWith with
// ContinueOptions.StopAtSeasonReview returns SeasonReviewReady instead of
// running the user's league season-end cohort, and keeps returning it until
// the manager acknowledges the review (AcknowledgeSeasonReview). A caller
// that does not ask (Continue, every AI-only career) never meets the stop and
// never needs the acknowledgement, so the pause grants no simulated time: the
// clock stays at the final kickoff for as long as the review is open.
//
// The review is a state, not a notification: SeasonReview reports it from
// the world alone, so a save made while it is open shows it again once
// loaded. Acknowledgements are commands and so are saved in the command log;
// the set of reviewed seasons is derived from them.

// ErrNoSeasonReview is returned by AcknowledgeSeasonReview for a season that
// has no review awaiting: not the user's league season, rounds still to
// play, already acknowledged or already ended.
var ErrNoSeasonReview = errors.New("app: no season review awaits acknowledgement")

// errSeasonReview makes a cohort handler leave the season-end cohort queued
// so that ContinueWith can report the review. It never leaves the package.
var errSeasonReview = errors.New("app: season review")

// ContinueOptions are the optional stops of ContinueWith, which are off in
// Continue.
type ContinueOptions struct {
	// StopAtSeasonReview stops before the user's league season ends, once its
	// last round has results, until AcknowledgeSeasonReview.
	StopAtSeasonReview bool
}

// SeasonReviewReady means the user's league season has every result and has
// not ended yet. The world does not advance past it until the review is
// acknowledged; Revision is the value to pass as the acknowledgement's
// ExpectedRevision.
//
// At is the final kickoff: the contract-year end (NextContractYearEnd) and
// the next season's first kickoff both lie ahead, so a review never promises
// a renewal that has already expired. OpenEditions lists the other
// competitions the user's team is entered in that still have rounds to play,
// such as a cup edition: the football year is not over while any is open,
// and they continue after the league season ends.
type SeasonReviewReady struct {
	At                  sim.GameInstant
	Revision            Revision
	Season              competitions.SeasonRef
	OpenEditions        []competitions.SeasonRef // ascending
	NextContractYearEnd sim.GameInstant
	Resolved            []BatchResolved
}

func (SeasonReviewReady) continueResult() {}

// AcknowledgeSeasonReview records that the manager has seen the review of
// Season, so that ContinueWith may end it. Season is the SeasonReviewReady's.
type AcknowledgeSeasonReview struct {
	ID               CommandID
	ExpectedRevision Revision
	Season           competitions.SeasonRef
}

// SeasonReviewAcknowledged is the recorded acknowledgement, returned
// unchanged on retries.
type SeasonReviewAcknowledged struct {
	Command  CommandID
	Revision Revision
	At       sim.GameInstant
	Season   competitions.SeasonRef
}

type SeasonReviewRecord struct {
	Request AcknowledgeSeasonReview
	Result  SeasonReviewAcknowledged
}

// userLeagueSeason returns the current season of the league the user's team
// plays in.
func (w *World) userLeagueSeason() (competitions.SeasonRef, bool) {
	team, ok := w.userTeam()
	if !ok {
		return competitions.SeasonRef{}, false
	}
	for _, l := range w.leagues {
		if entrants, _ := w.competitions.Entrants(l.season); slices.Contains(entrants, team) {
			return l.season, true
		}
	}
	return competitions.SeasonRef{}, false
}

// seasonEndPending reports whether ref's season-end task is still queued.
func (w *World) seasonEndPending(ref competitions.SeasonRef) bool {
	for _, r := range w.seasonEnds {
		if r == ref {
			return true
		}
	}
	return false
}

// reviewDue returns the user's league season when its review awaits: every
// round has results, it has not ended, and it is not acknowledged.
func (w *World) reviewDue() (competitions.SeasonRef, bool) {
	ref, ok := w.userLeagueSeason()
	if !ok || w.reviewed[ref] || !w.competitions.SeasonCompleted(ref) || !w.seasonEndPending(ref) {
		return competitions.SeasonRef{}, false
	}
	return ref, true
}

// endsUnreviewedSeason reports whether a season-end cohort closes the user's
// league season before its review is acknowledged.
func (w *World) endsUnreviewedSeason(cohort []sim.Task) bool {
	ref, ok := w.reviewDue()
	if !ok {
		return false
	}
	for _, t := range cohort {
		if w.seasonEnds[t.PayloadID] == ref {
			return true
		}
	}
	return false
}

// SeasonReview reports the season review awaiting acknowledgement, if any.
// It is a read-only query: it never runs tasks or moves the clock.
func (w *World) SeasonReview() (SeasonReviewReady, bool, error) {
	ref, ok := w.reviewDue()
	if !ok {
		return SeasonReviewReady{}, false, nil
	}
	end, err := w.contractYearEnd(w.Now())
	if err != nil {
		return SeasonReviewReady{}, false, fmt.Errorf("app: season review: %w", err)
	}
	ready := SeasonReviewReady{At: w.Now(), Revision: w.revision, Season: ref, NextContractYearEnd: end}
	team, _ := w.userTeam()
	for _, other := range w.competitions.Seasons() {
		if other == ref || w.competitions.SeasonCompleted(other) {
			continue
		}
		if entrants, _ := w.competitions.Entrants(other); slices.Contains(entrants, team) {
			ready.OpenEditions = append(ready.OpenEditions, other)
		}
	}
	return ready, true, nil
}

// AcknowledgeSeasonReview commits the acknowledgement and its event. Retrying
// a recorded command returns its result and emits nothing.
func (w *World) AcknowledgeSeasonReview(cmd AcknowledgeSeasonReview) (SeasonReviewAcknowledged, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.review != nil && r.review.Request == cmd })
	if err != nil {
		return SeasonReviewAcknowledged{}, err
	}
	if retry {
		return rec.review.Result, nil
	}
	if ref, ok := w.reviewDue(); !ok || ref != cmd.Season {
		return SeasonReviewAcknowledged{}, fmt.Errorf("%w: %s", ErrNoSeasonReview, cmd.Season)
	}
	res := SeasonReviewAcknowledged{Command: cmd.ID, Revision: w.revision + 1, At: w.Now(), Season: cmd.Season}
	w.commands[cmd.ID] = commandRecord{review: &SeasonReviewRecord{Request: cmd, Result: res}}
	w.reviewed[cmd.Season] = true
	w.revision = res.Revision
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindSeasonReviewed, SeasonReviewed: &events.SeasonReviewed{
		Competition: cmd.Season.Competition, Season: uint16(cmd.Season.Season),
	}})
	w.publish()
	return res, nil
}

// restoreSeasonReview validates a saved acknowledgement and marks its season
// reviewed. It runs after every module is restored.
func (w *World) restoreSeasonReview(c SeasonReviewRecord, revision Revision) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if q.ExpectedRevision >= r.Revision || r.Revision-q.ExpectedRevision != 1 || r.Revision > revision ||
		q.Season != r.Season || !q.Season.Valid() || r.At < 0 || r.At > w.Now() {
		return fmt.Errorf("invalid season review record %+v", c)
	}
	if _, ok := w.leagueIndex(q.Season.Competition); !ok {
		return fmt.Errorf("%s is not a league season", q.Season)
	}
	team, ok := w.userTeam()
	if !ok {
		return ErrNoUserClub
	}
	if entrants, _ := w.competitions.Entrants(q.Season); !slices.Contains(entrants, team) {
		return fmt.Errorf("%s is not the user team's league season", q.Season)
	}
	rounds := w.competitions.Rounds(q.Season)
	if !w.competitions.SeasonCompleted(q.Season) || len(rounds) == 0 || r.At < rounds[len(rounds)-1].Kickoff {
		return fmt.Errorf("%s was acknowledged at %d before its last round", q.Season, r.At)
	}
	if w.reviewed[q.Season] {
		return fmt.Errorf("%s is acknowledged twice", q.Season)
	}
	w.reviewed[q.Season] = true
	return nil
}
