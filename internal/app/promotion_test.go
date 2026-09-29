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
// league keeps its size, and the entrants follow NextEntrants from the final
// rankings of the season before.
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
		rankings := map[ids.CompetitionID][]ids.TeamID{}
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
			if w.competitions.SeasonCompleted(ref) {
				rankings[l.def.ID] = w.competitions.Ranking(ref)
			}
		}
		if len(seen) != 32 {
			t.Fatalf("season %d has %d teams", s, len(seen))
		}
		if s == current {
			break
		}
		want, err := competitions.NextEntrants(rankings, w.movementLinks())
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range w.leagues {
			got, _ := w.competitions.Entrants(competitions.SeasonRef{Competition: l.def.ID, Season: s + 1})
			if !slices.Equal(got, want[l.def.ID]) {
				t.Fatalf("season %d league %d entrants %v, want %v", s+1, l.def.ID, got, want[l.def.ID])
			}
		}
		e1, _ := w.competitions.Entrants(competitions.SeasonRef{Competition: 1, Season: s})
		e2, _ := w.competitions.Entrants(competitions.SeasonRef{Competition: 1, Season: s + 1})
		moved = moved || !slices.Equal(e1, e2)
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
	cases := map[string]func(s *WorldSnapshot){
		"link to a missing league":                 func(s *WorldSnapshot) { s.Promotions[0].Lower = 9 },
		"too many places":                          func(s *WorldSnapshot) { s.Promotions[0].Places = 5 },
		"linked calendars differ":                  func(s *WorldSnapshot) { s.Leagues[2].Definition.RoundInterval *= 2 },
		"links removed after a season moved teams": func(s *WorldSnapshot) { s.Promotions = nil },
		"places changed after a season moved teams": func(s *WorldSnapshot) {
			s.Promotions[0].Places = 1
		},
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
