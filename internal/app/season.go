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
	"github.com/thewalpa/project-zimble/internal/finance"
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

// endSeasons closes every season in the cohort and creates what follows:
//
//   - A linked league season's end runs the promotion play-offs over each of
//     its links (competitions.FormatTies): one tie per place, the bottom of
//     Upper at home to the top of Lower (competitions.PlayoffPairings), one
//     round after competitions.PlayoffDelay. The linked leagues end in one
//     cohort. The winners cross the link, the losers stay, and the next
//     league seasons follow only once every play-off of that edition is
//     decided (competitions.NextEntrantsAfterPlayoffs) — or immediately,
//     from the plain swap, in a world without links.
//   - A league with no link just keeps its teams into its next season, which
//     starts in the week of its first kickoff's anniversary
//     (competitions.SeasonKickoff), whether or not links exist elsewhere.
//   - A cup edition's bracket seeds the qualifying seasons' final rankings
//     (content.Cup.Seeding), and it is played midweek during their next
//     season (see cupKickoffs). It is drawn once its qualifying leagues have
//     moved past the season it is drawn from (see cupEditionDue): with
//     play-offs, after they are decided. The first season has no edition to
//     play.
//   - An ending cup or play-off edition just closes.
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
		league               int // index into w.leagues, or -1 when no league season follows here
		task                 sim.TaskID
		payload              sim.PayloadID
		ref                  competitions.SeasonRef
		next                 competitions.SeasonRef // the league's next season; zero otherwise
		first                sim.GameInstant        // next's first kickoff
		prizeStart, prizeEnd int                    // range in the cohort's finance plan
	}
	var prizePostings []finance.Posting
	var ends []ending
	var created []ending                  // next seasons the play-offs decide
	var editions []competitions.SeasonRef // new play-off and cup editions to schedule
	var specs []competitions.NewSeason
	rankings := map[ids.CompetitionID][]ids.TeamID{} // ended leagues' final rankings
	endedSeason := map[ids.CompetitionID]competitions.Season{}
	links := w.movementLinks()
	linked := map[ids.CompetitionID]bool{}
	for _, l := range links {
		linked[l.Upper], linked[l.Lower] = true, true
	}
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
			postings, err := w.prizePostings(ref)
			if err != nil {
				return err
			}
			start := len(prizePostings)
			prizePostings = append(prizePostings, postings...)
			ends = append(ends, ending{league: -1, task: t.ID, payload: t.PayloadID, ref: ref, prizeStart: start, prizeEnd: len(prizePostings)})
			continue
		}
		if _, ok := w.playoffLink(ref.Competition); ok {
			ends = append(ends, ending{league: -1, task: t.ID, payload: t.PayloadID, ref: ref})
			continue
		}
		li, ok := w.leagueIndex(ref.Competition)
		if !ok || w.leagues[li].season != ref {
			return fmt.Errorf("app: season-end task %d for %s, which is not a league's current season, a cup edition or a play-off", t.ID, ref)
		}
		rankings[ref.Competition] = w.competitions.Ranking(ref)
		endedSeason[ref.Competition] = ref.Season
		if linked[ref.Competition] {
			// Its next season waits for the play-offs below.
			ends = append(ends, ending{league: li, task: t.ID, payload: t.PayloadID, ref: ref})
			continue
		}
		next := competitions.SeasonRef{Competition: ref.Competition, Season: ref.Season + 1}
		first, err := competitions.SeasonKickoff(w.calendar, w.leagues[li].def.FirstKickoff, next.Season)
		if err != nil || first <= at {
			return fmt.Errorf("app: %s first kickoff %d is invalid or not after %d: %v", next, first, at, err)
		}
		ends = append(ends, ending{league: li, task: t.ID, payload: t.PayloadID, ref: ref, next: next, first: first})
	}

	// A league with no link keeps its teams; its next season follows now.
	if len(rankings) > 0 {
		kept, err := competitions.NextEntrants(rankings, nil)
		if err != nil {
			return fmt.Errorf("app: season end at %d: %w", at, err)
		}
		for _, e := range ends {
			if !e.next.Valid() {
				continue
			}
			specs = append(specs, competitions.NewSeason{
				Ref: e.next, Format: competitions.FormatLeague, Entrants: kept[e.ref.Competition],
				Timing: competitions.Timing{FirstKickoff: e.first, RoundInterval: w.leagues[e.league].def.RoundInterval},
			})
			advanced[e.ref.Competition] = e.next.Season
		}
	}

	// Linked leagues end together; each of their links plays off its places.
	for _, l := range links {
		nU, endedU := endedSeason[l.Upper]
		nL, endedL := endedSeason[l.Lower]
		switch {
		case endedU != endedL:
			return fmt.Errorf("app: season end at %d: link %+v needs both leagues to end together", at, l)
		case !endedU:
			continue
		case nU != nL:
			return fmt.Errorf("app: season end at %d: link %+v ends seasons %d and %d", at, l, nU, nL)
		}
		comp, ok := w.playoffCompetition(l)
		if !ok {
			return fmt.Errorf("app: link %+v has no play-off competition", l)
		}
		spec, err := w.playoffEdition(l, comp, nU, rankings)
		if err != nil {
			return err
		}
		if spec.Timing.FirstKickoff <= at {
			return fmt.Errorf("app: %s would kick off at %d, not after %d", spec.Ref, spec.Timing.FirstKickoff, at)
		}
		specs = append(specs, spec)
		editions = append(editions, spec.Ref)
	}

	// A play-off end just closes its edition until every play-off of its
	// group is decided (see linkGroups); then the group's leagues move on to
	// their next seasons from the decided movement.
	var decided []competitions.Season
	for _, e := range ends {
		if _, ok := w.playoffLink(e.ref.Competition); ok {
			decided = append(decided, e.ref.Season)
		}
	}
	slices.Sort(decided)
	for _, n := range slices.Compact(decided) {
		for _, g := range linkGroups(links) {
			if !w.groupPlayoffsDecided(g, n) {
				continue
			}
			entrants, err := w.playoffMovement(g, n)
			if err != nil {
				return fmt.Errorf("app: play-offs of season %d end at %d: %w", n, at, err)
			}
			for _, comp := range groupLeagues(g) {
				if advanced[comp] != n {
					continue
				}
				next := competitions.SeasonRef{Competition: comp, Season: n + 1}
				if _, exists := w.competitions.Entrants(next); exists {
					continue
				}
				li, _ := w.leagueIndex(comp)
				first, err := competitions.SeasonKickoff(w.calendar, w.leagues[li].def.FirstKickoff, next.Season)
				if err != nil || first <= at {
					return fmt.Errorf("app: %s first kickoff %d is invalid or not after %d: %v", next, first, at, err)
				}
				specs = append(specs, competitions.NewSeason{
					Ref: next, Format: competitions.FormatLeague, Entrants: entrants[comp],
					Timing: competitions.Timing{FirstKickoff: first, RoundInterval: w.leagues[li].def.RoundInterval},
				})
				advanced[comp] = next.Season
				created = append(created, ending{league: li, next: next})
			}
		}
	}

	for _, c := range w.cups {
		edition, due := w.cupEditionDue(c, advanced)
		if !due {
			continue
		}
		spec, err := w.cupEdition(c, edition)
		if err != nil {
			return err
		}
		if spec.Timing.Kickoffs[0] <= at {
			return fmt.Errorf("app: %s would kick off at %d, not after %d", spec.Ref, spec.Timing.Kickoffs[0], at)
		}
		specs = append(specs, spec)
		editions = append(editions, spec.Ref)
	}
	if err := w.checkNoTeamClash(specs); err != nil {
		return fmt.Errorf("app: season end at %d: %w", at, err)
	}
	prizePlan, err := w.finance.Plan(at, prizePostings)
	if err != nil {
		return fmt.Errorf("app: season end at %d: cup prizes: %w", at, err)
	}
	if len(specs) > 0 {
		if err := w.competitions.CreateSeasons(w.seed, specs); err != nil {
			return fmt.Errorf("app: season end at %d: %w", at, err)
		}
	}

	// Committed. Scheduling and applying the planned prizes cannot fail.
	w.applyFinance(prizePlan)
	prizeEntries := prizePlan.Entries()
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
		if champion, ok := w.competitions.Champion(e.ref); ok {
			ended.Champion = champion
		}
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonEnded, SeasonEnded: ended})
		w.emitLedgerEntries(at, taskCause(e.task), prizeEntries[e.prizeStart:e.prizeEnd])
		if !e.next.Valid() {
			continue
		}
		w.leagues[e.league].season = e.next
		schedule(e.next)
		w.emit(at, taskCause(e.task), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started(e.next)})
	}
	for _, e := range created {
		w.leagues[e.league].season = e.next
		schedule(e.next)
		w.emit(at, taskCause(cohort[len(cohort)-1].ID), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started(e.next)})
	}
	// A new play-off or cup edition is caused by the cohort's last season
	// end: the one that completed its qualification.
	for _, ref := range editions {
		schedule(ref)
		w.emit(at, taskCause(cohort[len(cohort)-1].ID), events.Event{Kind: events.KindSeasonStarted, SeasonStarted: started(ref)})
	}
	return nil
}

