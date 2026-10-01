package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/players"
)

func observe(t *testing.T, w *World, club ids.ClubID, requested []ids.PlayerID) ClubObservations {
	t.Helper()
	got, err := w.ObservePlayers(club, requested)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The observing club, rather than the human controller, chooses knowledge.
// Swapping the controller in an otherwise identical starting world preserves
// every observed fact, without consulting the chosen human club.
func TestClubObservationsAreControllerIndependent(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	var requested []ids.PlayerID
	for _, p := range w.registry.Players() {
		requested = append(requested, p.ID)
	}
	for _, observer := range []ids.ClubID{1, userClub3} {
		want := observe(t, w, observer, requested)
		for _, controller := range []ids.ClubID{0, 1, userClub3} {
			other := userWorld(t, 42, controller)
			a, b := w.Snapshot(), other.Snapshot()
			if !reflect.DeepEqual(a.Registry, b.Registry) || !reflect.DeepEqual(a.Players, b.Players) ||
				!reflect.DeepEqual(a.Employment, b.Employment) || !reflect.DeepEqual(a.Medical, b.Medical) {
				t.Fatal("controller comparison has different authoritative player state")
			}
			if got := observe(t, other, observer, requested); !reflect.DeepEqual(got, want) {
				t.Fatalf("club %d knowledge differs under controller %d", observer, controller)
			}
		}
	}
	// Different observing clubs currently receive the same exact knowledge.
	a, b := observe(t, w, 1, requested), observe(t, w, userClub3, requested)
	if a.Observer != 1 || b.Observer != userClub3 || !slices.Equal(a.Players, b.Players) {
		t.Fatal("the exact knowledge policy differs between clubs")
	}
}

func TestClubObservationsCanonicalizeAndRejectInvalidRequests(t *testing.T) {
	w := newWorld(t, 42)
	before := w.Snapshot()
	requested := []ids.PlayerID{21, 1, 21, 2}
	original := slices.Clone(requested)
	got := observe(t, w, 3, requested)
	var listed []ids.PlayerID
	for _, p := range got.Players {
		listed = append(listed, p.Player)
	}
	if !slices.Equal(listed, []ids.PlayerID{1, 2, 21}) || !slices.Equal(requested, original) {
		t.Fatalf("output %v, request %v; want sorted unique output and unchanged request", listed, requested)
	}
	if got.Observer != 3 || got.Revision != w.Revision() || got.AsOf != w.Now() {
		t.Fatalf("wrong observation context: %+v", got)
	}
	if empty := observe(t, w, 3, nil); len(empty.Players) != 0 || empty.Observer != 3 {
		t.Fatalf("empty request: %+v", empty)
	}
	for _, tc := range []struct {
		club      ids.ClubID
		requested []ids.PlayerID
		want      error
	}{
		{0, []ids.PlayerID{1}, ErrUnknownClub},
		{999, nil, ErrUnknownClub},
		{3, []ids.PlayerID{1, 0}, ErrUnknownPlayer},
		{3, []ids.PlayerID{1, 99999}, ErrUnknownPlayer},
	} {
		out, err := w.ObservePlayers(tc.club, tc.requested)
		if !errors.Is(err, tc.want) || !reflect.DeepEqual(out, ClubObservations{}) {
			t.Fatalf("club %d, players %v: got %+v, %v; want no partial output and %v", tc.club, tc.requested, out, err, tc.want)
		}
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("observation reads changed the world")
	}
}

// Observations are detached values. Editing them cannot change a squad view,
// an authoritative match input, the save or subsequent simulated outcomes.
func TestClubObservationsAreDetachedFromMatchInputs(t *testing.T) {
	// Managed: the batch must rest pending for the resolve commands below
	// (an unmanaged batch auto-resolves inside Continue).
	w := userWorld(t, 42, userClub3)
	ready := readyBatch(t, w)
	command := commandFor(ready, w.NextCommandID())
	input, err := w.prepareBatch(command.Rounds)
	if err != nil {
		t.Fatal(err)
	}
	control := roundTrip(t, w)
	before := w.Snapshot()
	got := observe(t, w, 3, []ids.PlayerID{1, 2})
	got.Players[0].Attributes[players.Goalkeeping] = 1
	got.Players[0].Overall = 1
	got.Players[0].Condition = 0
	got.Players[0].Name = "changed"
	got.Players[0].Contract.WeeklyWage = 1
	got.Players[0].Retired = true
	got.Players[1] = PlayerObservation{}
	inputAfter, err := w.prepareBatch(command.Rounds)
	if err != nil || !reflect.DeepEqual(input, inputAfter) || !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatalf("editing observations changed match inputs or authoritative state: %v", err)
	}
	view, _ := w.Squad(1)
	if view[0].Name == "changed" || view[0].Contract.WeeklyWage == 1 {
		t.Fatal("editing observations changed a human view")
	}
	a, err := w.ResolveRounds(command)
	if err != nil {
		t.Fatal(err)
	}
	b, err := control.ResolveRounds(command)
	if err != nil || !reflect.DeepEqual(a, b) || !reflect.DeepEqual(w.Snapshot(), control.Snapshot()) {
		t.Fatalf("reading and editing observations changed match outcomes: %v", err)
	}
}

