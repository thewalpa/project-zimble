package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/medical"
)

func seasonRef(comp ids.CompetitionID, season competitions.Season) competitions.SeasonRef {
	return competitions.SeasonRef{Competition: comp, Season: season}
}

// A career plays three seasons with stable identities: each season is a new
// draw of the same entrants a year on, fixture IDs never repeat, every
// fixture gets exactly one result, and history keeps each champion.
func TestCareerPlaysConsecutiveSeasons(t *testing.T) {
	w := newWorld(t, 42)
	initial := w.Snapshot()
	s1, s2, s3 := seasonRef(1, 1), seasonRef(1, 2), seasonRef(1, 3)

	playSeason(t, w)
	if w.leagues[0].season != s2 {
		t.Fatalf("current season %s after season 1", w.leagues[0].season)
	}
	first1, first2 := w.competitions.Rounds(s1)[0].Kickoff, w.competitions.Rounds(s2)[0].Kickoff
	if civil, _ := w.Calendar().Civil(first2); first2-first1 != 364*day || civil != (sim.CivilTime{Year: 2026, Month: 8, Day: 8, Hour: 15}) {
		t.Fatalf("season 2 starts %v (%d after season 1)", civil, first2-first1)
	}
	e1, _ := w.competitions.Entrants(s1)
	e2, _ := w.competitions.Entrants(s2)
	if !slices.Equal(e1, e2) {
		t.Fatal("entrants changed between seasons")
	}
	pairings := func(ref competitions.SeasonRef) (out [][3]uint64) {
		for _, f := range w.competitions.Fixtures(ref) {
			out = append(out, [3]uint64{uint64(f.Round), uint64(f.Home), uint64(f.Away)})
		}
		return out
	}
	if reflect.DeepEqual(pairings(s1), pairings(s2)) {
		t.Fatal("season 2 repeats season 1's draw")
	}
	if n := len(kickoffTasks(w)); n != 14 {
		t.Fatalf("%d kickoff tasks queued for season 2", n)
	}
	// Nine months of rest: everyone starts season 2 fully fit.
	mustContinue(t, w, first2-1)
	for id, c := range conditions(w) {
		if c != int(medical.MaxCondition) {
			t.Fatalf("player %d starts season 2 at condition %d", id, c)
		}
	}

	playSeason(t, w)
	if w.leagues[0].season != s3 {
		t.Fatalf("current season %s after season 2", w.leagues[0].season)
	}
	seen := map[ids.FixtureID]bool{}
	for i, ref := range []competitions.SeasonRef{s1, s2, s3} {
		fixtures := w.competitions.Fixtures(ref)
		if len(fixtures) != 56 {
			t.Fatalf("%s has %d fixtures", ref, len(fixtures))
		}
		for _, f := range fixtures {
			if seen[f.ID] || f.ID <= ids.FixtureID(56*i) || f.ID > ids.FixtureID(56*(i+1)) {
				t.Fatalf("fixture ID %d of %s repeated or out of sequence", f.ID, ref)
			}
			seen[f.ID] = true
			if _, done := w.competitions.Result(f.ID); done != (i < 2) {
				t.Fatalf("fixture %d of %s: result recorded = %v", f.ID, ref, done)
			}
		}
	}

	hist := w.History()
	if len(hist) != 3 || hist[2].Complete || hist[2].Champion != nil {
		t.Fatalf("history %+v", hist)
	}
	for i, ref := range []competitions.SeasonRef{s1, s2} {
		table, ok := w.Table(ref)
		if !ok || !table.Complete || hist[i].Season != ref || hist[i].Champion == nil || *hist[i].Champion != table.Rows[0].Label {
			t.Fatalf("season %s: history %+v, table top %+v", ref, hist[i], table.Rows[0].Label)
		}
		assertStandingsMatchResults(t, w.competitions.Results(ref), table.Rows)
	}
	if _, ok := w.Table(seasonRef(1, 9)); ok {
		t.Fatal("Table of a season that does not exist")
	}

	// Identities and generated content are untouched.
	final := w.Snapshot()
	if final.WorldFingerprint != initial.WorldFingerprint || !reflect.DeepEqual(final.Registry, initial.Registry) ||
		!reflect.DeepEqual(final.Players, initial.Players) || !reflect.DeepEqual(final.Employment, initial.Employment) {
		t.Fatal("a season transition changed identities or profiles")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The season ends at its last kickoff, but only after the last round is
// resolved: while that round awaits results, Continue does not run it.
func TestSeasonEndsAfterTheLastRoundIsResolved(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 13)
	ready := readyBatch(t, w)
	last := ready.At
	if again := mustContinue(t, w, last+30*day); !reflect.DeepEqual(again, ready) || w.leagues[0].season.Season != 1 {
		t.Fatal("the season ended while its last round awaited results")
	}
	resolveNow(t, w)
	rev := w.Revision()
	if res := mustContinue(t, w, last); res != (ReachedTarget{Now: last}) {
		t.Fatalf("Continue at the last kickoff = %#v", res)
	}
	if w.leagues[0].season.Season != 2 || w.Revision() != rev+1 || w.Now() != last {
		t.Fatalf("season %s, revision %d (was %d), now %d", w.leagues[0].season, w.Revision(), rev, w.Now())
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Saving in the off-season, or between the last result and the season end,
// continues exactly like never saving.
func TestSaveAroundSeasonEndContinuesIdentically(t *testing.T) {
	for name, stop := range map[string]func(*World){
		"before season end": func(w *World) { playBatches(t, w, 14) }, // last round resolved, season end queued
		"off-season":        func(w *World) { playSeason(t, w); mustContinue(t, w, w.Now()+100*day+7) },
	} {
		straight := newWorld(t, 42)
		stop(straight)
		loaded := roundTrip(t, straight)
		// playSeason stops after a season end; from "before season end" the
		// first call only runs it.
		next := func(w *World) []RoundsResolved {
			r := playSeason(t, w)
			if len(r) == 0 {
				r = playSeason(t, w)
			}
			return r
		}
		a, b := next(straight), next(loaded)
		if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(loaded.Snapshot(), straight.Snapshot()) {
			t.Fatalf("%s: the next season differs after a save", name)
		}
		if n := len(a); n != 14 || loaded.leagues[0].season.Season != 3 {
			t.Fatalf("%s: %d batches, now in %s", name, n, loaded.leagues[0].season)
		}
	}
}

// Leagues ending together roll over in one all-or-nothing cohort.
func TestSimultaneousSeasonEndsAreAtomic(t *testing.T) {
	w := twoLeagueWorld(t)
	playBatches(t, w, 14)
	var victim sim.Task
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskSeasonEnd && w.seasonEnds[task.PayloadID].Competition == 2 {
			victim = task
		}
	}
	ref := w.seasonEnds[victim.PayloadID]
	delete(w.seasonEnds, victim.PayloadID)
	before := snapshot(w)
	seasons := w.competitions.Seasons()
	if _, err := w.Continue(w.Now() + day); err == nil {
		t.Fatal("season end succeeded with a missing payload")
	}
	if !reflect.DeepEqual(snapshot(w), before) || !reflect.DeepEqual(w.competitions.Seasons(), seasons) || w.leagues[0].season.Season != 1 {
		t.Fatal("a failed season-end cohort partially applied")
	}
	w.seasonEnds[victim.PayloadID] = ref
	mustContinue(t, w, w.Now()+day)
	for i, comp := range []ids.CompetitionID{1, 2} {
		f := w.competitions.Fixtures(seasonRef(comp, 2))
		lo, hi := ids.FixtureID(113+56*i), ids.FixtureID(168+56*i)
		if w.leagues[i].season != seasonRef(comp, 2) || f[0].ID < lo || f[55].ID > hi {
			t.Fatalf("competition %d: season %s, fixtures %d..%d, want within %d..%d", comp, w.leagues[i].season, f[0].ID, f[55].ID, lo, hi)
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSeasonEndRejectsAnIncompleteSeason(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 5)
	var cohort []sim.Task
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskSeasonEnd {
			cohort = append(cohort, task)
		}
	}
	before := w.Snapshot()
	if err := w.endSeasons(cohort[0].DueAt, cohort); err == nil {
		t.Fatal("ended a season with rounds left")
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("a rejected season end changed the world")
	}
}

func TestRestoreRejectsInvalidSeasonState(t *testing.T) {
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playSeason(t, w)
		playBatches(t, w, 2)
		return w.Snapshot()
	}
	seasonEndTask := func(s *WorldSnapshot) *sim.Task {
		for i, task := range s.Scheduler.Tasks {
			if task.Kind == taskSeasonEnd {
				return &s.Scheduler.Tasks[i]
			}
		}
		t.Fatal("no season-end task")
		return nil
	}
	cases := map[string]func(*WorldSnapshot){
		"season-end payload missing": func(s *WorldSnapshot) { s.SeasonEndPayloads = nil },
		"season-end payload for season 1": func(s *WorldSnapshot) {
			s.SeasonEndPayloads[0].Season = seasonRef(1, 1)
		},
		"season-end payload unknown season": func(s *WorldSnapshot) { s.SeasonEndPayloads[0].Season = seasonRef(1, 7) },
		"season-end payload ID reused": func(s *WorldSnapshot) {
			s.SeasonEndPayloads[0].ID = s.KickoffPayloads[0].ID
			seasonEndTask(s).PayloadID = s.KickoffPayloads[0].ID
		},
		"season-end task early":       func(s *WorldSnapshot) { seasonEndTask(s).DueAt -= day },
		"season-end task wrong phase": func(s *WorldSnapshot) { seasonEndTask(s).Phase = sim.PhaseFixtures },
		"season-end task missing": func(s *WorldSnapshot) {
			var kept []sim.Task
			for _, task := range s.Scheduler.Tasks {
				if task.Kind != taskSeasonEnd {
					kept = append(kept, task)
				}
			}
			s.Scheduler.Tasks = kept
		},
		"season 1 missing": func(s *WorldSnapshot) {
			s.Competitions.Seasons = s.Competitions.Seasons[1:]
			s.ResolveCommands = nil
		},
		"league back in season 1": func(s *WorldSnapshot) {
			s.Leagues[0].Season = seasonRef(1, 1)
		},
		"season interval edited": func(s *WorldSnapshot) {
			s.Leagues[0].Definition.SeasonInterval += sim.Day
			s.ContentFingerprint = contentFingerprint(s.Content, []content.League{s.Leagues[0].Definition})
		},
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
