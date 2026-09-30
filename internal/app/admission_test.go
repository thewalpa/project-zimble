package app

import (
	"errors"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

func moveIntoClub(t *testing.T, w *World, club ids.ClubID, pos players.Position) ids.PlayerID {
	t.Helper()
	for _, c := range w.registry.Clubs() {
		if c.ID == club {
			continue
		}
		team, _ := w.registry.SeniorTeam(c.ID)
		if w.squadCounts(team)[pos] <= w.defs.Quota(pos).Min {
			continue
		}
		for _, id := range w.employment.Squad(team) {
			p, _ := w.players.Profile(id)
			if p.Position != pos {
				continue
			}
			old, _ := w.employment.Assignment(id)
			target, _ := w.registry.SeniorTeam(club)
			commitMoves(t, w, employment.Changes{
				Departures: []ids.PlayerID{id},
				Signings: []employment.Assignment{{
					Player: id, Club: club, Team: target, Contract: old.Contract,
				}},
			})
			return id
		}
	}
	t.Fatalf("no %s available to move into club %d", pos, club)
	return 0
}

func signingFreeAgent(t *testing.T, w *World, pos players.Position, except ids.ClubID) ids.PlayerID {
	t.Helper()
	for _, c := range w.registry.Clubs() {
		if c.ID == except {
			continue
		}
		team, _ := w.registry.SeniorTeam(c.ID)
		if w.squadCounts(team)[pos] > w.defs.Quota(pos).Min {
			return release(t, w, c.ID, pos)
		}
	}
	t.Fatalf("no %s free agent available", pos)
	return 0
}

func admitForActor(t *testing.T, w *World, club ids.ClubID, player ids.PlayerID) error {
	t.Helper()
	if club == w.userClub {
		_, err := w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player, Offer: suggest(t, w, player)})
		return err
	}
	m, err := w.newMarket(w.Now())
	if err != nil {
		return err
	}
	m.at = m.open
	m.pool = w.freeAgentPool()
	return m.sign(club, player)
}

func TestSharedAdmissionAllowsSurplusForHumanAndAI(t *testing.T) {
	for _, tc := range []struct {
		name string
		club ids.ClubID
	}{
		{name: "human", club: userClub3},
		{name: "AI", club: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userWorld(t, 42, userClub3)
			moveIntoClub(t, w, tc.club, players.Defender) // 21st player, above the roster target
			free := signingFreeAgent(t, w, players.Midfielder, tc.club)
			if got := squadSize(w.squadCounts(mustClubTeam(t, w, tc.club))); got != 21 {
				t.Fatalf("setup squad size = %d, want 21", got)
			}
			if err := admitForActor(t, w, tc.club, free); err != nil {
				t.Fatalf("otherwise legal signing into a 21-player squad: %v", err)
			}
		})
	}
}

func mustClubTeam(t *testing.T, w *World, club ids.ClubID) ids.TeamID {
	t.Helper()
	team, ok := w.registry.SeniorTeam(club)
	if !ok {
		t.Fatalf("club %d has no senior team", club)
	}
	return team
}

func TestSharedAdmissionRejectsFullSquadWithPositionVacancy(t *testing.T) {
	for _, tc := range []struct {
		name string
		club ids.ClubID
	}{
		{name: "human", club: userClub3},
		{name: "AI", club: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userWorld(t, 42, userClub3)
			team := mustClubTeam(t, w, tc.club)
			var keeper ids.PlayerID
			for _, id := range w.employment.Squad(team) {
				p, _ := w.players.Profile(id)
				if p.Position == players.Goalkeeper {
					keeper = id
					break
				}
			}
			commitMoves(t, w, employment.Changes{Departures: []ids.PlayerID{keeper}})
			for range 6 {
				moveIntoClub(t, w, tc.club, players.Defender)
			}
			counts := w.squadCounts(team)
			if squadSize(counts) != w.defs.SquadLimit || counts[players.Goalkeeper] >= w.defs.Quota(players.Goalkeeper).Count {
				t.Fatalf("setup counts = %v, limit %d; want full squad with goalkeeper vacancy", counts, w.defs.SquadLimit)
			}
			freeKeeper := signingFreeAgent(t, w, players.Goalkeeper, tc.club)
			before := w.employment.Assignments()
			if err := admitForActor(t, w, tc.club, freeKeeper); !errors.Is(err, ErrSquadFull) {
				t.Fatalf("full squad signing error = %v, want %v", err, ErrSquadFull)
			}
			if got := w.employment.Assignments(); len(got) != len(before) {
				t.Fatal("rejected signing changed employment")
			}
			var target ids.PlayerID
			var seller ids.ClubID
			for _, c := range w.registry.Clubs() {
				if c.ID == tc.club {
					continue
				}
				team, _ := w.registry.SeniorTeam(c.ID)
				if w.squadCounts(team)[players.Goalkeeper] <= w.defs.Quota(players.Goalkeeper).Min {
					continue
				}
				for _, id := range w.employment.Squad(team) {
					p, _ := w.players.Profile(id)
					if p.Position == players.Goalkeeper {
						target, seller = id, c.ID
						break
					}
				}
				if target != 0 {
					break
				}
			}
			m, err := w.newMarket(w.Now())
			if err != nil {
				t.Fatal(err)
			}
			m.at = m.open
			o := transfers.Offer{Player: target, Seller: seller, Buyer: tc.club, Fee: money.Units(1)}
			if err := m.complete(o); !errors.Is(err, ErrSquadFull) {
				t.Fatalf("transfer completion into full squad: %v", err)
			}
			if len(m.jobs.Signings) != 0 || len(m.postings) != 0 {
				t.Fatal("rejected transfer staged employment or money")
			}
		})
	}
}