// The projection follows committed employment and lifecycle changes, and is
// rebuilt from the same authoritative state after restore.
func TestClubObservationsFollowLifecycleAndRestore(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	released := release(t, w, userClub3, players.Forward)
	free := observe(t, w, userClub3, []ids.PlayerID{released}).Players[0]
	if free.Club != 0 || free.Team != 0 || free.Contract != (employment.Contract{}) || free.Retired || free.Demand <= 0 {
		t.Fatalf("released player's observation: %+v", free)
	}
	playSeason(t, w)
	mustContinue(t, w, playerYearTask(t, w).DueAt)
	var requested []ids.PlayerID
	var retired, employed int
	for _, p := range w.registry.Players() {
		requested = append(requested, p.ID)
	}
	got := observe(t, w, userClub3, requested)
	for _, p := range got.Players {
		if p.Retired {
			retired++
			if p.Club != 0 || p.Team != 0 || p.Condition != 0 || p.DaysOut != 0 || p.Contract != (employment.Contract{}) {
				t.Fatalf("retired player's observation: %+v", p)
			}
		} else if p.Club != 0 {
			employed++
			if p.Team == 0 || p.Contract.WeeklyWage <= 0 || p.Overall == 0 {
				t.Fatalf("employed player's observation: %+v", p)
			}
		}
	}
	if retired == 0 || employed == 0 || got.AsOf == 0 || got.Revision == 0 {
		t.Fatal("scenario did not advance the lifecycle")
	}
	if restored := observe(t, roundTrip(t, w), userClub3, requested); !reflect.DeepEqual(got, restored) {
		t.Fatal("restore changed club observations")
	}
	if free.Retired || free.Club != 0 || free.Contract != (employment.Contract{}) {
		t.Fatal("later lifecycle changes mutated a retained observation")
	}
}

// Club rows shown to a manager aggregate the observing club's knowledge,
// whoever controls it. Under today's exact policy they equal the
// administrative Summary for every observer.
func TestObservedClubsAggregateClubKnowledge(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	before := w.Snapshot()
	want := w.Summary().ClubRows
	for _, observer := range []ids.ClubID{1, userClub3} {
		for _, controller := range []ids.ClubID{0, 1, userClub3} {
			got, ok := userWorld(t, 42, controller).ObservedClubs(observer)
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("observer %d under controller %d: rows differ from the exact summary", observer, controller)
			}
		}
	}
	rows, _ := w.ObservedClubs(userClub3)
	rows[0].AverageOverall, rows[0].Positions[0].Count = 0, 99
	if again, _ := w.ObservedClubs(userClub3); !reflect.DeepEqual(again, want) {
		t.Fatal("observed club rows share storage")
	}
	for _, observer := range []ids.ClubID{0, 999} {
		if got, ok := w.ObservedClubs(observer); ok || got != nil {
			t.Fatalf("observer %d: got %v, %v; want rejection", observer, got, ok)
		}
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("observed club reads changed the world")
	}
}
