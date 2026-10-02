package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

var withReview = ContinueOptions{StopAtSeasonReview: true}

// toReview plays a managed world, resolving the user's batches, until
// ContinueWith reports a season review. The stop must come before the target.
func toReview(t *testing.T, w *World) SeasonReviewReady {
	t.Helper()
	until := w.Now() + 800*day
	for {
		res, err := w.ContinueWith(until, withReview)
		if err != nil {
			t.Fatal(err)
		}
		switch r := res.(type) {
		case FixtureRoundReady:
			resolveNow(t, w)
		case SeasonReviewReady:
			return r
		default:
			t.Fatalf("ContinueWith = %#v, want a season review", r)
		}
	}
}

func acknowledge(t *testing.T, w *World, ref competitions.SeasonRef) SeasonReviewAcknowledged {
	t.Helper()
	res, err := w.AcknowledgeSeasonReview(AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: ref})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The review comes after the last result and before the season ends; it grants
// no time and repeats unchanged until acknowledged.
func TestSeasonReviewStopsBeforeTheSeasonEnds(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ready := toReview(t, w)
	season := w.leagues[0].season
	if ready.Season != season || ready.Season.Season != 1 || !w.competitions.SeasonCompleted(ready.Season) || !w.seasonEndPending(ready.Season) {
		t.Fatalf("review %+v, league season %s", ready, season)
	}
	rounds := w.competitions.Rounds(season)
	if last := rounds[len(rounds)-1].Kickoff; ready.At != last || w.Now() != last {
		t.Fatalf("review at %d, now %d, last kickoff %d", ready.At, w.Now(), last)
	}
	if ready.NextContractYearEnd <= ready.At || ready.Revision != w.Revision() {
		t.Fatalf("contract-year end %d, revision %d (world %d)", ready.NextContractYearEnd, ready.Revision, w.Revision())
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	for _, until := range []sim.GameInstant{w.Now(), w.Now() + 30*day, w.Now() + 700*day} {
		again, err := w.ContinueWith(until, withReview)
		if err != nil || !reflect.DeepEqual(again, ready) {
			t.Fatalf("ContinueWith(%d) = %#v, %v; want the same review", until, again, err)
		}
	}
	if q, ok, err := w.SeasonReview(); err != nil || !ok || !reflect.DeepEqual(q, ready) {
		t.Fatalf("SeasonReview = %+v, %v, %v", q, ok, err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("repeating the stop changed the world")
	}

	ack := acknowledge(t, w, ready.Season)
	if ack.Season != ready.Season || ack.At != ready.At || ack.Revision != ready.Revision+1 {
		t.Fatalf("acknowledgement %+v", ack)
	}
	if _, ok, _ := w.SeasonReview(); ok {
		t.Fatal("the review is still open after its acknowledgement")
	}
	ev := w.Events()
	if last := ev[len(ev)-1]; last.Kind != events.KindSeasonReviewed || last.SeasonReviewed.Competition != ready.Season.Competition {
		t.Fatalf("last event %+v", last)
	}
	// Acknowledged, the world goes on: the season ends at the same instant.
	res, err := w.ContinueWith(ready.At, withReview)
	if err != nil {
		t.Fatal(err)
	}
	reached(t, res, ready.At)
	if w.seasonEndPending(ready.Season) {
		t.Fatal("the season did not end after the acknowledgement")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Without the option nothing stops and nothing needs acknowledging.
func TestContinueNeverStopsForTheReview(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ready := toReview(t, w)
	plain := userWorld(t, 42, userClub)
	playBatches(t, plain, 14)
	if plain.Now() != ready.At {
		t.Fatalf("plain world at %d, review at %d", plain.Now(), ready.At)
	}
	reached(t, mustContinue(t, plain, plain.Now()), ready.At)
	if plain.seasonEndPending(ready.Season) {
		t.Fatal("Continue stopped for the review")
	}
	if len(plain.Snapshot().ReviewCommands) != 0 {
		t.Fatal("Continue recorded a review")
	}
}

// An AI-only career has no review to stop for.
func TestCareerWithoutManagerHasNoReview(t *testing.T) {
	w := newWorld(t, 42)
	until := w.Now() + 400*day
	res, err := w.ContinueWith(until, withReview)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := res.(ReachedTarget); !ok || r.Now != until {
		t.Fatalf("ContinueWith = %#v", res)
	}
	if _, ok, _ := w.SeasonReview(); ok {
		t.Fatal("a career without a manager has a review")
	}
	_, err = w.AcknowledgeSeasonReview(AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: w.leagues[0].season})
	if !errors.Is(err, ErrNoUserClub) {
		t.Fatalf("err = %v", err)
	}
}

// A long call and many short ones meet the same review, and so does a loaded
// save; acknowledging and playing on gives the same world either way.
func TestSeasonReviewIsTheSameHoweverTheClockIsStepped(t *testing.T) {
	long := userWorld(t, 42, userClub)
	short := userWorld(t, 42, userClub)
	a := toReview(t, long)

	var b SeasonReviewReady
	for b.Season == (competitions.SeasonRef{}) {
		res, err := short.ContinueWith(short.Now()+3*day, withReview)
		if err != nil {
			t.Fatal(err)
		}
		switch r := res.(type) {
		case FixtureRoundReady:
			resolveNow(t, short)
		case SeasonReviewReady:
			b = r
		}
	}
	// Batches auto-resolved by each call, and the revision every call bumps,
	// differ with the stepping; the review itself does not.
	b.Resolved, a.Resolved, b.Revision, a.Revision = nil, nil, 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("long %+v, short %+v", a, b)
	}

	loaded := roundTrip(t, long)
	res, err := loaded.ContinueWith(loaded.Now()+365*day, withReview)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := long.SeasonReview()
	if got, ok := res.(SeasonReviewReady); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded world: %#v, want the review", res)
	}

	for _, w := range []*World{long, short, loaded} {
		acknowledge(t, w, a.Season)
		mustContinue(t, w, a.At+60*day)
	}
	if !reflect.DeepEqual(long.Snapshot(), loaded.Snapshot()) {
		t.Fatal("a world loaded at the review continued differently")
	}
	if !reflect.DeepEqual(long.Snapshot().Competitions, short.Snapshot().Competitions) ||
		!reflect.DeepEqual(long.Snapshot().Scheduler, short.Snapshot().Scheduler) {
		t.Fatal("stepping changed the competitions or the task queue")
	}
}

// Retries return the recorded result; everything else that is not a pending
// review is rejected and changes nothing.
func TestAcknowledgeSeasonReviewRules(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ref := w.leagues[0].season
	cmd := AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: ref}
	if _, err := w.AcknowledgeSeasonReview(cmd); !errors.Is(err, ErrNoSeasonReview) {
		t.Fatalf("mid-season: %v", err)
	}
	ready := toReview(t, w)
	other := ready.Season
	other.Season++
	for name, bad := range map[string]AcknowledgeSeasonReview{
		"another season":   {ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: other},
		"a zero command":   {ExpectedRevision: w.Revision(), Season: ready.Season},
		"a stale revision": {ID: w.NextCommandID(), ExpectedRevision: w.Revision() - 1, Season: ready.Season},
	} {
		before := w.Snapshot()
		if _, err := w.AcknowledgeSeasonReview(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if !reflect.DeepEqual(w.Snapshot(), before) {
			t.Errorf("%s: changed the world", name)
		}
	}
	cmd = AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: ready.Season}
	first, err := w.AcknowledgeSeasonReview(cmd)
	if err != nil {
		t.Fatal(err)
	}
	rev, journal := w.Revision(), len(w.Events())
	if retry, err := w.AcknowledgeSeasonReview(cmd); err != nil || retry != first || w.Revision() != rev || len(w.Events()) != journal {
		t.Fatalf("retry = %+v, %v; revision %d (was %d)", retry, err, w.Revision(), rev)
	}
	reused := cmd
	reused.Season = other
	if _, err := w.AcknowledgeSeasonReview(reused); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused command ID: %v", err)
	}
	again := AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: ready.Season}
	if _, err := w.AcknowledgeSeasonReview(again); !errors.Is(err, ErrNoSeasonReview) {
		t.Fatalf("second acknowledgement: %v", err)
	}
	// A retry still returns the record once the season has ended.
	mustContinue(t, w, ready.At)
	if retry, err := w.AcknowledgeSeasonReview(cmd); err != nil || retry != first {
		t.Fatalf("retry after the season ended = %+v, %v", retry, err)
	}
	if _, err := w.AcknowledgeSeasonReview(AcknowledgeSeasonReview{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Season: ready.Season}); !errors.Is(err, ErrNoSeasonReview) {
		t.Fatalf("acknowledging an ended season: %v", err)
	}
}

