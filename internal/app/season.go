package app

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
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

// endSeasons closes every season in the cohort, creates each ending
// league's next season, and creates the next edition of every cup whose
// qualifying league seasons are now all over (see cupEditionDue):
//
//   - A league's next season starts SeasonInterval after the ending season's
//     first kickoff. Its entrants are the ending season's, moved along the
//     promotion links by every linked league's final ranking
//     (competitions.NextEntrants); linked leagues end in one cohort.
//   - A cup edition's bracket seeds the qualifying seasons' final rankings
//     (content.Cup.Seeding); its first round kicks off FirstRoundDelay after
//     the latest of their last kickoffs.
//   - An ending cup edition just closes.
//
// Everything is checked and every new season is created in one
// all-or-nothing competitions call before anything else changes; scheduling
// their tasks afterwards cannot fail (kickoffs are validated and lie after
// this instant).
//
// Ended seasons stay in the competitions store with their fixtures and
// results; their rankings and champions remain derivable (see History).
func (w *World) endSeasons(at sim.GameInstant, cohort []sim.Task) error {
	type ending struct {
		league  int // -1 for a cup edition
		task    sim.TaskID
		payload sim.PayloadID
		ref     competitions.SeasonRef
		next    competitions.SeasonRef // zero for a cup edition
	}
	var ends []ending
	var specs []competitions.NewSeason
	rankings := map[ids.CompetitionID][]ids.TeamID{}
	advanced := map[ids.CompetitionID]competitions.Season{} // leagues' current seasons once this cohort commits
	for _, l := range w.leagues {
		advanced[l.def.ID] = l.season.Season
	}
	for _, t := range cohort {
		ref, ok := w.seasonEnds[t.PayloadID]
		if !ok {
			return fmt.Errorf("app: task %d references missing season-end payload %d", t.ID, t.PayloadID)
		}
		if !w.competitions.SeasonCompleted(ref) {
			return fmt.Errorf("app: %s cannot end at %d: not every round has results", ref, at)
		}
		if _, ok := w.cupIndex(ref.Competition); ok {
			ends = append(ends, ending{league: -1, task: t.ID, payload: t.PayloadID, ref: ref})
			continue
		}
		li, ok := w.leagueIndex(ref.Competition)
		if !ok || w.leagues[li].season != ref {
			return fmt.Errorf("app: season-end task %d for %s, which is not a league's current season or a cup edition", t.ID, ref)
		}
		def := w.leagues[li].def
		rounds := w.competitions.Rounds(ref)
		first := rounds[0].Kickoff + sim.GameInstant(def.SeasonInterval)
		if !first.Valid() || first <= at {
			return fmt.Errorf("app: %s next first kickoff %d is invalid or not after %d", ref, first, at)
		}
		rankings[ref.Competition] = w.competitions.Ranking(ref)
		next := competitions.SeasonRef{Competition: ref.Competition, Season: ref.Season + 1}
		advanced[ref.Competition] = next.Season
		ends = append(ends, ending{league: li, task: t.ID, payload: t.PayloadID, ref: ref, next: next})
	}
	if len(rankings) > 0 {
		entrants, err := competitions.NextEntrants(rankings, w.movementLinks())
		if err != nil {
			return fmt.Errorf("app: season end at %d: %w", at, err)
		}
		for _, e := range ends {
			if e.league < 0 {
				continue
			}
			def := w.leagues[e.league].def
			first := w.competitions.Rounds(e.ref)[0].Kickoff + sim.GameInstant(def.SeasonInterval)
			specs = append(specs, competitions.NewSeason{
				Ref: e.next, Format: competitions.FormatLeague, Entrants: entrants[e.ref.Competition],
				Timing: competitions.Timing{FirstKickoff: first, RoundInterval: def.RoundInterval},
			})
		}
	}
	var editions []competitions.SeasonRef
	for _, c := range w.cups {
		edition, due := w.cupEditionDue(c, advanced)
		if !due {
			continue
		}
		spec, err := w.cupEdition(c, edition)
		if err != nil {
			return err
		}
		if spec.Timing.FirstKickoff <= at {
			return fmt.Errorf("app: %s would kick off at %d, not after %d", spec.Ref, spec.Timing.FirstKickoff, at)
		}
		specs = append(specs, spec)
		editions = append(editions, spec.Ref)
	}
	if len(specs) > 0 {
		if err := w.competitions.CreateSeasons(w.seed, specs); err != nil {
			return fmt.Errorf("app: season end at %d: %w", at, err)
		}
	}

	// Committed. Scheduling below only fails on a broken invariant.
	schedule := func(ref competitions.SeasonRef) {
		if err := w.scheduleRounds(ref); err != nil {
			panic(fmt.Sprintf("app: unreachable: %v", err))
		}
		if err := w.scheduleSeasonEnd(ref); err != nil {
			panic(fmt.Sprintf("app: unreachable: %v", err))
		}
	}
	started := func(ref competitions.SeasonRef) *events.SeasonStarted {
		entrants, _ := w.competitions.Entrants(ref)
		return &events.SeasonStarted{
			Competition: ref.Competition, Season: uint16(ref.Season),
			FirstKickoff: w.competitions.Rounds(ref)[0].Kickoff, Entrants: entrants,
		}
	}
	for _, e := range ends {
		delete(w.seasonEnds, e.payload)
		ended := &events.SeasonEnded{Competition: e.ref.Competition, Season: uint16(e.ref.Season), Ranking: w.competitions.Ranking(e.ref)}
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonEnded, SeasonEnded: ended})
		if e.league < 0 {
			continue
		}
		w.leagues[e.league].season = e.next
		schedule(e.next)
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started(e.next)})
	}
	// A new cup edition is caused by the cohort's last season end: the one
	// that completed its qualification.
	for _, ref := range editions {
		schedule(ref)
		w.emit(at, taskCause(cohort[len(cohort)-1].ID), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started(ref)})
	}
	return nil
}

