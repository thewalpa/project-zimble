package app

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/inbox"
)

var cup1 = competitions.SeasonRef{Competition: 3, Season: 1}

// top returns a league season's team at a final position (1-based).
func top(w *World, league ids.CompetitionID, pos int) ids.TeamID {
	return w.competitions.Ranking(competitions.SeasonRef{Competition: league, Season: 1})[pos-1]
}

// playToCupRound plays everything before round r (1-based) of each cup's
// latest edition kicks off: the league matchdays before it.
func playToCupRound(t *testing.T, w *World, r int) []RoundsResolved {
	t.Helper()
	return playUntil(t, w, func(w *World) sim.GameInstant {
		at := w.Now()
		for _, e := range w.Cups() {
			at = max(at, e.Rounds[r-1].Kickoff-1)
		}
		return at
	})
}

// cupBatches counts the resolved batches that hold a round of the cup.
func cupBatches(resolved []RoundsResolved) int {
	n := 0
	for _, r := range resolved {
		if slices.ContainsFunc(r.Rounds, func(ref competitions.RoundRef) bool { return ref.Season.Competition == cup1.Competition }) {
			n++
		}
	}
	return n
}

// The first edition is created when the play-offs after both leagues' first
// seasons decide: the top four of each, seeded so the champions can meet
// only in the final, the better-placed team at home in every quarter-final.
// It is played during the leagues' second season, on the Wednesdays after
// matchdays 5, 10 and 14 of 14 (competitions.CupMatchdays): the final
// follows the last matchday.
func TestCupEditionIsCreatedFromTheLeagues(t *testing.T) {
	w := newWorld(t, 42)
	if len(w.Cups()) != 0 {
		t.Fatal("a cup edition exists before any league season ended")
	}
	playLeagues(t, w)
	playPlayoffs(t, w)
	e, ok := w.Cup(cup1)
	if !ok || e.Name != "Continental Cup" || len(e.Rounds) != 3 || e.Complete {
		t.Fatalf("edition %+v, %v", e, ok)
	}
	want := []ids.TeamID{top(w, 1, 1), top(w, 2, 4), top(w, 2, 2), top(w, 1, 3), top(w, 2, 1), top(w, 1, 4), top(w, 1, 2), top(w, 2, 3)}
	var got []ids.TeamID
	for _, l := range e.Entrants {
		got = append(got, l.Team)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("bracket %v, want %v", got, want)
	}
	for i, tie := range e.Rounds[0].Ties {
		if tie.Home.Team != want[2*i] || tie.Away.Team != want[2*i+1] {
			t.Fatalf("quarter-final %d is %v v %v", i+1, tie.Home.Team, tie.Away.Team)
		}
	}
	second := w.Schedules()[0]
	if second.Season != 2 {
		t.Fatalf("the leagues are in season %d", second.Season)
	}
	for i, r := range e.Rounds {
		matchday := second.Rounds[[]int{5, 10, 14}[i]-1].Kickoff
		if r.Kickoff != matchday+sim.GameInstant(competitions.CupMidweek) || !strings.HasPrefix(w.calendar.Format(r.Kickoff), "Wed") || r.Status != competitions.RoundScheduled {
			t.Fatalf("round %d kicks off at %s (%s), matchday at %s", i+1, w.calendar.Format(r.Kickoff), r.Status, w.calendar.Format(matchday))
		}
		if (i == 0) != (len(r.Ties) > 0) {
			t.Fatalf("round %d has %d ties before any cup result", i+1, len(r.Ties))
		}
	}
	if names := []string{e.Rounds[0].Name, e.Rounds[1].Name, e.Rounds[2].Name}; !slices.Equal(names, []string{"quarter-final", "semi-final", "final"}) {
		t.Fatalf("round names %v", names)
	}
	var started []*events.SeasonStarted
	for _, ev := range w.Events() {
		if ev.Kind == events.KindSeasonStarted && ev.SeasonStarted.Competition == 3 {
			started = append(started, ev.SeasonStarted)
		}
	}
	if len(started) != 1 || !slices.Equal(started[0].Entrants, want) {
		t.Fatalf("season-started events %+v", started)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Only the first divisions send teams to the cup, and each league's
// qualifying places are the teams its first edition is drawn from.
func TestCupQualifiers(t *testing.T) {
	w := newWorld(t, 42)
	want := map[ids.CompetitionID][]CupQualifier{
		1: {{Cup: 3, Name: "Continental Cup", Places: 4}},
		2: {{Cup: 3, Name: "Continental Cup", Places: 4}},
	}
	for _, league := range []ids.CompetitionID{1, 2, 3, 4, 5, 99} {
		if got := w.CupQualifiers(league); !reflect.DeepEqual(got, want[league]) {
			t.Fatalf("league %d qualifies for %+v, want %+v", league, got, want[league])
		}
	}
	w.CupQualifiers(1)[0].Places = 8
	if w.CupQualifiers(1)[0].Places != 4 {
		t.Fatal("editing a returned qualifier reached the world")
	}

	playLeagues(t, w)
	playPlayoffs(t, w)
	e, _ := w.Cup(cup1)
	var drawn, qualified []ids.TeamID
	for _, l := range e.Entrants {
		drawn = append(drawn, l.Team)
	}
	for _, league := range []ids.CompetitionID{1, 2, 4, 5} {
		for _, q := range w.CupQualifiers(league) {
			for pos := 1; pos <= q.Places; pos++ {
				qualified = append(qualified, top(w, league, pos))
			}
		}
	}
	slices.Sort(drawn)
	slices.Sort(qualified)
	if !slices.Equal(drawn, qualified) {
		t.Fatalf("edition 1 drew %v, the qualifying places hold %v", drawn, qualified)
	}
}

// The cup is played round by round to a champion. Every tie has a winner
// (a level one by penalties, recorded officially and in the events), the
// winners meet in the next round, and only cup matches are knockouts.
func TestCupIsPlayedToAChampion(t *testing.T) {
	shootouts := 0
	for _, seed := range []uint64{42, 7, 11, 23} {
		w := newWorld(t, seed)
		playLeagues(t, w)
		playPlayoffs(t, w)
		resolved := playCup(t, w)
		if n := cupBatches(resolved); n != 3 {
			t.Fatalf("seed %d: %d cup batches", seed, n)
		}
		e, _ := w.Cup(cup1)
		if !e.Complete || e.Champion == nil {
			t.Fatalf("seed %d: edition %+v", seed, e)
		}
		winners := func(ties []FixtureLine) (out []ids.TeamID) {
			for _, tie := range ties {
				r, _ := w.competitions.Result(tie.ID)
				winner, ok := r.Winner()
				if !ok || !tie.Played {
					t.Fatalf("seed %d: tie %d has no winner", seed, tie.ID)
				}
				if level := r.HomeGoals == r.AwayGoals; level != (r.HomePenalties != r.AwayPenalties) {
					t.Fatalf("seed %d: tie %d result %+v", seed, tie.ID, r)
				}
				if r.HomeGoals == r.AwayGoals {
					shootouts++
				}
				out = append(out, winner)
			}
			return out
		}
		for i := 0; i+1 < len(e.Rounds); i++ {
			w := winners(e.Rounds[i].Ties)
			var next []ids.TeamID
			for _, tie := range e.Rounds[i+1].Ties {
				next = append(next, tie.Home.Team, tie.Away.Team)
			}
			if !slices.Equal(w, next) {
				t.Fatalf("seed %d: round %d winners %v, next round %v", seed, i+1, w, next)
			}
		}
		if champion := winners(e.Rounds[2].Ties); champion[0] != e.Champion.Team {
			t.Fatalf("seed %d: final won by %d, champion %d", seed, champion[0], e.Champion.Team)
		}
		// Reports and events carry the shootout; the official ranking
		// ends the edition.
		for _, r := range resolved {
			for _, m := range r.Matches {
				off, _ := w.competitions.Result(m.Fixture)
				if m.Shootout != [2]uint16{off.HomePenalties, off.AwayPenalties} {
					t.Fatalf("seed %d: report %+v, official %+v", seed, m, off)
				}
			}
		}
		for _, ev := range w.Events() {
			if p := ev.MatchCompleted; p != nil && p.Competition == 3 {
				off, _ := w.competitions.Result(p.Fixture)
				if p.HomePenalties != off.HomePenalties || p.AwayPenalties != off.AwayPenalties {
					t.Fatalf("seed %d: event %+v", seed, p)
				}
			}
			if p := ev.SeasonEnded; p != nil && p.Competition == 3 && p.Ranking[0] != e.Champion.Team {
				t.Fatalf("seed %d: season ended %+v", seed, p)
			}
		}
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if shootouts == 0 {
		t.Fatal("no cup tie in four editions went to penalties")
	}
}

// League fixtures are never knockout matches for the engine; the cup's and
// the play-offs' are. (prepareBatch copies these rules into every match it
// plans; batches with no user fixture never pause to plan one.)
func TestOnlyCupMatchesAreKnockouts(t *testing.T) {
	w := newWorld(t, 42)
	knockout := func(comp ids.CompetitionID, want bool, what string) {
		t.Helper()
		rules, err := w.matchRules(comp)
		if err != nil {
			t.Fatalf("%s rules: %v", what, err)
		}
		if rules.Knockout != want {
			t.Fatalf("%s rules %+v: knockout %t, want %t", what, rules, rules.Knockout, want)
		}
	}
	knockout(w.leagues[0].def.ID, false, "league")
	playLeagues(t, w) // ends the seasons and creates the play-offs
	seen := 0
	for _, link := range w.movementLinks() {
		comp, ok := w.playoffCompetition(link)
		if !ok {
			t.Fatalf("link %+v has no play-off competition", link)
		}
		knockout(comp, true, "play-off")
		seen++
	}
	if seen == 0 {
		t.Fatal("no play-off competition after the season")
	}
	playPlayoffs(t, w) // draws the cup edition
	knockout(cup1.Competition, true, "cup")
}

// Saving before and after each cup round continues exactly like never
// saving: brackets are restored, not rebuilt. (A batch awaiting results is
// save-tested in TestSaveWhileBatchPending; without a user club no batch
// pauses for one.)
func TestCupSurvivesSaves(t *testing.T) {
	play := func(w *World, save func(*World) *World) *World {
		playSeason(t, w) // draws edition 1
		for range 2 {    // editions 1 and 2, during seasons 2 and 3
			for r := 1; r <= 3; r++ { // the quarter-finals, semi-finals and final
				playToCupRound(t, w, r)
				w = save(w) // before each cup round
				if res := playBatches(t, w, 1); cupBatches(res) != 1 {
					t.Fatalf("cup round %d not played: %+v", r, res)
				}
				w = save(w) // after it
			}
			playSeason(t, w) // the rest of the season, the play-offs and the next draw
		}
		return w
	}
	straight := play(newWorld(t, 42), func(w *World) *World { return w })
	w := play(newWorld(t, 42), func(w *World) *World { return roundTrip(t, w) })
	if !reflect.DeepEqual(w.Snapshot(), straight.Snapshot()) {
		t.Fatal("saves changed the career")
	}
}

// A managed club in the cup gets its matchdays, results (with penalties)
// and how far it went; it can submit cup lineups.
func TestManagedClubInTheCup(t *testing.T) {
	plain := newWorld(t, 42)
	playLeagues(t, plain)
	champion := plain.teamLabel(top(plain, 1, 1)).Club

	w := userWorld(t, 42, champion)
	playLeagues(t, w)
	playPlayoffs(t, w)
	playToCupRound(t, w, 1)
	ready := readyBatch(t, w)
	if len(ready.UserFixtures) != 1 {
		t.Fatalf("user fixtures %v in the quarter-finals", ready.UserFixtures)
	}
	info, ok := w.FixtureInfo(ready.UserFixtures[0])
	if !ok || !info.Cup || info.CompetitionName != "Continental Cup" || info.RoundName != "quarter-final" {
		t.Fatalf("fixture info %+v", info)
	}
	submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
	resolveNow(t, w)
	playCup(t, w)
	team := mustUserTeam(t, w)
	stage := ""
	results := 0
	for _, m := range w.Inbox() {
		if m.Competition != 3 {
			continue
		}
		switch m.Kind {
		case inbox.KindResult:
			results++
			r, _ := w.competitions.Result(m.Fixture)
			pens := [2]uint16{r.HomePenalties, r.AwayPenalties}
			if r.Away == team {
				pens = [2]uint16{pens[1], pens[0]}
			}
			if m.Shootout != pens || !m.Cup || m.RoundName == "" {
				t.Fatalf("result message %+v for %+v", m, r)
			}
		case inbox.KindSeasonEnded:
			stage = m.Stage
		}
	}
	e, _ := w.Cup(cup1)
	want := "winner"
	if e.Champion.Team != team {
		want = e.Rounds[results-1].Name
	}
	if results == 0 || stage != want {
		t.Fatalf("%d cup results, stage %q, want %q", results, stage, want)
	}
}

func TestRestoreRejectsInvalidCups(t *testing.T) {
	built := func() WorldSnapshot {
		w := newWorld(t, 42)
		playLeagues(t, w)
		playPlayoffs(t, w)
		playToCupRound(t, w, 1)
		playBatches(t, w, 1) // the quarter-finals are played
		return w.Snapshot()
	}
	cupSeason := func(s *WorldSnapshot) *competitions.SeasonSnapshot {
		for i := range s.Competitions.Seasons {
			if s.Competitions.Seasons[i].Ref == cup1 {
				return &s.Competitions.Seasons[i]
			}
		}
		t.Fatal("no cup season")
		return nil
	}
	cases := map[string]func(*WorldSnapshot){
		"cup definition edited": func(s *WorldSnapshot) { s.Cups[0].FirstRoundDelay++ },
		"cup definition edited and refingerprinted": func(s *WorldSnapshot) {
			s.Cups[0].Qualifiers = slices.Clone(s.Cups[0].Qualifiers)
			slices.Reverse(s.Cups[0].Qualifiers)
			s.ContentFingerprint = contentFingerprint(s.Content, leagueDefsOf(s), s.Cups, s.Promotions)
		},
		"cup round rescheduled": func(s *WorldSnapshot) {
			c := cupSeason(s)
			c.Rounds[1].Kickoff += sim.GameInstant(sim.Day)
		},
		"cup prizes increase, refingerprinted": func(s *WorldSnapshot) {
			s.Cups[0].Prizes = slices.Clone(s.Cups[0].Prizes)
			s.Cups[0].Prizes[1] = s.Cups[0].Prizes[0] + 1
			s.ContentFingerprint = contentFingerprint(s.Content, leagueDefsOf(s), s.Cups, s.Promotions)
		},
		"cup with a league's ID": func(s *WorldSnapshot) {
			s.Cups[0].ID = 2
			s.ContentFingerprint = contentFingerprint(s.Content, leagueDefsOf(s), s.Cups, s.Promotions)
		},
		"cup removed": func(s *WorldSnapshot) {
			s.Cups = nil
			s.ContentFingerprint = contentFingerprint(s.Content, leagueDefsOf(s), s.Cups, s.Promotions)
		},
		"bracket reseeded": func(s *WorldSnapshot) {
			c := cupSeason(s)
			c.Entrants[0], c.Entrants[2] = c.Entrants[2], c.Entrants[0]
		},
		"shootout edited": func(s *WorldSnapshot) {
			c := cupSeason(s)
			for i, r := range c.Results {
				if r.HomeGoals != r.AwayGoals {
					c.Results[i].HomePenalties = 3
					return
				}
			}
		},
		"cup season-end task missing": func(s *WorldSnapshot) {
			var payload sim.PayloadID
			for _, p := range s.SeasonEndPayloads {
				if p.Season == cup1 {
					payload = p.ID
				}
			}
			s.Scheduler.Tasks = slices.DeleteFunc(s.Scheduler.Tasks, func(task sim.Task) bool {
				return task.Kind == taskSeasonEnd && task.PayloadID == payload
			})
			s.SeasonEndPayloads = slices.DeleteFunc(s.SeasonEndPayloads, func(p SeasonEndPayload) bool { return p.ID == payload })
		},
	}
	for name, mutate := range cases {
		snap := built()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Restore(built()); err != nil {
		t.Fatalf("unmodified save: %v", err)
	}
}

// A future knockout round reserves its surviving entrants' kickoff even
// before their ties exist. Eliminated teams may play another competition.
func TestCupClashChecksUngeneratedRounds(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	cup, _ := w.Cup(cup1)
	spec := competitions.NewSeason{
		Ref:      competitions.SeasonRef{Competition: 90, Season: 1},
		Format:   competitions.FormatKnockout,
		Entrants: []ids.TeamID{cup.Entrants[0].Team, cup.Entrants[1].Team},
		Timing:   competitions.Timing{Kickoffs: []sim.GameInstant{cup.Rounds[1].Kickoff}},
	}
	before := w.Snapshot()
	if err := w.checkNoTeamClash([]competitions.NewSeason{spec}); err == nil {
		t.Fatal("a second cup can clash with an ungenerated semi-final")
	}
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("clash check changed the world")
	}
	playToCupRound(t, w, 2)
	var eliminated []ids.TeamID
	for _, r := range w.competitions.Results(cup1) {
		winner, _ := r.Winner()
		loser := r.Home
		if winner == loser {
			loser = r.Away
		}
		eliminated = append(eliminated, loser)
	}
	spec.Entrants = eliminated[:2]
	if err := w.checkNoTeamClash([]competitions.NewSeason{spec}); err != nil {
		t.Fatalf("eliminated teams still reserve the semi-final: %v", err)
	}
}
