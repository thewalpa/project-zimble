package app

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
)

// injure lays a player off for days recovery days through the medical store,
// as a match's result would.
func injure(t *testing.T, w *World, player ids.PlayerID, days uint16) {
	t.Helper()
	plan, err := w.medical.PlanExposure([]medical.Exposure{{Player: player, Minutes: 1, Stamina: 50}}, []medical.Injury{{Player: player, Days: days}})
	if err != nil {
		t.Fatal(err)
	}
	w.applyMedical(plan)
}

// Injured players are not selected: the AI passes them over, the manager's
// lineups may not name them, and a carried-over lineup refills their places.
func TestInjuredPlayersAreNotSelected(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ready := readyBatch(t, w)
	fixture := ready.UserFixtures[0]
	suggested, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	star := suggested.Starters[5].Player
	injure(t, w, star, 6)
	if d, ok := w.Injury(star); !ok || d != 6 {
		t.Fatalf("injury %d, %t", d, ok)
	}

	next, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(next.Players(), star) {
		t.Fatal("the AI suggestion names an injured player")
	}
	cmd := SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: suggested}
	if _, err := w.SubmitLineup(cmd); !errors.Is(err, ErrInvalidLineup) {
		t.Fatalf("submitting an injured player: %v", err)
	}
	// The AI clubs pass over their injured too.
	for _, id := range ready.Rounds[0].Fixtures {
		f, _ := w.competitions.Fixture(id)
		if f.Home == mustUserTeam(t, w) || f.Away == mustUserTeam(t, w) {
			continue
		}
		rules, err := w.matchRules(f.Season.Competition)
		if err != nil {
			t.Fatal(err)
		}
		before, err := w.selectTeam(f.Home, rules)
		if err != nil {
			t.Fatal(err)
		}
		hurt := before.Starters[0].Player
		injure(t, w, hurt, 3)
		after, err := w.selectTeam(f.Home, rules)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(after.Players(), hurt) {
			t.Fatalf("AI team %d selects injured player %d", f.Home, hurt)
		}
		break
	}

	// The manager's lineup, submitted without him, is played; then he is
	// injured out of the carried-over lineup.
	submit(t, w, fixture, next)
	resolveNow(t, w)
	after := readyBatch(t, w)
	var carried MatchdayLineup
	for _, id := range after.UserFixtures {
		if carried, err = w.MatchdayLineup(id); err != nil {
			t.Fatal(err)
		}
		if carried.Source != LineupCarriedOver {
			t.Fatalf("second lineup is %s, want carried over", carried.Source)
		}
		var victim ids.PlayerID
		for _, s := range carried.Lineup.Starters {
			if _, hurt := w.Injury(s.Player); !hurt {
				victim = s.Player
				break
			}
		}
		injure(t, w, victim, 4)
		again, err := w.MatchdayLineup(id)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(again.Lineup.Players(), victim) || !slices.Contains(again.Dropped, victim) || len(again.Lineup.Starters) != 11 {
			t.Fatalf("carried lineup keeps injured %d: %+v", victim, again)
		}
	}
}