// playoffLink returns the link a play-off competition decides, if any (see
// competitions.PlayoffCompetitions).
func (w *World) playoffLink(comp ids.CompetitionID) (competitions.Link, bool) {
	l, ok := competitions.PlayoffCompetitions(w.movementLinks())[comp]
	return l, ok
}

// playoffCompetition returns a link's play-off competition ID, if any.
func (w *World) playoffCompetition(l competitions.Link) (ids.CompetitionID, bool) {
	for comp, link := range competitions.PlayoffCompetitions(w.movementLinks()) {
		if link == l {
			return comp, true
		}
	}
	return 0, false
}

// linkGroups returns the promotion links in connected groups over shared
// leagues (a chained league is one link's lower and another's upper end).
// A group decides as one: every play-off of its edition is complete before
// any of its leagues moves on (see playoffMovement). Groups, and the links
// inside them, are in (Upper, Lower) order.
func linkGroups(links []competitions.Link) [][]competitions.Link {
	ordered := slices.Clone(links)
	slices.SortFunc(ordered, func(a, b competitions.Link) int {
		return cmp.Or(cmp.Compare(a.Upper, b.Upper), cmp.Compare(a.Lower, b.Lower))
	})
	sharesEnd := func(a, b competitions.Link) bool {
		return a.Upper == b.Upper || a.Upper == b.Lower || a.Lower == b.Upper || a.Lower == b.Lower
	}
	var groups [][]competitions.Link
	used := make([]bool, len(ordered))
	for i := range ordered {
		if used[i] {
			continue
		}
		used[i] = true
		g := []competitions.Link{ordered[i]}
		for queue := []int{i}; len(queue) > 0; {
			k := queue[0]
			queue = queue[1:]
			for j := range ordered {
				if !used[j] && sharesEnd(ordered[k], ordered[j]) {
					used[j] = true
					g = append(g, ordered[j])
					queue = append(queue, j)
				}
			}
		}
		slices.SortFunc(g, func(a, b competitions.Link) int {
			return cmp.Or(cmp.Compare(a.Upper, b.Upper), cmp.Compare(a.Lower, b.Lower))
		})
		groups = append(groups, g)
	}
	return groups
}

