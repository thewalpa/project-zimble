package app

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/worldgen"
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
	// The play-offs decide who moves between the divisions; the first
	// division keeps its size and takes exactly the entrants the decided
	// ties call for.
	e1, _ := w.competitions.Entrants(s1)
	e2, _ := w.competitions.Entrants(s2)
	want, err := w.entrantsAfter(w.leagues[0].def.ID, 1)
	if len(e2) != len(e1) || err != nil || !slices.Equal(e2, want) {
		t.Fatalf("season 2 entrants %v after %v, want %v (%v)", e2, e1, want, err)
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
	if n := len(kickoffTasks(w)); n != 56+3 { // all four leagues' season 2 and the cup edition played during it; the play-offs were played
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
	// Fixture IDs are never reused and each season's follow the previous
	// one's (the other leagues, the cup and the play-offs take IDs in
	// between).
	seen := map[ids.FixtureID]bool{}
	var last ids.FixtureID
	for i, ref := range []competitions.SeasonRef{s1, s2, s3} {
		fixtures := w.competitions.Fixtures(ref)
		if len(fixtures) != 56 {
			t.Fatalf("%s has %d fixtures", ref, len(fixtures))
		}
		first := last
		for _, f := range fixtures {
			if seen[f.ID] || f.ID <= first {
				t.Fatalf("fixture ID %d of %s repeated or out of sequence", f.ID, ref)
			}
			seen[f.ID] = true
			last = max(last, f.ID)
			if _, done := w.competitions.Result(f.ID); done != (i < 2) {
				t.Fatalf("fixture %d of %s: result recorded = %v", f.ID, ref, done)
			}
		}
	}

	// History in competition ID order: the two first divisions' three
	// seasons each, then the cup's editions (the first, played during
	// season 2 and won by its final's winner, and the second, drawn for
	// season 3), the second divisions' six, and the play-offs' decided
	// editions, which have no champion.
	all := w.History()
	if len(all) != 18 {
		t.Fatalf("history %+v", all)
	}
	for _, rec := range all[14:] {
		if rec.Format != competitions.FormatTies || !rec.Complete || rec.Champion != nil {
			t.Fatalf("play-off history %+v", rec)
		}
	}
	if rec := all[7]; rec.Season != seasonRef(3, 2) || rec.Complete || rec.Champion != nil {
		t.Fatalf("cup edition 2 history %+v", rec)
	}
	for _, rec := range all[6:7] {
		cup, ok := w.Cup(rec.Season)
		final := cup.Rounds[len(cup.Rounds)-1].Ties[0]
		winner := final.Home
		if final.Score[1] > final.Score[0] || (final.Score[0] == final.Score[1] && final.Shootout[1] > final.Shootout[0]) {
			winner = final.Away
		}
		if !ok || rec.Format != competitions.FormatKnockout || !rec.Complete || rec.Champion == nil || *rec.Champion != winner || *cup.Champion != winner {
			t.Fatalf("cup %s: history %+v, final %+v", rec.Season, rec, final)
		}
	}
	hist := all[:3] // the first league
	if hist[2].Complete || hist[2].Champion != nil {
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

	// Identities are untouched: the player year between the seasons only
	// adds youth players after them. Profiles develop but keep positions.
	final := w.Snapshot()
	if final.WorldFingerprint != initial.WorldFingerprint || !reflect.DeepEqual(final.Registry.Clubs, initial.Registry.Clubs) ||
		!reflect.DeepEqual(final.Registry.Players[:len(initial.Registry.Players)], initial.Registry.Players) {
		t.Fatal("a season transition changed identities")
	}
	for i, p := range initial.Players {
		if final.Players[i].Player != p.Player || final.Players[i].Position != p.Position {
			t.Fatalf("player %d's profile became %+v", p.Player, final.Players[i])
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A play-off's season-end event and History agree that there is no champion:
// the event crowns nobody, while a league or cup event carries its derived
// champion (the top of its ranking).
func TestSeasonEndedEventsCarryTheDerivedChampion(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	history := map[competitions.SeasonRef]*TeamLabel{}
	for _, rec := range w.History() {
		history[rec.Season] = rec.Champion
	}
	seen := map[competitions.Format]bool{}
	for _, e := range w.Events() {
		p := e.SeasonEnded
		if p == nil {
			continue
		}
		ref := seasonRef(p.Competition, competitions.Season(p.Season))
		format, _ := w.competitions.Format(ref)
		seen[format] = true
		champion, ok := w.competitions.Champion(ref)
		if (p.Champion != 0) != ok || (ok && p.Champion != champion) {
			t.Fatalf("%s event crowns %d, derived (%d, %v)", ref, p.Champion, champion, ok)
		}
		rec := history[ref]
		if (rec == nil) != (p.Champion == 0) || rec != nil && rec.Team != p.Champion {
			t.Fatalf("%s event crowns %d, history %+v", ref, p.Champion, rec)
		}
		if format == competitions.FormatTies {
			if p.Champion != 0 {
				t.Fatalf("play-off %s crowned %d", ref, p.Champion)
			}
		} else if p.Champion != p.Ranking[0] {
			t.Fatalf("%s champion %d is not the top of %v", ref, p.Champion, p.Ranking)
		}
	}
	if !seen[competitions.FormatTies] || !seen[competitions.FormatLeague] {
		t.Fatalf("season ends of formats %v, want a play-off and a league", seen)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The season ends at its last kickoff, but only after the last round is
// resolved: while that round awaits results, Continue does not run it.
func TestSeasonEndsAfterTheLastRoundIsResolved(t *testing.T) {
	w := userWorld(t, 42, userClub) // the last batch must rest pending
	playBatches(t, w, 13)
	ready := readyBatch(t, w)
	last := ready.At
	if again := mustContinue(t, w, last+30*day); !reflect.DeepEqual(again, ready) || w.leagues[0].season.Season != 1 {
		t.Fatal("the season ended while its last round awaited results")
	}
	resolveNow(t, w)
	rev := w.Revision()
	reached(t, mustContinue(t, w, last), last)
	// The end cohort runs at the last kickoff: the seasons end together and
	// create their play-offs, and the next seasons wait for those to decide.
	if w.Revision() != rev+1 || w.Now() != last || w.leagues[0].season.Season != 1 {
		t.Fatalf("season %s, revision %d (was %d), now %d", w.leagues[0].season, w.Revision(), rev, w.Now())
	}
	for _, link := range w.movementLinks() {
		comp, ok := w.playoffCompetition(link)
		if !ok {
			t.Fatalf("link %+v has no play-off competition", link)
		}
		if _, exists := w.competitions.Entrants(competitions.SeasonRef{Competition: comp, Season: 1}); !exists {
			t.Fatalf("link %+v: no play-off after the season ended", link)
		}
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
		straight := userWorld(t, 42, userClub) // "before season end" needs a batch to rest pending
		stop(straight)
		loaded := roundTrip(t, straight)
		// Play on until season 3 begins: from "before season end" that runs
		// the season end, the first cup edition, then season 2 and its cup.
		next := func(w *World) []RoundsResolved {
			var out []RoundsResolved
			for w.leagues[0].season.Season < 3 {
				out = append(out, playSeason(t, w)...)
			}
			return out
		}
		a, b := next(straight), next(loaded)
		if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(loaded.Snapshot(), straight.Snapshot()) {
			t.Fatalf("%s: the next season differs after a save", name)
		}
		if n := len(a); n < 14+3 {
			t.Fatalf("%s: %d batches", name, n)
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
		"round interval edited": func(s *WorldSnapshot) {
			s.Leagues[0].Definition.RoundInterval += sim.Day
			s.ContentFingerprint = contentFingerprint(s.Content, leagueDefsOf(s), s.Cups, s.Promotions)
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

// leagueDefsOf returns a snapshot's league definitions.
func leagueDefsOf(s *WorldSnapshot) []content.League {
	var out []content.League
	for _, l := range s.Leagues {
		out = append(out, l.Definition)
	}
	return out
}

// For a century of seasons, every league's first round kicks off after that
// summer's transfer window has closed, and its last round, its play-offs and
// the final of the cup edition played during it come before the next player
// year, so the football year keeps its shape against the civil calendar.
// checkFootballYear's bound holds for the default content and for the
// August-to-May calendar of three-week rounds.
func TestSeasonsStayInsideTheContractYear(t *testing.T) {
	for name, interval := range map[string]sim.Duration{"weekly": sim.Week, "three-weekly": 3 * sim.Week} {
		w := worldWithRoundInterval(t, interval)
		for _, l := range w.leagues {
			for season := competitions.Season(1); season <= 100; season++ {
				first, err := competitions.SeasonKickoff(w.calendar, l.def.FirstKickoff, season)
				if err != nil {
					t.Fatal(err)
				}
				_, closes, err := w.transferWindow(first)
				if err != nil || first < closes {
					t.Fatalf("%s: league %d season %d kicks off %s, window closes %s (%v)", name, l.def.ID, season, w.calendar.Format(first), w.calendar.Format(closes), err)
				}
				yearEnd, err := w.contractYearEnd(first)
				if err != nil {
					t.Fatal(err)
				}
				last := first + sim.GameInstant(l.def.Rounds()-1)*sim.GameInstant(l.def.RoundInterval) + sim.GameInstant(competitions.PlayoffDelay)
				for _, c := range w.cups {
					if season > 1 {
						kickoffs, err := w.cupKickoffs(c, season-1)
						if err != nil {
							t.Fatal(err)
						}
						if kickoffs[0] <= closes {
							t.Fatalf("%s: edition %d starts %s, in the window", name, season-1, w.calendar.Format(kickoffs[0]))
						}
						last = max(last, kickoffs[len(kickoffs)-1])
					}
				}
				if last >= yearEnd-sim.GameInstant(sim.Day) {
					t.Fatalf("%s: season %d's football year ends %s, not before the player year %s", name, season, w.calendar.Format(last), w.calendar.Format(yearEnd))
				}
			}
		}
	}
}

// worldWithRoundInterval loads the default career with every league's rounds
// interval apart.
func worldWithRoundInterval(t *testing.T, interval sim.Duration) *World {
	t.Helper()
	return worldWithSeedAndRoundInterval(t, 42, interval)
}

func worldWithSeedAndRoundInterval(t *testing.T, seed random.Seed, interval sim.Duration) *World {
	t.Helper()
	defs := content.Default()
	snap, err := worldgen.Generate(defs, seed)
	if err != nil {
		t.Fatal(err)
	}
	leagues := content.DefaultLeagues()
	for i := range leagues {
		leagues[i].RoundInterval = interval
	}
	w, err := load(defs, leagues, content.DefaultCups(), content.DefaultPromotions(), DefaultEpoch(), snap)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A calendar that does not fit the football year, or a cup that cannot be
// played between its leagues' matchdays, is refused when the career is
// created and when it is restored.
func TestLoadRejectsCalendarsOutsideTheFootballYear(t *testing.T) {
	defs := content.Default()
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	all := func(f func(*content.League)) func([]content.League, []content.Cup) {
		return func(leagues []content.League, _ []content.Cup) {
			for i := range leagues {
				f(&leagues[i])
			}
		}
	}
	cases := map[string]func([]content.League, []content.Cup){
		"kicks off in the transfer window":            all(func(l *content.League) { l.FirstKickoff.Month, l.FirstKickoff.Day = 7, 26 }),
		"a later season could kick off in the window": all(func(l *content.League) { l.FirstKickoff.Month, l.FirstKickoff.Day = 7, 31 }),
		"runs into the next contract year":            all(func(l *content.League) { l.FirstKickoff.Month, l.RoundInterval = 10, 3*sim.Week }),
		"play-offs in the player year": all(func(l *content.League) {
			l.FirstKickoff, l.RoundInterval = sim.CivilTime{Year: 2025, Month: 9, Day: 20, Hour: 15}, 3*sim.Week
		}),
		"no midweek between matchdays": all(func(l *content.League) { l.RoundInterval = competitions.CupMidweek }),
		"cup leagues on two calendars": func(leagues []content.League, _ []content.Cup) {
			for i := range leagues {
				if leagues[i].ID == 2 || leagues[i].ID == 5 { // a linked pair keeps one calendar
					leagues[i].FirstKickoff.Day += 7
				}
			}
		},
		"more cup rounds than matchdays": func(leagues []content.League, cups []content.Cup) {
			for i := range leagues {
				leagues[i].Entrants = 2 // two matchdays
			}
			cups[0].Qualifiers = []content.Qualifier{{League: 1, Places: 2}, {League: 2, Places: 2}, {League: 4, Places: 2}, {League: 5, Places: 2}}
		},
	}
	for name, edit := range cases {
		leagues, cups := content.DefaultLeagues(), content.DefaultCups()
		edit(leagues, cups)
		if _, err := load(defs, leagues, cups, content.DefaultPromotions(), DefaultEpoch(), snap); err == nil {
			t.Errorf("%s: load succeeded", name)
		}
	}

	// A save is checked the same way.
	saved := newWorld(t, 42).Snapshot()
	for i := range saved.Leagues {
		saved.Leagues[i].Definition.FirstKickoff.Month = 10
		saved.Leagues[i].Definition.RoundInterval = 3 * sim.Week
	}
	saved.ContentFingerprint = contentFingerprint(saved.Content, leagueDefsOf(&saved), saved.Cups, saved.Promotions)
	if w, err := Restore(saved); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
		t.Fatalf("restore of an overrunning calendar: %v", err)
	}
}

// With rounds three weeks apart the football year fills the year: the
// leagues play from August to May, the play-offs a week after their last
// matchday, and the cup edition drawn from a season is played on Wednesdays
// during the next, its final after the last matchday. No team has two
// fixtures at once, everything lies between the transfer window's close and
// the player year, saving midway continues identically, and one Continue
// equals many.
func TestFootballYearFromAugustToMay(t *testing.T) {
	w := worldWithRoundInterval(t, 3*sim.Week)
	civil := func(at sim.GameInstant) sim.CivilTime {
		c, err := w.calendar.Civil(at)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := civil(w.Schedules()[0].Rounds[13].Kickoff); c.Year != 2026 || c.Month != 5 {
		t.Fatalf("season 1 ends %v", c)
	}
	playSeason(t, w)
	saved := roundTrip(t, w)
	for _, x := range []*World{w, saved} {
		playSeason(t, x)
	}
	if !reflect.DeepEqual(saved.Snapshot(), w.Snapshot()) {
		t.Fatal("a save between seasons continued differently")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	// One Continue over both seasons equals the many above; only the
	// revisions, which count commits, differ.
	one := worldWithRoundInterval(t, 3*sim.Week)
	mustContinue(t, one, w.Now())
	stOne, stMany := snapshot(one), snapshot(w)
	stOne.Revision, stMany.Revision = 0, 0
	for _, st := range []*state{&stOne, &stMany} {
		for i := range st.Events {
			st.Events[i].Revision, st.Events[i].Sequence = 0, 0
		}
	}
	if !reflect.DeepEqual(stOne, stMany) {
		t.Fatal("one Continue over two seasons differs from playing them step by step")
	}

	cup, ok := w.Cup(cup1)
	if !ok || !cup.Complete {
		t.Fatalf("edition 1 %+v", cup)
	}
	var played [][2]int // year, month
	for _, r := range cup.Rounds {
		if !strings.HasPrefix(w.calendar.Format(r.Kickoff), "Wed") {
			t.Fatalf("edition 1 %s on %s", r.Name, w.calendar.Format(r.Kickoff))
		}
		c := civil(r.Kickoff)
		played = append(played, [2]int{c.Year, int(c.Month)})
	}
	if !slices.Equal(played, [][2]int{{2026, 11}, {2027, 2}, {2027, 5}}) {
		t.Fatalf("edition 1 played in %v", played)
	}

	type slot struct {
		team ids.TeamID
		at   sim.GameInstant
	}
	busy := map[slot]bool{}
	for _, ref := range w.competitions.Seasons() {
		for _, f := range w.competitions.Fixtures(ref) {
			for _, team := range []ids.TeamID{f.Home, f.Away} {
				if busy[slot{team, f.Kickoff}] {
					t.Fatalf("team %d has two fixtures at %s", team, w.calendar.Format(f.Kickoff))
				}
				busy[slot{team, f.Kickoff}] = true
			}
			_, closes, err := w.transferWindow(f.Kickoff)
			if err != nil {
				t.Fatal(err)
			}
			yearEnd, err := w.contractYearEnd(f.Kickoff)
			if err != nil {
				t.Fatal(err)
			}
			if f.Kickoff < closes || f.Kickoff >= yearEnd-sim.GameInstant(sim.Day) {
				t.Fatalf("%s fixture %d on %s, outside the football year", ref, f.ID, w.calendar.Format(f.Kickoff))
			}
		}
	}
}

// A career runs with leagues of other sizes than eight: two linked divisions
// of ten and two of six, the Continental Cup drawn from both top divisions.
// Each season is a full double round-robin of its size, the play-offs keep
// every division's size, the cup takes its places, the world validates
// throughout, and a save in the middle continues identically.
func TestLeaguesOfOtherSizesPlayConsecutiveSeasons(t *testing.T) {
	defs := content.Default()
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[ids.CompetitionID]int{1: 10, 2: 6, 4: 10, 5: 6}
	leagues := content.DefaultLeagues()
	for i := range leagues {
		leagues[i].Entrants = sizes[leagues[i].ID]
		leagues[i].RoundInterval = sim.Week
	}
	w, err := load(defs, leagues, content.DefaultCups(), content.DefaultPromotions(), DefaultEpoch(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if fp := w.Summary().Fingerprint; fp != newWorld(t, 42).Summary().Fingerprint {
		t.Fatal("league sizes changed the world fingerprint")
	}

	checkSeason := func(ref competitions.SeasonRef) {
		t.Helper()
		n := sizes[ref.Competition]
		entrants, ok := w.competitions.Entrants(ref)
		if !ok || len(entrants) != n || len(w.competitions.Rounds(ref)) != 2*(n-1) || len(w.competitions.Fixtures(ref)) != n*(n-1) {
			t.Fatalf("%s: %d entrants, %d rounds, %d fixtures for a league of %d", ref, len(entrants), len(w.competitions.Rounds(ref)), len(w.competitions.Fixtures(ref)), n)
		}
		if table, ok := w.Table(ref); !ok || len(table.Rows) != n {
			t.Fatalf("%s: table of %d rows", ref, len(table.Rows))
		}
	}
	for comp := range sizes {
		checkSeason(seasonRef(comp, 1))
	}

	playLeagues(t, w)
	saved := roundTrip(t, w)
	for _, x := range []*World{w, saved} {
		playPlayoffs(t, x)
		playCup(t, x)
		playSeason(t, x)
	}
	if !reflect.DeepEqual(saved.Snapshot(), w.Snapshot()) {
		t.Fatal("a save after the leagues continued differently")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	for comp := range sizes {
		for _, n := range []competitions.Season{1, 2} {
			ref := seasonRef(comp, n)
			checkSeason(ref)
			if !w.competitions.SeasonCompleted(ref) {
				t.Fatalf("%s is not complete", ref)
			}
		}
		checkSeason(seasonRef(comp, 3))
	}
	// Two places cross each link: the sizes hold and the divisions swap
	// exactly the teams the play-offs moved.
	for _, link := range content.DefaultPromotions() {
		u1, _ := w.competitions.Entrants(seasonRef(link.Upper, 1))
		u2, _ := w.competitions.Entrants(seasonRef(link.Upper, 2))
		l2, _ := w.competitions.Entrants(seasonRef(link.Lower, 2))
		left := 0
		for _, team := range u1 {
			if !slices.Contains(u2, team) {
				left++
				if !slices.Contains(l2, team) {
					t.Fatalf("team %d left %d but is not in %d", team, link.Upper, link.Lower)
				}
			}
		}
		if left > link.Places {
			t.Fatalf("%d teams left league %d over %d places", left, link.Upper, link.Places)
		}
	}
	// Edition 1 was played during season 2, over the shorter league's ten
	// matchdays; edition 2 is drawn for season 3.
	for _, edition := range []competitions.SeasonRef{seasonRef(3, 1), seasonRef(3, 2)} {
		entrants, ok := w.competitions.Entrants(edition)
		if complete := w.competitions.SeasonCompleted(edition); !ok || len(entrants) != 8 || complete != (edition.Season == 1) {
			t.Fatalf("%s: %d entrants, complete %v", edition, len(entrants), complete)
		}
	}
}
