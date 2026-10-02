package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

func TestRecruitmentKnowledgeIsControllerIndependentAndReadOnly(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	at := w.ContractYearEnd()
	before := w.Snapshot()
	for _, club := range []ids.ClubID{1, userClub3} {
		want, err := w.recruitmentPlayers(club, at)
		if err != nil {
			t.Fatal(err)
		}
		for _, controller := range []ids.ClubID{0, 1, userClub3} {
			other := userWorld(t, 42, controller)
			got, err := other.recruitmentPlayers(club, at)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("club %d with controller %d: knowledge differs, %v", club, controller, err)
			}
			for _, id := range []ids.PlayerID{1, 2} {
				a, err := w.aiOffer(club, id, 2026)
				b, otherErr := other.aiOffer(club, id, 2026)
				if err != nil || otherErr != nil || a != b || a.WeeklyWage != want[id].Demand {
					t.Fatalf("club %d contract suggestions differ: %+v, %+v, %v, %v", club, a, b, err, otherErr)
				}
			}
		}
		aged := false
		for id, p := range want {
			current := observe(t, w, club, []ids.PlayerID{id}).Players[0]
			aged = aged || current.Age != p.Age
			age, err := w.age(id, at)
			if err != nil || p.Age != age {
				t.Fatalf("player %d age at decision instant: %d, want %d (%v)", id, p.Age, age, err)
			}
			current.Age = p.Age
			if current != p {
				t.Fatalf("player %d knowledge differs from club projection", id)
			}
		}
		if !aged {
			t.Fatal("test did not cross a birthday before the cohort instant")
		}
	}
	if _, err := w.recruitmentPlayers(0, at); !errors.Is(err, ErrUnknownClub) {
		t.Fatalf("unknown observer: %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("policy reads changed authoritative state")
	}
}

// Different ratings in detached cached observations stand in for future club
// knowledge. Policies must consume those ratings, while consent uses truth.
func TestMarketPoliciesUseDecidingClubKnowledge(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	target := bestAt(t, w, players.Defender, userClub3)
	free := release(t, w, 4, players.Defender)
	before := w.Snapshot()
	m, err := w.newMarket(w.Now())
	if err != nil {
		t.Fatal(err)
	}
	m.pool = w.freeAgentIDs()
	known, err := m.observations(userClub3)
	if err != nil {
		t.Fatal(err)
	}
	p := known[target.Player]
	p.Overall = 1
	known[p.Player] = p
	p = known[free]
	p.Overall = 99
	known[free] = p
	other, err := m.observations(4)
	if err != nil {
		t.Fatal(err)
	}
	p = other[free]
	p.Overall = 7
	other[free] = p
	mine, err := m.freeAgents(userClub3)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := m.freeAgents(4)
	if err != nil || len(mine) != 1 || len(theirs) != 1 || mine[0].Overall != 99 || theirs[0].Overall != 7 {
		t.Fatalf("club-specific pool: %+v, %+v, %v", mine, theirs, err)
	}
	cands, err := m.candidates(userClub3, func(id ids.PlayerID) bool { return id == target.Player })
	if err != nil || len(cands) != 1 || cands[0].Overall != 1 || cands[0].Value != target.Value {
		t.Fatalf("buyer knowledge and seller price: %+v, %v", cands, err)
	}
	if !m.joins(target.Player, userClub3) || m.average(userClub3) != w.squadAverage(userClub3) {
		t.Fatal("observed ratings changed authoritative consent inputs")
	}
	members, err := m.members(userClub3, players.Defender)
	if err != nil {
		t.Fatal(err)
	}
	id := members[0].Player
	p = known[id]
	p.Overall = 100
	known[id] = p
	members, err = m.members(userClub3, players.Defender)
	if err != nil || members[0].Overall != 100 {
		t.Fatalf("own squad comparison bypassed knowledge: %+v, %v", members, err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("detached policy inputs changed authoritative state")
	}
}

func TestMarketKnowledgeKeepsStagedMembership(t *testing.T) {
	for _, mode := range []string{"signing", "transfer"} {
		t.Run(mode, func(t *testing.T) {
			w := userWorld(t, 42, userClub3)
			var player ids.PlayerID
			var seller ids.ClubID
			var offer transfers.Offer
			if mode == "signing" {
				player = release(t, w, 1, players.Defender)
			} else {
				target := bestAt(t, w, players.Defender, userClub3)
				player, seller = target.Player, clubOf(w, target.Player)
				terms, err := w.windowTerms(userClub3, player, w.TransferWindow().Opens)
				if err != nil {
					t.Fatal(err)
				}
				offer = transfers.Offer{ID: 1, Player: player, Seller: seller, Buyer: userClub3, Fee: target.Value, Terms: terms}
			}
			before := w.Snapshot()
			m, err := w.newMarket(w.Now())
			if err != nil {
				t.Fatal(err)
			}
			m.pool = w.freeAgentIDs()
			if _, err := m.observations(userClub3); err != nil {
				t.Fatal(err)
			}
			if seller != 0 {
				if _, err := m.observations(seller); err != nil {
					t.Fatal(err)
				}
				err = m.complete(offer)
			} else {
				err = m.sign(userClub3, player)
			}
			if err != nil {
				t.Fatal(err)
			}
			members, err := m.members(userClub3, players.Defender)
			if err != nil || !slices.ContainsFunc(members, func(mem ai.Member) bool { return mem.Player == player }) {
				t.Fatalf("staged arrival absent from buyer comparison: %+v, %v", members, err)
			}
			pool, err := m.freeAgents(4) // also check a club first observed after staging
			if err != nil || slices.ContainsFunc(pool, func(f ai.FreeAgent) bool { return f.Player == player }) {
				t.Fatalf("staged arrival still available to another club: %+v, %v", pool, err)
			}
			if seller != 0 {
				members, err = m.members(seller, players.Defender)
				if err != nil || slices.ContainsFunc(members, func(mem ai.Member) bool { return mem.Player == player }) {
					t.Fatalf("staged departure still in seller comparison: %+v, %v", members, err)
				}
			}
			if !reflect.DeepEqual(w.Snapshot(), before) {
				t.Fatal("staging policy decisions changed authoritative state")
			}
		})
	}
}

func TestRecruitmentPolicySurvivesRestore(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, 1, players.Defender)
	restored := roundTrip(t, w)
	for _, career := range []*World{w, restored} {
		mustContinue(t, career, career.TransferWindow().Closes)
	}
	if !reflect.DeepEqual(w.Snapshot(), restored.Snapshot()) {
		t.Fatal("recruitment after restore changed decisions or committed facts")
	}
}