// Promotions returns the links between divisions: at a season's end the
// bottom Places teams of Upper's final table swap leagues with the top
// Places of Lower's. A table's promotion and relegation places follow from
// them. Read-only.
func (w *World) Promotions() []content.Promotion { return slices.Clone(w.promotions) }

// movementLinks returns the promotion links as competitions' rule.
func (w *World) movementLinks() []competitions.Link {
	links := make([]competitions.Link, len(w.promotions))
	for i, p := range w.promotions {
		links[i] = competitions.Link{Upper: p.Upper, Lower: p.Lower, Places: p.Places}
	}
	return links
}

// checkPromotions validates links against the league definitions: the
// content rules, and that linked leagues share one calendar, so their
// seasons end in one cohort and movement can be decided from all rankings.
func checkPromotions(leagues []content.League, links []content.Promotion) error {
	if err := content.ValidatePromotions(leagues, links); err != nil {
		return err
	}
	byID := map[ids.CompetitionID]content.League{}
	for _, l := range leagues {
		byID[l.ID] = l
	}
	for _, p := range links {
		u, l := byID[p.Upper], byID[p.Lower]
		if u.FirstKickoff != l.FirstKickoff || u.RoundInterval != l.RoundInterval || u.SeasonInterval != l.SeasonInterval {
			return fmt.Errorf("app: linked leagues %d and %d must share their calendar", p.Upper, p.Lower)
		}
	}
	return nil
}

// cupEditionDue returns a cup's next edition and whether it is due: edition
// N follows the latest one (or is 1), and is due once every qualifying
// league has moved past season N (its season-end has run), given each
// league's current season.
func (w *World) cupEditionDue(c content.Cup, current map[ids.CompetitionID]competitions.Season) (competitions.Season, bool) {
	edition := w.latestEdition(c.ID) + 1
	for _, q := range c.Qualifiers {
		if current[q.League] <= edition {
			return edition, false
		}
	}
	return edition, true
}

// latestEdition returns a cup's highest edition in the store, or 0.
func (w *World) latestEdition(comp ids.CompetitionID) competitions.Season {
	var latest competitions.Season
	for _, ref := range w.competitions.Seasons() {
		if ref.Competition == comp {
			latest = max(latest, ref.Season)
		}
	}
	return latest
}