// Recovery days count down with the daily recovery, an event and a message
// announce the return, and a save keeps the layoff.
func TestInjuriesHealAndSurviveSaves(t *testing.T) {
	w := userWorld(t, 42, userClub)
	squad, _ := w.Squad(userClub)
	hurt := squad[0].Player
	injure(t, w, hurt, 3)
	w = roundTrip(t, w)
	if d, ok := w.Injury(hurt); !ok || d != 3 {
		t.Fatalf("a save lost the injury: %d, %t", d, ok)
	}
	if s, _ := w.Squad(userClub); s[0].DaysOut != 3 {
		t.Fatalf("squad shows %d days out", s[0].DaysOut)
	}
	start := w.Now()
	mustContinue(t, w, start+3*day)
	if _, ok := w.Injury(hurt); ok {
		t.Fatal("still injured after three recovery days")
	}
	got := 0
	for _, e := range w.Events() {
		if p := e.PlayerRecovered; p != nil && p.Player == hurt {
			got++
			if p.Team != mustUserTeam(t, w) {
				t.Fatalf("recovery event %+v", p)
			}
		}
	}
	if got != 1 {
		t.Fatalf("%d recovery events", got)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	snap := w.Snapshot()
	snap.Medical[0].DaysOut = medical.MaxInjuryDays + 1
	if _, err := Restore(snap); err == nil {
		t.Fatal("a layoff beyond the maximum was restored")
	}
}

// A club that cannot field a team from its fit players fields the injured
// ones too, so a match can always be played.
func TestASquadTooHurtToFieldPlaysItsInjured(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	squad := w.employment.Squad(team)
	fit := len(w.availableSquad(team))
	for _, id := range squad {
		if p, _ := w.players.Profile(id); p.Position == players.Goalkeeper {
			injure(t, w, id, 10)
		}
	}
	if got := w.availableSquad(team); !reflect.DeepEqual(got, squad) || fit != len(squad) {
		t.Fatalf("available %d of %d players with every goalkeeper hurt", len(got), len(squad))
	}
	ready := readyBatch(t, w)
	l, err := w.SuggestLineup(ready.UserFixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	submit(t, w, ready.UserFixtures[0], l) // an injured goalkeeper may be named
	resolveNow(t, w)
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Over several seasons injuries happen, every club can always field a legal
// lineup from its fit players (or plays its injured), everyone recovers and
// the world stays valid.
func TestInjuriesOverSeasons(t *testing.T) {
	for _, seed := range []uint64{7, 42} {
		w := userWorld(t, seed, userClub)
		injured := 0
		for range 3 {
			playSeason(t, w)
			for _, e := range w.Events() {
				if e.PlayerInjured != nil {
					injured++
				}
			}
			for _, c := range w.registry.Clubs() {
				team, _ := w.registry.SeniorTeam(c.ID)
				if w.canField(w.employment.Squad(team)) && !w.canField(w.availableSquad(team)) {
					t.Fatalf("seed %d: club %d cannot field a team", seed, c.ID)
				}
			}
			if err := w.Validate(); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
			mustContinue(t, w, w.ContractYearEnd())
		}
		if injured == 0 {
			t.Fatalf("seed %d: no injuries in three seasons", seed)
		}
		// Every current injury recovers by its deadline, even when a new
		// season starts during that time and can cause fresh injuries.
		awaiting := map[ids.PlayerID]sim.GameInstant{}
		for _, r := range w.medical.Records() {
			if r.DaysOut > 0 {
				awaiting[r.Player] = w.Now() + sim.GameInstant(r.DaysOut)*day
			}
		}
		seen := w.lastEvent
		for len(awaiting) > 0 {
			target := w.Now() + day
			for {
				if _, ready := mustContinue(t, w, target).(FixtureRoundReady); !ready {
					break
				}
				resolveNow(t, w)
			}
			for _, e := range w.Events() {
				if e.ID > seen && e.PlayerRecovered != nil {
					delete(awaiting, e.PlayerRecovered.Player)
				}
			}
			seen = w.lastEvent
			for player, deadline := range awaiting {
				if w.Now() > deadline {
					t.Fatalf("seed %d: player %d did not recover by %s", seed, player, w.calendar.Format(deadline))
				}
			}
		}
	}
}

func TestInjuriesAreDeterministic(t *testing.T) {
	a, b := userWorld(t, 42, userClub), userWorld(t, 42, userClub)
	playSeason(t, a)
	playSeason(t, b)
	if !reflect.DeepEqual(a.medical.Snapshot(), b.medical.Snapshot()) || !reflect.DeepEqual(a.Events(), b.Events()) {
		t.Fatal("the same seed produced different injuries")
	}
}

// medicalLevels separates the default football year from the weekly-rest
// stress scenario. Rest buckets are a team's gap between fixtures, rather
// than a player's gap between appearances.
type medicalLevels struct {
	playerSeasons, injuries, injuryDays        int
	starters, tired, conditionSum              int
	kickoffs, out, mostOut, emergencies        int
	lowest, lowestRested                       int
	startsByRest, tiredByRest, conditionByRest [3]int
}

func measureMedicalLevels(t *testing.T, w *World) medicalLevels {
	t.Helper()
	end, nextYear := w.Now()+3*365*day, w.Now()
	seen := w.lastEvent
	var m medicalLevels
	m.lowest, m.lowestRested = int(medical.MaxCondition), int(medical.MaxCondition)
	lastPlayed := map[ids.TeamID]sim.GameInstant{}
	for {
		if w.Now() >= nextYear {
			nextYear += 365 * day
			for _, c := range w.registry.Clubs() {
				team, _ := w.registry.SeniorTeam(c.ID)
				m.playerSeasons += len(w.employment.Squad(team))
			}
		}
		kick := end
		for _, task := range w.scheduler.Pending() {
			if task.Kind == taskRoundKickoff && task.DueAt < kick {
				kick = task.DueAt
			}
		}
		if kick == end {
			break
		}
		mustContinue(t, w, kick-1)
		before := map[ids.PlayerID]medical.Record{}
		for _, r := range w.medical.Records() {
			before[r.Player] = r
		}
		res, ok := mustContinue(t, w, kick).(ReachedTarget)
		if !ok {
			t.Fatalf("an AI-only career stopped at %d", kick)
		}
		for _, b := range res.Resolved {
			for _, match := range b.Matches {
				for side, team := range []ids.TeamID{match.Home.Team, match.Away.Team} {
					n := 0
					for _, id := range w.employment.Squad(team) {
						if before[id].DaysOut > 0 {
							n++
						}
					}
					m.kickoffs++
					m.out += n
					m.mostOut = max(m.mostOut, n)
					for _, s := range match.Lineups[side].Starters {
						c := int(before[s.Player].Condition)
						gap := sim.Duration(3 * sim.Week)
						if last, played := lastPlayed[team]; played {
							gap = sim.Duration(b.At - last)
						}
						bucket := 2 // at least three weeks, including the first match
						if gap < sim.Week {
							bucket = 0
						} else if gap < 3*sim.Week {
							bucket = 1
						}
						m.startsByRest[bucket]++
						m.conditionByRest[bucket] += c
						if c < int(medical.MaxCondition) {
							m.tiredByRest[bucket]++
						}
						if before[s.Player].DaysOut > 0 {
							m.emergencies++
						}
						m.starters++
						m.conditionSum += c
						if c < int(medical.MaxCondition) {
							m.tired++
						}
						m.lowest = min(m.lowest, c)
						if last, played := lastPlayed[team]; !played || b.At-last >= sim.GameInstant(sim.Week) {
							m.lowestRested = min(m.lowestRested, c)
						}
					}
					lastPlayed[team] = b.At
				}
			}
		}
		for _, e := range w.Events() {
			if e.ID > seen && e.PlayerInjured != nil {
				m.injuries++
				m.injuryDays += int(e.PlayerInjured.Days)
			}
		}
		seen = w.lastEvent
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d injuries in %d player-seasons (%.3f each), %.2f days per injury; %.2f out per club at kickoff (most %d), %d emergency starts; %d of %d starters tired (%.2f%%), mean %.2f, lowest %d, weekly-rest lowest %d",
		m.injuries, m.playerSeasons, float64(m.injuries)/float64(m.playerSeasons), float64(m.injuryDays)/float64(m.injuries),
		float64(m.out)/float64(m.kickoffs), m.mostOut, m.emergencies, m.tired, m.starters, 100*float64(m.tired)/float64(m.starters),
		float64(m.conditionSum)/float64(m.starters), m.lowest, m.lowestRested)
	for i, label := range []string{"under 7 days", "7 to under 21 days", "21+ days or first match"} {
		if n := m.startsByRest[i]; n > 0 {
			t.Logf("%s: %d starts, %.2f%% tired, mean %.2f", label, n, 100*float64(m.tiredByRest[i])/float64(n), float64(m.conditionByRest[i])/float64(n))
		}
	}
	return m
}

// Preserve medical.Version 4's weekly-rest stress scenario. Its separate
// congestion floor covers the four-day cup turnaround and developed stamina
// in generation v9; the default football year has its own measurements.
func TestInjuryAndConditionLevelsOverSeasons(t *testing.T) {
	m := measureMedicalLevels(t, worldWithSeedAndRoundInterval(t, 7, sim.Week))
	if perHundred := m.injuries * 100 / m.playerSeasons; perHundred < 30 || perHundred > 100 {
		t.Errorf("%d injuries per 100 player-seasons, want 30-100", perHundred)
	}
	if perHundred := m.out * 100 / m.kickoffs; perHundred < 40 || perHundred > 250 || m.mostOut > 10 {
		t.Errorf("%d players out per 100 club kickoffs (most %d), want 40-250 and never more than 10", perHundred, m.mostOut)
	}
	if pct := m.tired * 100 / m.starters; pct < 15 || pct > 70 || m.lowest < 25 || m.lowestRested < 40 {
		t.Errorf("%d%% of starters below full condition at kickoff (lowest %d, weekly-rest lowest %d), want 15-70%%, none below 25 and weekly-rest starters at least 40", pct, m.lowest, m.lowestRested)
	}
}

func TestMedicalLevelsOnTheFootballYear(t *testing.T) {
	for _, seed := range []uint64{7, 42, 2026} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			w := newWorld(t, seed)
			m := measureMedicalLevels(t, w)
			// Loose availability bounds; these cover whole squads rather than
			// just the regular starters who bear most of the injury risk.
			if rate := m.injuries * 100 / m.playerSeasons; rate < 30 || rate > 100 {
				t.Errorf("%d injuries per 100 player-seasons, want 30-100", rate)
			}
			if rate := m.out * 100 / m.kickoffs; rate < 15 || rate > 100 || m.mostOut > 10 || m.emergencies != 0 {
				t.Errorf("%d out per 100 club kickoffs, maximum %d, %d emergency starts", rate, m.mostOut, m.emergencies)
			}
			if pct := m.tired * 100 / m.starters; m.tired == 0 || pct > 10 || m.lowest < 60 {
				t.Errorf("%d%% tired starts, minimum %d: want some fatigue, at most 10%% tired and minimum 60", pct, m.lowest)
			}
			// Three-week breaks should restore players; short turnarounds
			// should still leave selection a meaningful condition penalty.
			for i, n := range m.startsByRest {
				if n < 100 {
					t.Fatalf("rest bucket %d has only %d starts", i, n)
				}
			}
			if short, long := m.tiredByRest[0]*100/m.startsByRest[0], m.tiredByRest[2]*100/m.startsByRest[2]; short < 30 || long > 5 || short <= long {
				t.Errorf("short-turnaround starts %d%% tired, long-rest starts %d%%: want at least 30%% and at most 5%% respectively", short, long)
			}
		})
	}
}

