package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
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
		for _, p := range append(slices.Clone(after.Starters), after.Bench...) {
			if p.Player == hurt {
				t.Fatalf("AI team %d selects injured player %d", f.Home, hurt)
			}
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
		// Every layoff is bounded, so a quiet stretch heals everyone.
		mustContinue(t, w, w.Now()+medical.MaxInjuryDays*day)
		for _, r := range w.medical.Records() {
			if r.DaysOut > 0 {
				t.Fatalf("seed %d: player %d still out after %d days", seed, r.Player, medical.MaxInjuryDays)
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