// cupEdition builds a cup edition from its qualifying league seasons, which
// must be complete: the bracket from their final rankings and the timing
// from their last kickoffs.
func (w *World) cupEdition(c content.Cup, edition competitions.Season) (competitions.NewSeason, error) {
	ref := competitions.SeasonRef{Competition: c.ID, Season: edition}
	var last sim.GameInstant
	rankings := make([][]ids.TeamID, len(c.Qualifiers))
	for i, q := range c.Qualifiers {
		season := competitions.SeasonRef{Competition: q.League, Season: edition}
		if !w.competitions.SeasonCompleted(season) {
			return competitions.NewSeason{}, fmt.Errorf("app: %s needs %s, which is not complete", ref, season)
		}
		rankings[i] = w.competitions.Ranking(season)
		if len(rankings[i]) < q.Places {
			return competitions.NewSeason{}, fmt.Errorf("app: %s has %d teams for %d places in %s", season, len(rankings[i]), q.Places, ref)
		}
		rounds := w.competitions.Rounds(season)
		last = max(last, rounds[len(rounds)-1].Kickoff)
	}
	var bracket []ids.TeamID
	for _, seed := range c.Seeding() {
		bracket = append(bracket, rankings[seed[0]][seed[1]-1])
	}
	first, err := last.Add(c.FirstRoundDelay)
	if err != nil {
		return competitions.NewSeason{}, fmt.Errorf("app: %s first kickoff: %w", ref, err)
	}
	return competitions.NewSeason{
		Ref: ref, Format: competitions.FormatKnockout, Entrants: bracket,
		Timing: competitions.Timing{FirstKickoff: first, RoundInterval: c.RoundInterval},
	}, nil
}

