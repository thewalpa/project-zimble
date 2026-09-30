package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Over several seasons every team is in exactly one league each season, each
// league keeps its size, and the entrants follow the decided movement: the
// plain swap (NextEntrants) for a league with no link, and its group's
// play-off outcomes (NextEntrantsAfterPlayoffs) otherwise.
func TestPromotionKeepsEveryTeamInOneLeague(t *testing.T) {
	w := newWorld(t, 42)
	for range 4 {
		playSeason(t, w)
	}
	current := w.leagues[0].season.Season
	if current != 5 {
		t.Fatalf("season %d after four seasons", current)
	}
	moved := false
	for s := competitions.Season(1); s <= current; s++ {
		seen := map[ids.TeamID]ids.CompetitionID{}
		for _, l := range w.leagues {
			ref := competitions.SeasonRef{Competition: l.def.ID, Season: s}
			entrants, ok := w.competitions.Entrants(ref)
			if !ok || len(entrants) != l.def.Entrants {
				t.Fatalf("%s has %d entrants", ref, len(entrants))
			}
			for _, team := range entrants {
				if other, dup := seen[team]; dup {
					t.Fatalf("season %d: team %d in leagues %d and %d", s, team, other, l.def.ID)
				}
				seen[team] = l.def.ID
			}
		}
		if len(seen) != 32 {
			t.Fatalf("season %d has %d teams", s, len(seen))
		}
		if s == current {
			break
		}
		for _, l := range w.movementLinks() {
			comp, ok := w.playoffCompetition(l)
			if !ok {
				t.Fatalf("link %+v has no play-off", l)
			}
			if ref := (competitions.SeasonRef{Competition: comp, Season: s}); !w.competitions.SeasonCompleted(ref) {
				t.Fatalf("%s not decided", ref)
			}
		}
		for _, l := range w.leagues {
			want, err := w.entrantsAfter(l.def.ID, s)
			if err != nil {
				t.Fatal(err)
			}
			next := competitions.SeasonRef{Competition: l.def.ID, Season: s + 1}
			got, _ := w.competitions.Entrants(next)
			if !slices.Equal(got, want) {
				t.Fatalf("season %d league %d entrants %v, want %v", s+1, l.def.ID, got, want)
			}
			e1, _ := w.competitions.Entrants(competitions.SeasonRef{Competition: l.def.ID, Season: s})
			moved = moved || !slices.Equal(e1, got)
		}
	}
	if !moved {
		t.Fatal("no team ever changed division")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Movement is part of the save: the links are pinned, and a season whose
// entrants do not follow the rankings, or links the leagues cannot carry, are
// rejected.
func TestPromotionSaveRejections(t *testing.T) {
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playSeason(t, w)
		return w.Snapshot()
	}
	if got := build().Promotions; !slices.Equal(got, content.DefaultPromotions()) {
		t.Fatalf("snapshot links %v", got)
	}
	// flipPlayoffOutcome reverses the first play-off tie's score (and its
	// shoot-out), a well-formed result with the other team through.
	flipPlayoffOutcome := func(s *WorldSnapshot) {
		for i := range s.Competitions.Seasons {
			se := &s.Competitions.Seasons[i]
			if se.Format != competitions.FormatTies {
				continue
			}
			r := &se.Results[0]
			r.HomeGoals, r.AwayGoals = r.AwayGoals, r.HomeGoals
			r.HomePenalties, r.AwayPenalties = r.AwayPenalties, r.HomePenalties
			return
		}
	}
	// dropPlayoffTies removes the play-off editions entirely.
	dropPlayoffTies := func(s *WorldSnapshot) {
		kept := make([]competitions.SeasonSnapshot, 0, len(s.Competitions.Seasons))
		for _, se := range s.Competitions.Seasons {
			if se.Format != competitions.FormatTies {
				kept = append(kept, se)
			}
		}
		s.Competitions.Seasons = kept
	}
	// markPlayoffsUndecided puts the first play-off back to a round awaiting
	// results, as if the next season had been created before it was played.
	markPlayoffsUndecided := func(s *WorldSnapshot) {
		for i := range s.Competitions.Seasons {
			se := &s.Competitions.Seasons[i]
			if se.Format != competitions.FormatTies {
				continue
			}
			se.Results = nil
			se.Rounds[0].Status = competitions.RoundScheduled
			return
		}
	}
	cases := map[string]func(s *WorldSnapshot){
		"link to a missing league":                 func(s *WorldSnapshot) { s.Promotions[0].Lower = 9 },
		"too many places":                          func(s *WorldSnapshot) { s.Promotions[0].Places = 5 },
		"linked calendars differ":                  func(s *WorldSnapshot) { s.Leagues[2].Definition.RoundInterval *= 2 },
		"links removed after the play-offs":        func(s *WorldSnapshot) { s.Promotions = nil },
		"places changed after the play-offs":       func(s *WorldSnapshot) { s.Promotions[0].Places = 1 },
		"play-off outcome flipped":                 flipPlayoffOutcome,
		"play-off ties missing":                    dropPlayoffTies,
		"next season before the play-offs decided": markPlayoffsUndecided,
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		// Re-signed, so the rule under test rejects it, not the fingerprint.
		snap.ContentFingerprint = contentFingerprint(snap.Content, leagueDefsOf(&snap), snap.Cups, snap.Promotions)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}
