package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/careers"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/players"
)

// commitMoves applies employment changes that a test makes outside any
// workflow as one commit with their events, as a workflow would: a
// ContractExpired for each departure and a PlayerSigned for each signing,
// caused by the last allocated task. The journal, the inbox and the careers
// stay consistent with employment.
func commitMoves(t *testing.T, w *World, changes employment.Changes) {
	t.Helper()
	plan, err := w.employment.Plan(changes)
	if err != nil {
		t.Fatal(err)
	}
	left := map[ids.PlayerID]employment.Assignment{}
	for _, p := range changes.Departures {
		left[p], _ = w.employment.Assignment(p)
	}
	w.applyEmployment(plan)
	w.revision++
	cause := taskCause(w.scheduler.Snapshot().LastTaskID)
	for _, p := range changes.Departures {
		a := left[p]
		w.emit(w.Now(), cause, events.Event{Kind: events.KindContractExpired, ContractExpired: &events.ContractExpired{Player: p, Club: a.Club, Team: a.Team}})
	}
	for _, a := range changes.Signings {
		w.emit(w.Now(), cause, events.Event{Kind: events.KindPlayerSigned, PlayerSigned: &events.PlayerSigned{
			Player: a.Player, Club: a.Club, Team: a.Team, Expires: a.Contract.Expires, WeeklyWage: a.Contract.WeeklyWage,
		}})
	}
	w.publish()
}

// A release and a transfer end spells and start one; the careers survive a
// save and outlive the journal.
func TestCareersRecordReleasesAndTransfers(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	releasedAt := w.Now()
	released := longestContract(t, w, players.Forward).Player
	if _, err := w.ReleasePlayer(releaseCmd(w, released)); err != nil {
		t.Fatal(err)
	}
	target := bestAt(t, w, players.Forward, userClub3)
	seller := clubOf(w, target.Player)
	bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, day)
	if clubOf(w, target.Player) != userClub3 {
		t.Fatal("the transfer did not complete")
	}

	want := map[ids.PlayerID][]careers.Spell{
		released: {{Club: userClub3, Joined: careers.JoinedAtStart, Until: releasedAt, Left: careers.LeftReleased}},
		target.Player: {
			{Club: seller, Joined: careers.JoinedAtStart, Until: day, Left: careers.LeftTransfer},
			{Club: userClub3, From: day, Joined: careers.JoinedTransfer, Fee: target.Value},
		},
	}
	check := func(w *World) {
		t.Helper()
		for player, spells := range want {
			got, ok := w.PlayerCareer(player)
			var plain []careers.Spell
			for _, s := range got {
				if l, _ := w.ClubLabel(s.Club); s.ClubName == "" || s.ClubName != l.ClubName {
					t.Fatalf("player %d: spell %+v has the wrong club name", player, s)
				}
				plain = append(plain, s.Spell)
			}
			if !ok || !reflect.DeepEqual(plain, spells) {
				t.Fatalf("player %d: career %+v, want %+v", player, got, spells)
			}
		}
	}
	check(w)
	r := roundTrip(t, w)
	check(r)

	// Trim the journal: the careers keep what the events no longer show.
	defer func(n int) { journalRetention = n }(journalRetention)
	journalRetention = 5
	mustContinue(t, r, 3*day)
	if evs := r.Events(); len(evs) == 0 || evs[0].ID == 1 {
		t.Fatal("the journal was not trimmed")
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	check(r)
	check(roundTrip(t, r))

	if _, ok := r.PlayerCareer(0); ok {
		t.Fatal("career of an unknown player")
	}
}