// The medical view agrees with the stores it reads: every injured player is
// listed with a return that the daily recovery then honours, positions add
// up to the squad, and a squad too hurt to field a team says so.
func TestSquadMedicalReportsInjuriesAndForecastsReturns(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	if _, ok := w.SquadMedical(0); ok {
		t.Fatal("an unknown club has a medical view")
	}
	m, _ := w.SquadMedical(userClub)
	if len(m.Injured) != 0 || m.Fit != len(w.employment.Squad(team)) || !m.CanField {
		t.Fatalf("fresh squad: %d injured, %d fit", len(m.Injured), m.Fit)
	}

	squad := w.employment.Squad(team)
	injure(t, w, squad[0], 2)
	injure(t, w, squad[1], 5)
	m, _ = w.SquadMedical(userClub)
	if len(m.Injured) != 2 || m.Injured[0].Player != squad[1] || m.Injured[0].DaysOut != 5 {
		t.Fatalf("injured %+v, want the five-day layoff first", m.Injured)
	}
	total, injured := 0, 0
	for _, p := range m.Positions {
		total += p.Squad
		injured += p.Injured
		if p.Fit != p.Squad-p.Injured || p.Short != (p.Fit < p.Count) || p.Critical != (p.Fit < p.Min) {
			t.Fatalf("position %+v is inconsistent", p)
		}
	}
	if total != len(squad) || injured != 2 || m.Fit != len(squad)-2 {
		t.Fatalf("positions hold %d players, %d injured, %d fit of %d", total, injured, m.Fit, len(squad))
	}

	// The forecast is exact when nothing else happens: fit the instant it
	// names, not the recovery before.
	for _, inj := range m.Injured {
		if d, hurt := w.Injury(inj.Player); !hurt || d != inj.DaysOut {
			t.Fatalf("player %d: view says %d days, store %d (%t)", inj.Player, inj.DaysOut, d, hurt)
		}
	}
	fitFrom := m.Injured[1].FitFrom // the two-day layoff
	mustContinue(t, w, fitFrom-1)
	if _, hurt := w.Injury(squad[0]); !hurt {
		t.Fatal("fit before the forecast")
	}
	mustContinue(t, w, fitFrom)
	if _, hurt := w.Injury(squad[0]); hurt {
		t.Fatalf("still injured at the forecast %d", fitFrom)
	}

	// Every goalkeeper hurt: the squad cannot field a team without them.
	for _, id := range squad {
		if _, hurt := w.Injury(id); hurt {
			continue
		}
		if p, _ := w.players.Profile(id); p.Position == players.Goalkeeper {
			injure(t, w, id, 10)
		}
	}
	m, _ = w.SquadMedical(userClub)
	if m.CanField {
		t.Fatal("a squad with every goalkeeper hurt can field a team")
	}
	for _, p := range m.Positions {
		if p.Position == players.Goalkeeper && (p.Fit != 0 || !p.Critical) {
			t.Fatalf("goalkeepers %+v, want none fit and critical", p)
		}
	}
}