// Vacancy recruitment uses the deciding club's pool and seller's listed price:
// an unsold listing does not require a fee when a comparable free agent exists.
// A materially better listed player still attracts a bid within the club's
// budget. Planning either choice leaves the authoritative world untouched.
func TestVacancyRecruitmentComparesPoolAndListedPlayers(t *testing.T) {
	for _, tc := range []struct {
		name           string
		freeOverall    int
		reserveBlocked bool
		wantBid        bool
	}{
		{name: "comparable free agent", freeOverall: 58},
		{name: "better listed player", freeOverall: 40, wantBid: true},
		{name: "fee exceeds reserve budget", freeOverall: 40, reserveBlocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := userWorld(t, 42, userClub3)
			free := release(t, w, 1, players.Defender)
			squad, _ := w.Squad(2)
			var defenders []SquadPlayer
			for _, p := range squad {
				if p.Position == players.Defender {
					defenders = append(defenders, p)
				}
			}
			listed := slices.MinFunc(defenders, func(a, b SquadPlayer) int { return a.Overall - b.Overall })
			before := w.Snapshot()
			m, err := w.newMarket(w.TransferWindow().Opens + freeAgentGrace(w.defs.Transfers.WindowDays))
			if err != nil {
				t.Fatal(err)
			}
			m.pool = w.freeAgentIDs()
			// Isolate club 1's vacancy and one listed target; other clubs have
			// pending decisions, and other targets have already moved.
			for _, c := range w.registry.Clubs() {
				if c.ID != 1 {
					m.openBids[c.ID] = 1
				}
			}
			for id := range m.employer {
				m.moved[id] = id != listed.Player
			}
			price := money.Units(10_000)
			m.list(listed.Player, 2, price)
			known, err := m.observations(1)
			if err != nil {
				t.Fatal(err)
			}
			p := known[free]
			p.Overall = tc.freeOverall
			known[free] = p
			p = known[listed.Player]
			p.Overall = 60
			known[listed.Player] = p
			if tc.reserveBlocked {
				// Cash can cover the fee, but the policy keeps wage money aside.
				m.balances[1] = price
			}
			if err := m.aiActions(); err != nil {
				t.Fatal(err)
			}
			if tc.wantBid {
				if len(m.changes.Bids) != 1 || m.changes.Bids[0].Player != listed.Player || m.changes.Bids[0].Fee != price || len(m.jobs.Signings) != 0 {
					t.Fatalf("bids %+v, signings %+v", m.changes.Bids, m.jobs.Signings)
				}
			} else if len(m.jobs.Signings) != 1 || m.jobs.Signings[0].Player != free || len(m.changes.Bids) != 0 {
				t.Fatalf("bids %+v, signings %+v", m.changes.Bids, m.jobs.Signings)
			}
			if !reflect.DeepEqual(w.Snapshot(), before) {
				t.Fatal("recruitment planning changed the world")
			}
		})
	}
}

// The final recruitment pass fills a role with available supply even when
// another role has a larger vacancy and no available player. It never invents
// a player or empties another club to satisfy an unfillable vacancy.
func TestClosingRecruitmentFillsAvailableRoles(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	free := release(t, w, 1, players.Defender)
	m, err := w.newMarket(w.TransferWindow().Closes)
	if err != nil {
		t.Fatal(err)
	}
	m.pool = []ids.PlayerID{free}
	m.counts[1][players.Midfielder] = w.defs.Quota(players.Midfielder).Count - 2
	before := w.Snapshot()
	if err := m.fillSquads(); err != nil {
		t.Fatal(err)
	}
	if len(m.jobs.Signings) != 1 || m.jobs.Signings[0].Club != 1 || m.jobs.Signings[0].Player != free {
		t.Fatalf("signings %+v", m.jobs.Signings)
	}
	if m.counts[1][players.Defender] != w.defs.Quota(players.Defender).Count || m.counts[1][players.Midfielder] != w.defs.Quota(players.Midfielder).Count-2 {
		t.Fatalf("staged roster %+v", m.counts[1])
	}
	if len(m.pool) != 0 || !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("closing recruitment reused supply or changed the world while planning")
	}
}