// A save whose careers disagree with the journal or with employment is
// rejected.
func TestRestoreRejectsInvalidCareers(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, day)
	bought := func(s *WorldSnapshot) *careers.Career {
		for i, c := range s.Careers.Careers {
			if c.Player == target.Player {
				return &s.Careers.Careers[i]
			}
		}
		t.Fatal("no career for the bought player")
		return nil
	}
	cases := map[string]func(*WorldSnapshot){
		"offset behind the journal": func(s *WorldSnapshot) { s.Careers.Offset-- },
		"career missing":            func(s *WorldSnapshot) { s.Careers.Careers = s.Careers.Careers[1:] },
		"current club differs": func(s *WorldSnapshot) {
			c := bought(s)
			c.Spells[1].Club = c.Spells[0].Club + 1
			if c.Spells[1].Club == userClub3 {
				c.Spells[1].Club++
			}
		},
		"fee differs from the journal": func(s *WorldSnapshot) { bought(s).Spells[1].Fee++ },
		"transfer forgotten": func(s *WorldSnapshot) {
			c := bought(s)
			c.Spells = []careers.Spell{{Club: userClub3, Joined: careers.JoinedAtStart}}
		},
		"unknown club": func(s *WorldSnapshot) { s.Careers.Careers[0].Spells[0].Club = 999 },
	}
	for name, mutate := range cases {
		snap := w.Snapshot()
		mutate(&snap)
		r, err := Restore(snap)
		if err == nil || r != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
}

// Every completed match names who played and scored: a command-resolved
// match's facts are its report's, and the careers count exactly the
// journal's appearances and goals, at the club each player was at.
func TestCareersCountAppearancesAndGoals(t *testing.T) {
	w := userWorld(t, 42, userClub)
	reported := 0
	for range 3 {
		readyBatch(t, w)
		res := resolveNow(t, w)
		for _, m := range res.Matches {
			appeared, scorers, lineups := reportFacts(m)
			e := matchCompleted(t, w, m.Fixture)
			if !lineups || !slices.Equal(e.Appeared, appeared) || !slices.Equal(e.Scorers, scorers) {
				t.Fatalf("fixture %d: event %v / %v, report %v / %v", m.Fixture, e.Appeared, e.Scorers, appeared, scorers)
			}
			reported++
		}
	}
	if n := checkCareerCounts(t, w); n != reported {
		t.Fatalf("%d matches, %d reported", n, reported)
	}
	checkCareerCounts(t, roundTrip(t, w))

	// Without a managed club, Continue auto-resolves every batch; its
	// matches name who played and scored the same way.
	auto := userWorld(t, 42, 0)
	mustContinue(t, auto, 6*week)
	if n := checkCareerCounts(t, auto); n == 0 {
		t.Fatal("no auto-resolved matches")
	}
}

// checkCareerCounts checks that every match in a complete journal names at
// least two elevens and that each player's spells sum to the journal's
// appearances and goals. It returns the number of matches.
func checkCareerCounts(t *testing.T, w *World) int {
	t.Helper()
	evs := w.Events()
	if len(evs) == 0 || evs[0].ID != 1 {
		t.Fatal("the journal was trimmed")
	}
	apps, goals := map[ids.PlayerID]int{}, map[ids.PlayerID]int{}
	matches := 0
	for _, e := range evs {
		if p := e.MatchCompleted; p != nil {
			matches++
			if len(p.Appeared) < 22 {
				t.Fatalf("fixture %d: %d players appeared", p.Fixture, len(p.Appeared))
			}
			for _, id := range p.Appeared {
				apps[id]++
			}
			for _, id := range p.Scorers {
				goals[id]++
			}
		}
	}
	for _, c := range w.careers.Careers() {
		a, g := 0, 0
		for _, s := range c.Spells {
			a, g = a+int(s.Appearances), g+int(s.Goals)
		}
		if a != apps[c.Player] || g != goals[c.Player] {
			t.Fatalf("player %d: career %d apps %d goals, journal %d and %d", c.Player, a, g, apps[c.Player], goals[c.Player])
		}
		delete(apps, c.Player)
	}
	if len(apps) != 0 {
		t.Fatalf("players %v appeared without a career", apps)
	}
	return matches
}

func matchCompleted(t *testing.T, w *World, fixture ids.FixtureID) *events.MatchCompleted {
	t.Helper()
	for _, e := range w.Events() {
		if p := e.MatchCompleted; p != nil && p.Fixture == fixture {
			return p
		}
	}
	t.Fatalf("no MatchCompleted for fixture %d", fixture)
	return nil
}