// The next season's review follows the same rule, and a save keeps every
// acknowledgement.
func TestEverySeasonHasItsOwnReview(t *testing.T) {
	w := userWorld(t, 42, userClub)
	first := toReview(t, w)
	acknowledge(t, w, first.Season)
	w = roundTrip(t, w)
	var second SeasonReviewReady
	for second.Season == (competitions.SeasonRef{}) {
		res, err := w.ContinueWith(w.Now()+800*day, withReview)
		if err != nil {
			t.Fatal(err)
		}
		switch r := res.(type) {
		case FixtureRoundReady:
			resolveNow(t, w)
		case SeasonReviewReady:
			second = r
		default:
			t.Fatalf("ContinueWith = %#v", r)
		}
	}
	if second.Season.Season != 2 || second.Season.Competition != first.Season.Competition || second.At <= first.At {
		t.Fatalf("first %+v, second %+v", first, second)
	}
	// The cup edition drawn after season 1 plays through season 2.
	if len(second.OpenEditions) == 0 {
		t.Logf("no open edition at the end of season 2")
	}
	for _, ref := range second.OpenEditions {
		if w.competitions.SeasonCompleted(ref) || ref == second.Season {
			t.Fatalf("open edition %s", ref)
		}
	}
	acknowledge(t, w, second.Season)
	if got := len(w.Snapshot().ReviewCommands); got != 2 {
		t.Fatalf("%d acknowledgements saved", got)
	}
	roundTrip(t, w)
}