// validateSeasons checks each competition's seasons and season-end tasks:
//
//   - every competition season belongs to a league or a cup, in its format;
//   - a league's seasons in the store are exactly 1..current; every earlier
//     season is complete and is followed by the entrants NextEntrants
//     derives from every league's final ranking of it; each season starts
//     SeasonInterval after the previous one;
//   - a cup's editions are exactly 1..N, where edition n exists exactly when
//     every qualifying league has moved past season n; each edition is the
//     one cupEdition builds (bracket and timing);
//   - each league has exactly one season-end task: for its current season,
//     due at that season's last kickoff in the Consequences phase; each cup
//     edition not yet complete has exactly one, a complete one at most one
//     (its end may still be due at this instant); every season-end payload is
//     used by exactly one task.
func (w *World) validateSeasons() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	perCompetition := map[ids.CompetitionID][]competitions.SeasonRef{}
	for _, ref := range w.competitions.Seasons() {
		format, _ := w.competitions.Format(ref)
		_, isLeague := w.leagueIndex(ref.Competition)
		_, isCup := w.cupIndex(ref.Competition)
		switch {
		case isLeague && format == competitions.FormatLeague, isCup && format == competitions.FormatKnockout:
			perCompetition[ref.Competition] = append(perCompetition[ref.Competition], ref)
		default:
			fail("%s (%s) belongs to no league or cup of its format", ref, format)
		}
	}
	current := map[ids.CompetitionID]competitions.Season{}
	moved := map[competitions.Season]map[ids.CompetitionID][]ids.TeamID{} // NextEntrants after each season number
	for _, l := range w.leagues {
		current[l.def.ID] = l.season.Season
		seasons := perCompetition[l.def.ID]
		slices.SortFunc(seasons, func(a, b competitions.SeasonRef) int { return cmp.Compare(a.Season, b.Season) })
		if len(seasons) != int(l.season.Season) || len(seasons) == 0 || seasons[len(seasons)-1] != l.season {
			fail("league %d has seasons %v, want 1..%d", l.def.ID, seasons, l.season.Season)
			continue
		}
		for i, ref := range seasons[:len(seasons)-1] {
			if !w.competitions.SeasonCompleted(ref) {
				fail("%s is not complete but %s has begun", ref, l.season)
			} else {
				want, ok := moved[ref.Season]
				if !ok {
					rankings := map[ids.CompetitionID][]ids.TeamID{}
					for _, other := range w.leagues {
						o := competitions.SeasonRef{Competition: other.def.ID, Season: ref.Season}
						if w.competitions.SeasonCompleted(o) {
							rankings[other.def.ID] = w.competitions.Ranking(o)
						}
					}
					var err error
					if want, err = competitions.NextEntrants(rankings, w.movementLinks()); err != nil {
						fail("movement after season %d: %v", ref.Season, err)
					}
					moved[ref.Season] = want
				}
				if got, _ := w.competitions.Entrants(seasons[i+1]); !slices.Equal(got, want[l.def.ID]) {
					fail("%s entrants %v, want %v after %s", seasons[i+1], got, want[l.def.ID], ref)
				}
			}
			prev, next := w.competitions.Rounds(ref), w.competitions.Rounds(seasons[i+1])
			if len(prev) > 0 && len(next) > 0 && next[0].Kickoff != prev[0].Kickoff+sim.GameInstant(l.def.SeasonInterval) {
				fail("%s starts at %d, want %d after %s", seasons[i+1], next[0].Kickoff, l.def.SeasonInterval, ref)
			}
		}
	}
	for _, c := range w.cups {
		editions := perCompetition[c.ID]
		slices.SortFunc(editions, func(a, b competitions.SeasonRef) int { return cmp.Compare(a.Season, b.Season) })
		for i, ref := range editions {
			if ref.Season != competitions.Season(i+1) {
				fail("cup %d has editions %v, want 1..%d", c.ID, editions, len(editions))
				break
			}
			want, err := w.cupEdition(c, ref.Season)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			entrants, _ := w.competitions.Entrants(ref)
			rounds := w.competitions.Rounds(ref)
			if !slices.Equal(entrants, want.Entrants) || len(rounds) == 0 || rounds[0].Kickoff != want.Timing.FirstKickoff {
				fail("%s has entrants %v from %d, want %v from %d", ref, entrants, rounds[0].Kickoff, want.Entrants, want.Timing.FirstKickoff)
			}
			for j := 1; j < len(rounds); j++ {
				if rounds[j].Kickoff != rounds[j-1].Kickoff+sim.GameInstant(c.RoundInterval) {
					fail("%s round %d kicks off at %d, want %d after round %d", ref, j+1, rounds[j].Kickoff, c.RoundInterval, j)
				}
			}
		}
		if _, due := w.cupEditionDue(c, current); due {
			fail("cup %d edition %d is due but was not created", c.ID, len(editions)+1)
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
	for _, c := range w.cups {
		for _, ref := range perCompetition[c.ID] {
			if n := perSeason[ref]; n > 1 || (n == 0 && !w.competitions.SeasonCompleted(ref)) {
				fail("%s has %d season-end tasks", ref, n)
			}
			delete(perSeason, ref)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(w.seasonEnds)) {
		if uses[id] != 1 {
			fail("season-end payload %d is used by %d tasks", id, uses[id])
		}
		if ref := w.seasonEnds[id]; perSeason[ref] > 0 {
			fail("season-end payload %d is for %s, not a current season or cup edition", id, ref)
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

// SeasonRecord summarizes one competition season: a league season or a cup
// edition. Champion is set only for a complete one: the top of a league's
// final table, or a cup's final winner.
type SeasonRecord struct {
	Season          competitions.SeasonRef
	CompetitionName string
	Format          competitions.Format
	Complete        bool
	Champion        *TeamLabel
}

// History lists every league season and cup edition in (competition,
// season) order, with the champion of each complete one. It is derived from
// retained seasons and their results, not stored. Read-only.
func (w *World) History() []SeasonRecord {
	refs := w.competitions.Seasons()
	slices.SortFunc(refs, func(a, b competitions.SeasonRef) int {
		return cmp.Or(cmp.Compare(a.Competition, b.Competition), cmp.Compare(a.Season, b.Season))
	})
	var out []SeasonRecord
	for _, ref := range refs {
		format, _ := w.competitions.Format(ref)
		rec := SeasonRecord{Season: ref, CompetitionName: w.competitionName(ref.Competition), Format: format, Complete: w.competitions.SeasonCompleted(ref)}
		if champion, ok := w.competitions.Champion(ref); ok {
			label := w.teamLabel(champion)
			rec.Champion = &label
		}
		out = append(out, rec)
	}
	return out
}
