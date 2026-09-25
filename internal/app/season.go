package app

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
)

// SeasonEndPayload is a season-end task's payload: the season that ends.
type SeasonEndPayload struct {
	ID     sim.PayloadID
	Season competitions.SeasonRef
}

// scheduleSeasonEnd queues the task that closes ref and creates the next
// season. It is due at ref's last kickoff in the Consequences phase: the
// same instant as the last round, but it runs only once that round has been
// resolved, because Continue does not run tasks while a round awaits
// results. StableOrder is the competition ID.
func (w *World) scheduleSeasonEnd(ref competitions.SeasonRef) error {
	rounds := w.competitions.Rounds(ref)
	if len(rounds) == 0 {
		return fmt.Errorf("app: %s has no rounds", ref)
	}
	id := w.lastPayload + 1
	_, err := w.scheduler.Schedule(sim.TaskSpec{
		DueAt:       rounds[len(rounds)-1].Kickoff,
		Phase:       sim.PhaseConsequences,
		StableOrder: uint64(ref.Competition),
		Kind:        taskSeasonEnd,
		PayloadID:   id,
	})
	if err != nil {
		return fmt.Errorf("app: schedule end of %s: %w", ref, err)
	}
	w.lastPayload = id
	w.seasonEnds[id] = ref
	return nil
}

func (w *World) leagueIndex(comp ids.CompetitionID) (int, bool) {
	for i, l := range w.leagues {
		if l.def.ID == comp {
			return i, true
		}
	}
	return 0, false
}

// endSeasons closes every season in the cohort and creates each league's
// next season with the same entrants, its first kickoff SeasonInterval after
// the ending season's. Everything is checked and the new seasons are created
// in one all-or-nothing competitions call before anything else changes;
// scheduling their tasks afterwards cannot fail (kickoffs are validated and
// lie after this instant).
//
// The ending season stays in the competitions store with its fixtures and
// results; its standings and champion remain derivable (see History).
func (w *World) endSeasons(at sim.GameInstant, cohort []sim.Task) error {
	type ending struct {
		league  int
		task    sim.TaskID
		payload sim.PayloadID
		ref     competitions.SeasonRef
		next    competitions.SeasonRef
	}
	var ends []ending
	var specs []competitions.NewSeason
	for _, t := range cohort {
		ref, ok := w.seasonEnds[t.PayloadID]
		if !ok {
			return fmt.Errorf("app: task %d references missing season-end payload %d", t.ID, t.PayloadID)
		}
		li, ok := w.leagueIndex(ref.Competition)
		if !ok || w.leagues[li].season != ref {
			return fmt.Errorf("app: season-end task %d for %s, which is not a league's current season", t.ID, ref)
		}
		if !w.competitions.SeasonCompleted(ref) {
			return fmt.Errorf("app: %s cannot end at %d: not every round has results", ref, at)
		}
		def := w.leagues[li].def
		rounds := w.competitions.Rounds(ref)
		first := rounds[0].Kickoff + sim.GameInstant(def.SeasonInterval)
		if !first.Valid() || first <= at {
			return fmt.Errorf("app: %s next first kickoff %d is invalid or not after %d", ref, first, at)
		}
		entrants, _ := w.competitions.Entrants(ref)
		next := competitions.SeasonRef{Competition: ref.Competition, Season: ref.Season + 1}
		specs = append(specs, competitions.NewSeason{
			Ref: next, Entrants: entrants,
			Timing: competitions.Timing{FirstKickoff: first, RoundInterval: def.RoundInterval},
		})
		ends = append(ends, ending{league: li, task: t.ID, payload: t.PayloadID, ref: ref, next: next})
	}
	if err := w.competitions.CreateLeagueSeasons(w.seed, specs); err != nil {
		return fmt.Errorf("app: season end at %d: %w", at, err)
	}

	// Committed. Scheduling below only fails on a broken invariant.
	for _, e := range ends {
		delete(w.seasonEnds, e.payload)
		w.leagues[e.league].season = e.next
		if err := w.scheduleRounds(e.next); err != nil {
			panic(fmt.Sprintf("app: unreachable: %v", err))
		}
		if err := w.scheduleSeasonEnd(e.next); err != nil {
			panic(fmt.Sprintf("app: unreachable: %v", err))
		}
		ended := &events.SeasonEnded{Competition: e.ref.Competition, Season: uint16(e.ref.Season)}
		for _, st := range w.competitions.Standings(e.ref) {
			ended.Ranking = append(ended.Ranking, st.Team)
		}
		entrants, _ := w.competitions.Entrants(e.next)
		started := &events.SeasonStarted{
			Competition: e.next.Competition, Season: uint16(e.next.Season),
			FirstKickoff: w.competitions.Rounds(e.next)[0].Kickoff, Entrants: entrants,
		}
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonEnded, SeasonEnded: ended})
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started})
	}
	return nil
}