func TestRestoreRejectsInvalidSeasonReviews(t *testing.T) {
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		ready := toReview(t, w)
		acknowledge(t, w, ready.Season)
		return w.Snapshot()
	}
	cases := map[string]func(*WorldSnapshot){
		"season of no league": func(s *WorldSnapshot) {
			s.ReviewCommands[0].Request.Season.Competition = 99
			s.ReviewCommands[0].Result.Season.Competition = 99
		},
		"season without results": func(s *WorldSnapshot) {
			s.ReviewCommands[0].Request.Season.Season = 2
			s.ReviewCommands[0].Result.Season.Season = 2
		},
		"request and result differ": func(s *WorldSnapshot) { s.ReviewCommands[0].Result.Season.Season = 2 },
		"before the last round":     func(s *WorldSnapshot) { s.ReviewCommands[0].Result.At -= day },
		"after now":                 func(s *WorldSnapshot) { s.ReviewCommands[0].Result.At += day },
		"revision skipped":          func(s *WorldSnapshot) { s.ReviewCommands[0].Result.Revision++ },
		"command ID of another":     func(s *WorldSnapshot) { s.ReviewCommands[0].Result.Command++ },
		"acknowledged twice": func(s *WorldSnapshot) {
			dup := s.ReviewCommands[0]
			dup.Request.ID, dup.Result.Command = dup.Request.ID+1000, dup.Request.ID+1000
			s.ReviewCommands = append(s.ReviewCommands, dup)
		},
		"event without a record": func(s *WorldSnapshot) { s.ReviewCommands = nil },
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