// groupLeagues returns the group's league competition IDs in ascending order.
func groupLeagues(g []competitions.Link) []ids.CompetitionID {
	seen := map[ids.CompetitionID]bool{}
	var out []ids.CompetitionID
	for _, l := range g {
		for _, c := range []ids.CompetitionID{l.Upper, l.Lower} {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	slices.Sort(out)
	return out
}

// groupPlayoffsDecided reports whether every play-off of the group's edition
// exists and is complete.
func (w *World) groupPlayoffsDecided(g []competitions.Link, season competitions.Season) bool {
	for _, l := range g {
		comp, ok := w.playoffCompetition(l)
		if !ok {
			return false
		}
		ref := competitions.SeasonRef{Competition: comp, Season: season}
		if _, exists := w.competitions.Entrants(ref); !exists || !w.competitions.SeasonCompleted(ref) {
			return false
		}
	}
	return true
}

// entrantsAfter replays one league's entrants for the season after n from
// the finished season's results: the plain swap (competitions.NextEntrants)
// for a league with no link, and its group's decided play-offs
// (competitions.NextEntrantsAfterPlayoffs) otherwise. An error names the
// broken invariant.
func (w *World) entrantsAfter(comp ids.CompetitionID, n competitions.Season) ([]ids.TeamID, error) {
	ref := competitions.SeasonRef{Competition: comp, Season: n}
	links := w.movementLinks()
	linked := false
	for _, l := range links {
		if l.Upper == comp || l.Lower == comp {
			linked = true
			break
		}
	}
	if linked {
		for _, g := range linkGroups(links) {
			if slices.Contains(groupLeagues(g), comp) {
				if !w.groupPlayoffsDecided(g, n) {
					return nil, fmt.Errorf("app: play-offs of season %d over %+v are not decided", n, g)
				}
				want, err := w.playoffMovement(g, n)
				if err != nil {
					return nil, err
				}
				return want[comp], nil
			}
		}
		return nil, fmt.Errorf("app: league %d is in no link group", comp)
	}
	kept, err := competitions.NextEntrants(map[ids.CompetitionID][]ids.TeamID{comp: w.competitions.Ranking(ref)}, nil)
	if err != nil {
		return nil, err
	}
	return kept[comp], nil
}

// playoffMovement returns the group's entrants for the season after the
// given one, from the finished seasons' rankings and the decided play-off
// outcomes (competitions.NextEntrantsAfterPlayoffs). Every play-off of the
// edition must be decided (see groupPlayoffsDecided).
func (w *World) playoffMovement(g []competitions.Link, season competitions.Season) (map[ids.CompetitionID][]ids.TeamID, error) {
	finals := map[ids.CompetitionID][]ids.TeamID{}
	for _, comp := range groupLeagues(g) {
		ref := competitions.SeasonRef{Competition: comp, Season: season}
		if !w.competitions.SeasonCompleted(ref) {
			return nil, fmt.Errorf("app: %s is not complete", ref)
		}
		finals[comp] = w.competitions.Ranking(ref)
	}
	winners, err := w.playoffWinners(g, season)
	if err != nil {
		return nil, err
	}
	return competitions.NextEntrantsAfterPlayoffs(finals, g, winners)
}

// playoffWinners returns each link's tie winners in tie order from its
// play-off edition's results, one per tie of competitions.PlayoffPairings.
func (w *World) playoffWinners(links []competitions.Link, season competitions.Season) (map[competitions.Link][]ids.TeamID, error) {
	out := map[competitions.Link][]ids.TeamID{}
	for _, l := range links {
		comp, ok := w.playoffCompetition(l)
		if !ok {
			return nil, fmt.Errorf("app: link %+v has no play-off competition", l)
		}
		ref := competitions.SeasonRef{Competition: comp, Season: season}
		if !w.competitions.SeasonCompleted(ref) {
			return nil, fmt.Errorf("app: %s is not complete", ref)
		}
		results := w.competitions.Results(ref) // fixture order is tie order
		winners := make([]ids.TeamID, len(results))
		for i, r := range results {
			winner, ok := r.Winner()
			if !ok {
				return nil, fmt.Errorf("app: %s fixture %d has no winner", ref, r.Fixture)
			}
			winners[i] = winner
		}
		out[l] = winners
	}
	return out, nil
}

// playoffEdition builds a link's promotion play-off for the finished season:
// one FormatTies round over competitions.PlayoffPairings, kicking off
// competitions.PlayoffDelay after the linked leagues' last kickoffs.
func (w *World) playoffEdition(l competitions.Link, comp ids.CompetitionID, season competitions.Season, rankings map[ids.CompetitionID][]ids.TeamID) (competitions.NewSeason, error) {
	ref := competitions.SeasonRef{Competition: comp, Season: season}
	pairs, err := competitions.PlayoffPairings(l, rankings)
	if err != nil {
		return competitions.NewSeason{}, fmt.Errorf("app: %s: %w", ref, err)
	}
	var entrants []ids.TeamID
	for _, p := range pairs {
		entrants = append(entrants, p[0], p[1])
	}
	var last sim.GameInstant
	for _, c := range []ids.CompetitionID{l.Upper, l.Lower} {
		s := competitions.SeasonRef{Competition: c, Season: season}
		rounds := w.competitions.Rounds(s)
		if len(rounds) == 0 {
			return competitions.NewSeason{}, fmt.Errorf("app: %s needs %s, which has no rounds", ref, s)
		}
		last = max(last, rounds[len(rounds)-1].Kickoff)
	}
	first, err := last.Add(competitions.PlayoffDelay)
	if err != nil {
		return competitions.NewSeason{}, fmt.Errorf("app: %s first kickoff: %w", ref, err)
	}
	return competitions.NewSeason{
		Ref: ref, Format: competitions.FormatTies, Entrants: entrants,
		Timing: competitions.Timing{FirstKickoff: first, RoundInterval: sim.Week},
	}, nil
}

// checkNoTeamClash rejects new seasons that would put a team in two fixtures
// at the same instant, against each other and against every fixture already
// in the store. A rest gap between a team's matches stays future work. A
// knockout season's later rounds are created from its results, so every
// entrant takes part in each of its rounds' kickoffs.
func (w *World) checkNoTeamClash(specs []competitions.NewSeason) error {
	type slot struct {
		team ids.TeamID
		at   sim.GameInstant
	}
	seen := map[slot]bool{}
	mark := func(team ids.TeamID, at sim.GameInstant, what string) error {
		s := slot{team, at}
		if seen[s] {
			return fmt.Errorf("app: team %d plays two fixtures at %d (%s)", team, at, what)
		}
		seen[s] = true
		return nil
	}
	for _, ref := range w.competitions.Seasons() {
		fixtures := w.competitions.Fixtures(ref)
		for _, f := range fixtures {
			if err := mark(f.Home, f.Kickoff, ref.String()); err != nil {
				return err
			}
			if err := mark(f.Away, f.Kickoff, ref.String()); err != nil {
				return err
			}
		}
		if format, _ := w.competitions.Format(ref); format != competitions.FormatKnockout {
			continue
		}
		// Later ties do not exist yet, but every entrant still in the cup
		// might play them. Reserve their kickoffs before creating another
		// competition's fixtures; eliminated teams no longer need a slot.
		eliminated := map[ids.TeamID]bool{}
		for _, result := range w.competitions.Results(ref) {
			if winner, ok := result.Winner(); ok {
				loser := result.Home
				if winner == loser {
					loser = result.Away
				}
				eliminated[loser] = true
			}
		}
		entrants, _ := w.competitions.Entrants(ref)
		for _, round := range w.competitions.Rounds(ref) {
			if len(round.Fixtures) > 0 {
				continue
			}
			for _, team := range entrants {
				if !eliminated[team] {
					if err := mark(team, round.Kickoff, ref.String()); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, spec := range specs {
		kickoffs, err := spec.Kickoffs()
		if err != nil {
			return err
		}
		for _, team := range spec.Entrants {
			for _, at := range kickoffs {
				if err := mark(team, at, spec.Ref.String()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Promotions returns the links between divisions: at a season's end each
// link's places are played off (competitions.PlayoffPairings), the winners
// taking a place in Upper next season and the losers one in Lower
// (competitions.NextEntrantsAfterPlayoffs). A table's promotion and
// relegation places follow from them. Read-only.
func (w *World) Promotions() []content.Promotion { return slices.Clone(w.promotions) }

// movementLinks returns the promotion links as competitions' rule.
func (w *World) movementLinks() []competitions.Link {
	links := make([]competitions.Link, len(w.promotions))
	for i, p := range w.promotions {
		links[i] = competitions.Link{Upper: p.Upper, Lower: p.Lower, Places: p.Places}
	}
	return links
}

// checkCupCalendars validates cups against the leagues' calendars: a cup's
// qualifying leagues share one (first kickoff and round interval), so their
// matchdays coincide, with rounds further apart than competitions.CupMidweek,
// and the shortest of their seasons has a matchday for every cup round to
// follow (competitions.CupMatchdays). See cupKickoffs.
func checkCupCalendars(leagues []content.League, cups []content.Cup) error {
	byID := map[ids.CompetitionID]content.League{}
	for _, l := range leagues {
		byID[l.ID] = l
	}
	for _, c := range cups {
		first := byID[c.Qualifiers[0].League]
		for _, q := range c.Qualifiers {
			if l := byID[q.League]; l.FirstKickoff != first.FirstKickoff || l.RoundInterval != first.RoundInterval {
				return fmt.Errorf("app: cup %d qualifying leagues %d and %d must share their calendar", c.ID, first.ID, l.ID)
			}
		}
		if first.RoundInterval <= competitions.CupMidweek {
			return fmt.Errorf("app: cup %d needs league rounds more than %d apart, not %d", c.ID, competitions.CupMidweek, first.RoundInterval)
		}
		if _, err := competitions.CupMatchdays(cupLeagueRounds(c, byID), c.Rounds()); err != nil {
			return fmt.Errorf("app: cup %d: %w", c.ID, err)
		}
	}
	return nil
}

// cupLeagueRounds is the number of matchdays a cup's rounds are spread over:
// the shortest of its qualifying leagues' seasons.
func cupLeagueRounds(c content.Cup, leagues map[ids.CompetitionID]content.League) int {
	n := 0
	for i, q := range c.Qualifiers {
		if r := leagues[q.League].Rounds(); i == 0 || r < n {
			n = r
		}
	}
	return n
}

// checkFootballYear checks that every football year fits inside its contract
// year: each league season kicks off after the summer transfer window has
// closed, and its last fixture (its last round, its promotion play-offs and
// the final of any cup it hosts, see cupKickoffs) is played before the
// player-year task on the eve of the next contract-year end. Seasons kick
// off within three days of their first kickoff's anniversary
// (competitions.SeasonKickoff) and a leap day can move the anniversary a day
// against the contract year, so the first season is checked with four days'
// margin each side, which covers every later one.
func (w *World) checkFootballYear(leagues []content.League) error {
	byID := map[ids.CompetitionID]content.League{}
	for _, l := range leagues {
		byID[l.ID] = l
	}
	linked := map[ids.CompetitionID]bool{}
	for _, p := range w.promotions {
		linked[p.Upper], linked[p.Lower] = true, true
	}
	span := map[ids.CompetitionID]sim.Duration{} // last fixture's offset from the season's first kickoff
	for _, l := range leagues {
		span[l.ID] = sim.Duration(l.Rounds()-1) * l.RoundInterval
		if linked[l.ID] {
			span[l.ID] += competitions.PlayoffDelay
		}
	}
	for _, c := range w.cups {
		final := sim.Duration(cupLeagueRounds(c, byID)-1)*byID[c.Qualifiers[0].League].RoundInterval + competitions.CupMidweek
		for _, q := range c.Qualifiers {
			span[q.League] = max(span[q.League], final)
		}
	}
	const margin = 4 * sim.Day
	for _, l := range leagues {
		first, err := w.calendar.Instant(l.FirstKickoff)
		if err != nil {
			return fmt.Errorf("app: league %d first kickoff: %w", l.ID, err)
		}
		earliest, latest := first-sim.GameInstant(margin), first+sim.GameInstant(margin)+sim.GameInstant(span[l.ID])
		_, closes, err := w.transferWindow(earliest)
		if err != nil {
			return fmt.Errorf("app: league %d: %w", l.ID, err)
		}
		yearEnd, err := w.contractYearEnd(earliest)
		if err != nil {
			return fmt.Errorf("app: league %d: %w", l.ID, err)
		}
		if earliest < closes || latest >= yearEnd-sim.GameInstant(sim.Day) {
			return fmt.Errorf("app: league %d's football year (%s to %s, give or take %d days) does not fit between the transfer window's close %s and the player year %s",
				l.ID, w.calendar.Format(first), w.calendar.Format(first+sim.GameInstant(span[l.ID])), margin/sim.Day, w.calendar.Format(closes), w.calendar.Format(yearEnd-sim.GameInstant(sim.Day)))
		}
	}
	return nil
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
		if u.FirstKickoff != l.FirstKickoff || u.RoundInterval != l.RoundInterval {
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
// must be complete: the bracket from their final rankings, played during
// their next season (cupKickoffs).
func (w *World) cupEdition(c content.Cup, edition competitions.Season) (competitions.NewSeason, error) {
	ref := competitions.SeasonRef{Competition: c.ID, Season: edition}
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
	}
	var bracket []ids.TeamID
	for _, seed := range c.Seeding() {
		bracket = append(bracket, rankings[seed[0]][seed[1]-1])
	}
	kickoffs, err := w.cupKickoffs(c, edition)
	if err != nil {
		return competitions.NewSeason{}, fmt.Errorf("app: %s: %w", ref, err)
	}
	return competitions.NewSeason{
		Ref: ref, Format: competitions.FormatKnockout, Entrants: bracket,
		Timing: competitions.Timing{Kickoffs: kickoffs},
	}, nil
}

// cupKickoffs returns the round kickoffs of a cup's edition, which is drawn
// from its qualifying leagues' season n and played during their season n+1:
// each round competitions.CupMidweek after the matchday
// competitions.CupMatchdays picks, so the final follows the last matchday.
// The qualifying leagues share one calendar (checkCupCalendars), so the
// matchdays come from their definitions, whether or not season n+1 exists.
func (w *World) cupKickoffs(c content.Cup, edition competitions.Season) ([]sim.GameInstant, error) {
	byID := map[ids.CompetitionID]content.League{}
	for _, l := range w.leagues {
		byID[l.def.ID] = l.def
	}
	league := byID[c.Qualifiers[0].League]
	matchdays, err := competitions.CupMatchdays(cupLeagueRounds(c, byID), c.Rounds())
	if err != nil {
		return nil, err
	}
	first, err := competitions.SeasonKickoff(w.calendar, league.FirstKickoff, edition+1)
	if err != nil {
		return nil, err
	}
	out := make([]sim.GameInstant, len(matchdays))
	for i, m := range matchdays {
		if out[i], err = first.Add(sim.Duration(m-1)*league.RoundInterval + competitions.CupMidweek); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// validateSeasons checks each competition's seasons and season-end tasks:
//
//   - every competition season belongs to a league, a cup or a link's
//     play-off, in its format;
//   - a league's seasons in the store are exactly 1..current; every earlier
//     season is complete and is followed by the entrants its movement
//     derives: the plain swap (competitions.NextEntrants) for a league with
//     no link, and otherwise its group's decided play-offs
//     (competitions.NextEntrantsAfterPlayoffs). A linked league's finished
//     season may still be current while its group's play-offs are undecided.
//     Each season starts at its competitions.SeasonKickoff;
//   - a link's play-off editions exist exactly for the finished seasons whose
//     season ends have run, each the one playoffEdition builds. Once every
//     play-off of a group's edition is decided and its ends have run, the
//     group's leagues have their next seasons (see linkGroups);
//   - a cup's editions are exactly 1..N, where edition n exists exactly when
//     every qualifying league has moved past season n; each edition is the
//     one cupEdition builds (bracket and every round's kickoff);
//   - a league has one season-end task for its current season while anything
//     of that season is still to run (its results or, for a linked league,
//     its end creating the play-offs); a cup or play-off edition that is not
//     yet complete has exactly one, a complete one at most one (its end may
//     still be due at this instant); every season-end payload is used by
//     exactly one task.
func (w *World) validateSeasons() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	links := w.movementLinks()
	playComps := competitions.PlayoffCompetitions(links)
	linked := map[ids.CompetitionID]bool{}
	for _, l := range links {
		linked[l.Upper], linked[l.Lower] = true, true
	}

	// Season-end tasks, before the rules that count them.
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
	// The rules below read the counts while the orphan check consumes
	// them, so they need a copy of the map before any delete.
	endTasks := maps.Clone(perSeason)

	perCompetition := map[ids.CompetitionID][]competitions.SeasonRef{}
	for _, ref := range w.competitions.Seasons() {
		format, _ := w.competitions.Format(ref)
		_, isLeague := w.leagueIndex(ref.Competition)
		_, isCup := w.cupIndex(ref.Competition)
		_, isPlayoff := playComps[ref.Competition]
		switch {
		case isLeague && format == competitions.FormatLeague, isCup && format == competitions.FormatKnockout, isPlayoff && format == competitions.FormatTies:
			perCompetition[ref.Competition] = append(perCompetition[ref.Competition], ref)
		default:
			fail("%s (%s) belongs to no league, cup or play-off of its format", ref, format)
		}
	}
	current := map[ids.CompetitionID]competitions.Season{}
	for _, l := range w.leagues {
		current[l.def.ID] = l.season.Season
	}

	// entrantsAfter replays one league's entrants for the season after n from
	// the finished season's results, the play-off outcomes included for a
	// linked league: its whole group decides together.
	entrantsAfter := w.entrantsAfter

	for _, l := range w.leagues {
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
				want, err := entrantsAfter(l.def.ID, ref.Season)
				if err != nil {
					fail("movement of league %d after %s: %v", l.def.ID, ref, err)
				} else if got, _ := w.competitions.Entrants(seasons[i+1]); !slices.Equal(got, want) {
					fail("%s entrants %v, want %v after %s", seasons[i+1], got, want, ref)
				}
			}
			want, err := competitions.SeasonKickoff(w.calendar, l.def.FirstKickoff, seasons[i+1].Season)
			if next := w.competitions.Rounds(seasons[i+1]); err != nil || len(next) == 0 || next[0].Kickoff != want {
				fail("%s does not start at %d: %v", seasons[i+1], want, err)
			}
		}
		// A finished current season waits for its end to run (any league) or
		// for its play-offs to decide (a linked league that has created
		// them); a linked season with its end still due has both to come.
		n := endTasks[l.season]
		complete := w.competitions.SeasonCompleted(l.season)
		switch {
		case !complete && n != 1:
			fail("%s has %d season-end tasks, want 1", l.season, n)
		case complete && !linked[l.def.ID] && n != 1:
			fail("%s has %d season-end tasks, want 1", l.season, n)
		case complete && linked[l.def.ID] && n > 1:
			fail("%s has %d season-end tasks, want at most 1", l.season, n)
		}
		delete(perSeason, l.season)
	}

	for _, l := range links {
		comp, _ := w.playoffCompetition(l)
		top := max(current[l.Upper], current[l.Lower])
		for _, ref := range perCompetition[comp] {
			top = max(top, ref.Season)
		}
		for n := competitions.Season(1); n <= top; n++ {
			ref := competitions.SeasonRef{Competition: comp, Season: n}
			_, exists := w.competitions.Entrants(ref)
			uRef := competitions.SeasonRef{Competition: l.Upper, Season: n}
			lRef := competitions.SeasonRef{Competition: l.Lower, Season: n}
			uOk := w.competitions.SeasonCompleted(uRef)
			lOk := w.competitions.SeasonCompleted(lRef)
			pending := endTasks[uRef] + endTasks[lRef]
			switch {
			case exists && (!uOk || !lOk):
				fail("%s exists but %s and %s are not both complete", ref, uRef, lRef)
			case exists && pending > 0:
				fail("%s exists while season ends of season %d are still due", ref, n)
			case !exists && uOk && lOk && pending == 0:
				fail("%s was not created: both linked seasons are finished and their ends have run", ref)
			case pending == 1:
				fail("link %+v has one season end of season %d still due, want both together", l, n)
			}
			if !exists {
				continue
			}
			if uOk && lOk {
				want, err := w.playoffEdition(l, comp, n, map[ids.CompetitionID][]ids.TeamID{
					l.Upper: w.competitions.Ranking(uRef),
					l.Lower: w.competitions.Ranking(lRef),
				})
				if err != nil {
					errs = append(errs, err)
				} else {
					entrants, _ := w.competitions.Entrants(ref)
					rounds := w.competitions.Rounds(ref)
					if !slices.Equal(entrants, want.Entrants) || len(rounds) == 0 || rounds[0].Kickoff != want.Timing.FirstKickoff {
						fail("%s has entrants %v from %d, want %v from %d", ref, entrants, rounds[0].Kickoff, want.Entrants, want.Timing.FirstKickoff)
					}
				}
			}
			if tasks := endTasks[ref]; tasks > 1 || (tasks == 0 && !w.competitions.SeasonCompleted(ref)) {
				fail("%s has %d season-end tasks", ref, tasks)
			}
			delete(perSeason, ref)
		}
	}

	// A group's decided play-offs must have moved its leagues on, and only
	// after its play-off ends have run.
	for _, g := range linkGroups(links) {
		comps := groupLeagues(g)
		top := competitions.Season(0)
		for _, c := range comps {
			top = max(top, current[c])
		}
		for n := competitions.Season(1); n <= top; n++ {
			endPending := false
			for _, l := range g {
				comp, _ := w.playoffCompetition(l)
				endPending = endPending || endTasks[competitions.SeasonRef{Competition: comp, Season: n}] > 0
			}
			decided := w.groupPlayoffsDecided(g, n)
			for _, c := range comps {
				ref := competitions.SeasonRef{Competition: c, Season: n}
				if !w.competitions.SeasonCompleted(ref) {
					continue
				}
				next := competitions.SeasonRef{Competition: c, Season: n + 1}
				_, exists := w.competitions.Entrants(next)
				switch {
				case endPending && exists:
					fail("%s exists but the play-offs of season %d have not ended", next, n)
				case decided && !endPending && !exists:
					fail("%s should exist: the play-offs of season %d are decided", next, n)
				}
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
			for _, q := range c.Qualifiers {
				if current[q.League] <= ref.Season {
					fail("%s was drawn before league %d moved past season %d", ref, q.League, ref.Season)
				}
			}
			want, err := w.cupEdition(c, ref.Season)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			entrants, _ := w.competitions.Entrants(ref)
			var kickoffs []sim.GameInstant
			for _, r := range w.competitions.Rounds(ref) {
				kickoffs = append(kickoffs, r.Kickoff)
			}
			if !slices.Equal(entrants, want.Entrants) || !slices.Equal(kickoffs, want.Timing.Kickoffs) {
				fail("%s has entrants %v at %v, want %v at %v", ref, entrants, kickoffs, want.Entrants, want.Timing.Kickoffs)
			}
			if tasks := endTasks[ref]; tasks > 1 || (tasks == 0 && !w.competitions.SeasonCompleted(ref)) {
				fail("%s has %d season-end tasks", ref, tasks)
			}
			delete(perSeason, ref)
		}
		if _, due := w.cupEditionDue(c, current); due {
			fail("cup %d edition %d is due but was not created", c.ID, len(editions)+1)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(w.seasonEnds)) {
		if uses[id] != 1 {
			fail("season-end payload %d is used by %d tasks", id, uses[id])
		}
		if ref := w.seasonEnds[id]; perSeason[ref] > 0 {
			fail("season-end payload %d is for %s, not a current season, cup edition or play-off", id, ref)
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

// History lists every league season, cup edition and promotion play-off in
// (competition, season) order, with the champion of each complete one (a
// play-off has none). It is derived from retained seasons and their results,
// not stored. Read-only.
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