// validateSeasons checks each league's seasons and season-end task:
//
//   - a league's seasons in the store are exactly 1..current; every earlier
//     season is complete and has the current season's entrants (entrants do
//     not change between seasons yet); each season starts SeasonInterval
//     after the previous one;
//   - every competition season belongs to a league;
//   - each league has exactly one season-end task: for its current season,
//     due at that season's last kickoff in the Consequences phase, and every
//     season-end payload is used by exactly one task.
func (w *World) validateSeasons() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	perLeague := map[ids.CompetitionID][]competitions.SeasonRef{}
	for _, ref := range w.competitions.Seasons() {
		if _, ok := w.leagueIndex(ref.Competition); !ok {
			fail("%s belongs to no league", ref)
			continue
		}
		perLeague[ref.Competition] = append(perLeague[ref.Competition], ref)
	}
	for _, l := range w.leagues {
		seasons := perLeague[l.def.ID]
		slices.SortFunc(seasons, func(a, b competitions.SeasonRef) int { return cmp.Compare(a.Season, b.Season) })
		if len(seasons) != int(l.season.Season) || len(seasons) == 0 || seasons[len(seasons)-1] != l.season {
			fail("league %d has seasons %v, want 1..%d", l.def.ID, seasons, l.season.Season)
			continue
		}
		current, _ := w.competitions.Entrants(l.season)
		for i, ref := range seasons[:len(seasons)-1] {
			if !w.competitions.SeasonCompleted(ref) {
				fail("%s is not complete but %s has begun", ref, l.season)
			}
			if entrants, _ := w.competitions.Entrants(ref); !slices.Equal(entrants, current) {
				fail("%s entrants differ from %s", ref, l.season)
			}
			prev, next := w.competitions.Rounds(ref), w.competitions.Rounds(seasons[i+1])
			if len(prev) > 0 && len(next) > 0 && next[0].Kickoff != prev[0].Kickoff+sim.GameInstant(l.def.SeasonInterval) {
				fail("%s starts at %d, want %d after %s", seasons[i+1], next[0].Kickoff, l.def.SeasonInterval, ref)
			}
		}
	}

	uses := map[sim.PayloadID]int{}
	perSeason := map[competitions.SeasonRef]int{}
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskSeasonEnd {
			continue
		}
		uses[t.PayloadID]++
		ref, ok := w.seasonEnds[t.PayloadID]
		if !ok {
			fail("season-end task %d references missing payload %d", t.ID, t.PayloadID)
			continue
		}
		perSeason[ref]++
		rounds := w.competitions.Rounds(ref)
		if len(rounds) == 0 || t.DueAt != rounds[len(rounds)-1].Kickoff || t.Phase != sim.PhaseConsequences || t.StableOrder != uint64(ref.Competition) {
			fail("season-end task %d for %s due %d phase %d order %d", t.ID, ref, t.DueAt, t.Phase, t.StableOrder)
		}
	}
	for _, l := range w.leagues {
		if perSeason[l.season] != 1 {
			fail("%s has %d season-end tasks, want 1", l.season, perSeason[l.season])
		}
		delete(perSeason, l.season)
	}
	for _, id := range slices.Sorted(maps.Keys(w.seasonEnds)) {
		if uses[id] != 1 {
			fail("season-end payload %d is used by %d tasks", id, uses[id])
		}
		if ref := w.seasonEnds[id]; perSeason[ref] > 0 {
			fail("season-end payload %d is for %s, not a current season", id, ref)
		}
	}
	return errs
}

// Table returns the standings of any season of a league, derived from its
// official results. Read-only.
func (w *World) Table(ref competitions.SeasonRef) (Table, bool) {
	li, ok := w.leagueIndex(ref.Competition)
	if !ok {
		return Table{}, false
	}
	if _, ok := w.competitions.Entrants(ref); !ok {
		return Table{}, false
	}
	return w.table(w.leagues[li].def.Name, ref), true
}

// SeasonRecord summarizes one league season. Champion is set only for a
// complete season: the top of its derived final table.
type SeasonRecord struct {
	Season          competitions.SeasonRef
	CompetitionName string
	Complete        bool
	Champion        *TeamLabel
}

// History lists every league season in (competition, season) order, with
// the champion of each complete one. It is derived from retained seasons and
// their results, not stored. Read-only.
func (w *World) History() []SeasonRecord {
	refs := w.competitions.Seasons()
	slices.SortFunc(refs, func(a, b competitions.SeasonRef) int {
		return cmp.Or(cmp.Compare(a.Competition, b.Competition), cmp.Compare(a.Season, b.Season))
	})
	var out []SeasonRecord
	for _, ref := range refs {
		t, ok := w.Table(ref)
		if !ok {
			continue
		}
		rec := SeasonRecord{Season: ref, CompetitionName: t.CompetitionName, Complete: t.Complete}
		if t.Complete && len(t.Rows) > 0 {
			champion := t.Rows[0].Label
			rec.Champion = &champion
		}
		out = append(out, rec)
	}
	return out
}
