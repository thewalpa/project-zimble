package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

func listCmd(w *World, player ids.PlayerID, asking money.Money) ListPlayer {
	return ListPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player, Asking: asking}
}

// eventsOfRevision returns the kinds of the events a commit emitted.
func eventsOfRevision(w *World, r Revision) []events.Kind {
	var out []events.Kind
	for _, e := range w.Events() {
		if e.Revision == uint64(r) {
			out = append(out, e.Kind)
		}
	}
	return out
}

// The manager lists a player at an asking price, changes the price, and
// takes him off the list; the squad view, the transfer list and the events
// follow, and retries return the recorded result, also after a save.
func TestManagerListsAPlayer(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	squad, _ := w.Squad(userClub3)
	p := squad[0]
	value := p.Value
	messages := len(w.Inbox())
	cmd := listCmd(w, p.Player, value/2)
	res, err := w.ListPlayer(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if res != (PlayerListed{Command: cmd.ID, Revision: w.Revision(), Player: p.Player, Asking: value / 2}) {
		t.Fatalf("result %+v", res)
	}
	if l, ok := w.transfers.Listing(p.Player); !ok || l != (transfers.Listing{Player: p.Player, Club: userClub3, Asking: value / 2, ListedAt: w.Now()}) {
		t.Fatalf("listing %+v, %v", l, ok)
	}
	if row := w.squadPlayer(p.Player); !row.Listed || row.Value != value/2 {
		t.Fatalf("squad row %+v", row)
	}
	if list := w.TransferList(); len(list) != 1 || list[0].Player != p.Player || list[0].Club != userClub3 || list[0].ClubName == "" || list[0].Value != value/2 {
		t.Fatalf("transfer list %+v", list)
	}
	listed := w.Events()[len(w.Events())-1]
	if listed.Kind != events.KindPlayerListed || listed.Cause != commandCause(cmd.ID) ||
		*listed.PlayerListed != (events.PlayerListed{Player: p.Player, Club: userClub3, Team: mustUserTeam(t, w), Asking: value / 2}) {
		t.Fatalf("event %+v", listed)
	}
	if again, err := w.ListPlayer(cmd); err != nil || again != res {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if n := len(w.Inbox()); n != messages {
		t.Fatalf("listing changed the inbox: %d messages, had %d", n, messages)
	}
	w = roundTrip(t, w)
	if again, err := w.ListPlayer(cmd); err != nil || again != res {
		t.Fatalf("retry after loading: %+v %v", again, err)
	}

	// A new price replaces the listing: one PlayerListed event.
	mustContinue(t, w, day)
	relist := listFor(t, w, p.Player, value)
	if l, _ := w.transfers.Listing(p.Player); l.Asking != value || l.ListedAt != day {
		t.Fatalf("relisted %+v", l)
	}
	if kinds := eventsOfRevision(w, relist.Revision); !slices.Equal(kinds, []events.Kind{events.KindPlayerListed}) {
		t.Fatalf("relisting emitted %v", kinds)
	}
	// Asking zero takes him off the list.
	unlist := listFor(t, w, p.Player, 0)
	if _, ok := w.transfers.Listing(p.Player); ok || w.squadPlayer(p.Player).Listed || w.squadPlayer(p.Player).Value != value {
		t.Fatal("the player is still listed")
	}
	if kinds := eventsOfRevision(w, unlist.Revision); !slices.Equal(kinds, []events.Kind{events.KindPlayerUnlisted}) {
		t.Fatalf("unlisting emitted %v", kinds)
	}
	if err := roundTrip(t, w).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestListingRejectionsChangeNothing(t *testing.T) {
	type setup struct {
		w   *World
		cmd ListPlayer
	}
	fresh := func(t *testing.T) setup {
		w := userWorld(t, 42, userClub3)
		squad, _ := w.Squad(userClub3)
		return setup{w, listCmd(w, squad[0].Player, squad[0].Value)}
	}
	cases := []struct {
		name string
		want error
		edit func(*testing.T, *setup)
	}{
		{"zero ID", ErrInvalidCommand, func(_ *testing.T, s *setup) { s.cmd.ID = 0 }},
		{"stale revision", ErrStaleRevision, func(_ *testing.T, s *setup) { s.cmd.ExpectedRevision++ }},
		{"no user club", ErrNoUserClub, func(t *testing.T, s *setup) { s.w = newWorld(t, 42) }},
		{"another club's player", ErrNotUserPlayer, func(t *testing.T, s *setup) {
			s.cmd.Player = bestAt(t, s.w, players.Forward, userClub3).Player
		}},
		{"unknown player", ErrNotUserPlayer, func(_ *testing.T, s *setup) { s.cmd.Player = 99_999 }},
		{"free agent", ErrNotUserPlayer, func(t *testing.T, s *setup) { s.cmd.Player = release(t, s.w, userClub3, players.Forward) }},
		{"negative price", ErrInvalidPrice, func(_ *testing.T, s *setup) { s.cmd.Asking = -1 }},
		{"unlisting an unlisted player", ErrNotListed, func(_ *testing.T, s *setup) { s.cmd.Asking = 0 }},
		{"last day of the window", ErrWindowClosed, func(t *testing.T, s *setup) {
			mustContinue(t, s.w, s.w.TransferWindow().BidsClose)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"after the close", ErrWindowClosed, func(t *testing.T, s *setup) {
			mustContinue(t, s.w, s.w.TransferWindow().Closes)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"squad at its minimum", ErrSquadMinimum, func(t *testing.T, s *setup) {
			release(t, s.w, userClub3, players.Goalkeeper) // 3 -> 2, the minimum
			s.cmd.Player = longestContract(t, s.w, players.Goalkeeper).Player
		}},
		{"moved in this window", ErrNotTransferable, func(t *testing.T, s *setup) {
			release(t, s.w, userClub3, players.Forward)
			target := bestAt(t, s.w, players.Forward, userClub3)
			bidFor(t, s.w, target.Player, target.Value)
			mustContinue(t, s.w, day)
			if clubOf(s.w, target.Player) != userClub3 {
				t.Fatal("the purchase did not complete")
			}
			s.cmd = listCmd(s.w, target.Player, target.Value)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fresh(t)
			c.edit(t, &s)
			before := s.w.Snapshot()
			if _, err := s.w.ListPlayer(s.cmd); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !reflect.DeepEqual(s.w.Snapshot(), before) {
				t.Fatal("the refused listing changed the world")
			}
		})
	}
}

// A listing ends without an event of its own when the player leaves: sold,
// released, or at the close of the window.
func TestListingEndsWhenThePlayerLeaves(t *testing.T) {
	t.Run("sold", func(t *testing.T) {
		w, o := bidScenario(t)(t)
		res, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true})
		if err != nil || res.Status != transfers.StatusCompleted {
			t.Fatalf("accepting: %+v %v", res, err)
		}
		if _, ok := w.transfers.Listing(o.Player); ok || slices.Contains(eventsOfRevision(w, res.Revision), events.KindPlayerUnlisted) {
			t.Fatal("the sold player is still listed, or his listing was announced as withdrawn")
		}
		assertLedgersConsistent(t, w)
	})
	t.Run("released", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		p := longestContract(t, w, players.Forward)
		listFor(t, w, p.Player, p.Value)
		res, err := w.ReleasePlayer(releaseCmd(w, p.Player))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := w.transfers.Listing(p.Player); ok {
			t.Fatal("the released player is still listed")
		}
		if kinds := eventsOfRevision(w, res.Revision); !slices.Equal(kinds, []events.Kind{events.KindPlayerReleased, events.KindLedgerPosted}) {
			t.Fatalf("release emitted %v", kinds)
		}
		assertLedgersConsistent(t, w)
	})
	t.Run("window closes", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		squad, _ := w.Squad(userClub3)
		listFor(t, w, squad[0].Player, money.Units(1_000_000_000)) // too dear for anyone
		closes := w.TransferWindow().Closes
		mustContinue(t, w, closes-day)
		if _, ok := w.transfers.Listing(squad[0].Player); !ok {
			t.Fatal("the listing ended before the close")
		}
		mustContinue(t, w, closes)
		if len(w.TransferList()) != 0 {
			t.Fatalf("%d players listed after the close", len(w.TransferList()))
		}
		for _, e := range w.Events() {
			if e.Kind == events.KindPlayerUnlisted {
				t.Fatalf("the close announced %+v", e)
			}
		}
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

// assertAIListings checks the AI clubs' transfer list against their squads:
// an AI club lists exactly the players it does not need (ai.Surplus at each
// position above its roster count, except a player who moved in this
// window), each at ai.ListingPrice of his valuation when listed.
func assertAIListings(t *testing.T, w *World) {
	t.Helper()
	want := map[ids.PlayerID]bool{}
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub {
			continue
		}
		squad, _ := w.Squad(c.ID)
		for _, q := range w.defs.Roster {
			var members []ai.Member
			for _, p := range squad {
				if p.Position == q.Position {
					members = append(members, ai.Member{Player: p.Player, Overall: p.Overall})
				}
			}
			for _, id := range ai.Surplus(members, q.Count) {
				open, _, _ := w.transferWindow(w.Now())
				want[id] = !slices.ContainsFunc(w.transfers.Offers(), func(o transfers.Offer) bool {
					return o.Player == id && o.Status == transfers.StatusCompleted && o.ClosedAt >= open
				})
			}
		}
	}
	for _, l := range w.transfers.Listings() {
		if l.Club == w.userClub {
			continue
		}
		value, err := w.valuation(l.Player, l.ListedAt)
		if err != nil || !want[l.Player] || l.Asking != ai.ListingPrice(value) {
			t.Fatalf("AI listing %+v, surplus %v, valuation %s", l, want[l.Player], value)
		}
		delete(want, l.Player)
	}
	for id, listable := range want {
		if listable {
			t.Fatalf("surplus player %d of club %d is not listed", id, clubOf(w, id))
		}
	}
}

// Through a window without a manager, AI clubs buy upgrades, list exactly
// the players these replace, and sell them to clubs that need them; a
// listed player a club needs again is taken off the list.
func TestAIClubsListThePlayersTheyReplace(t *testing.T) {
	w := newWorld(t, 42)
	closes := w.TransferWindow().Closes
	listedEver := map[ids.PlayerID]bool{}
	for d := sim.GameInstant(1); d*day < closes; d++ {
		mustContinue(t, w, d*day)
		assertAIListings(t, w)
		assertAISquadsFullOrShort(t, w)
		for _, l := range w.transfers.Listings() {
			listedEver[l.Player] = true
		}
	}
	sold := 0
	for _, o := range w.transfers.Offers() {
		if o.Status == transfers.StatusCompleted && listedEver[o.Player] {
			sold++
		}
	}
	if len(listedEver) == 0 || sold == 0 {
		t.Fatalf("%d players listed, %d of them sold", len(listedEver), sold)
	}

	// A club that loses another player at a listed player's position needs
	// him again: the next run takes him off the list.
	w = newWorld(t, 42)
	var l transfers.Listing
	for d := sim.GameInstant(1); d*day < closes && l.Player == 0; d++ {
		mustContinue(t, w, d*day)
		for _, x := range w.transfers.Listings() {
			if !slices.ContainsFunc(w.transfers.Open(), func(o transfers.Offer) bool { return o.Player == x.Player }) {
				l = x
				break
			}
		}
	}
	if l.Player == 0 {
		t.Fatal("no AI club listed a player nobody bid for")
	}
	pos := w.squadPlayer(l.Player).Position
	squad, _ := w.Squad(l.Club)
	other := slices.IndexFunc(squad, func(p SquadPlayer) bool { return p.Position == pos && p.Player != l.Player })
	plan, err := w.employment.Plan(employment.Changes{Departures: []ids.PlayerID{squad[other].Player}})
	if err != nil {
		t.Fatal(err)
	}
	w.applyEmployment(plan)
	mustContinue(t, w, w.Now()+day)
	if _, ok := w.transfers.Listing(l.Player); ok || clubOf(w, l.Player) != l.Club {
		t.Fatal("the needed player is still listed, or was sold")
	}
	i := slices.IndexFunc(w.Events(), func(e events.Event) bool {
		return e.Kind == events.KindPlayerUnlisted && e.PlayerUnlisted.Player == l.Player
	})
	if i < 0 || w.Events()[i].Cause.Kind != events.CauseTask || w.Events()[i].PlayerUnlisted.Club != l.Club {
		t.Fatal("the transfer run did not announce the withdrawal")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// assertAISquadsFullOrShort checks the bound the upgrade rule keeps during a
// window: an AI squad holds at most one player beyond its roster count in
// all, at a single position.
func assertAISquadsFullOrShort(t *testing.T, w *World) {
	t.Helper()
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub {
			continue
		}
		counts := w.squadCounts(w.competitionsTeam(c.ID))
		extra := 0
		for _, q := range w.defs.Roster {
			extra += max(0, counts[q.Position]-q.Count)
		}
		if extra > 1 {
			t.Fatalf("AI club %d holds %d players beyond its roster count: %v", c.ID, extra, counts)
		}
	}
}

// The manager buys an AI club's listed player at the asking price, below
// his valuation.
func TestManagerBuysAListedPlayerAtTheAskingPrice(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	var target ListedPlayer
	for d := sim.GameInstant(1); target.Player == 0; d++ {
		if d*day >= w.TransferWindow().BidsClose {
			t.Fatal("no AI club listed a player the manager can bid for")
		}
		mustContinue(t, w, d*day)
		for _, l := range w.TransferList() {
			if !slices.ContainsFunc(w.transfers.Open(), func(o transfers.Offer) bool { return o.Player == l.Player }) {
				target = l
				break
			}
		}
	}
	value, _ := w.valuation(target.Player, w.Now())
	if target.Value >= value {
		t.Fatalf("listed at %s, valued at %s", target.Value, value)
	}
	before := balance(t, w, userClub3)
	res := bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, w.Now()+day)
	if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusCompleted || clubOf(w, target.Player) != userClub3 {
		t.Fatalf("offer %+v", o)
	}
	if paid := before - balance(t, w, userClub3); paid < target.Value {
		t.Fatalf("paid %s for a player listed at %s", paid, target.Value)
	}
	if _, ok := w.transfers.Listing(target.Player); ok {
		t.Fatal("the bought player is still listed")
	}
	assertLedgersConsistent(t, w)
}

func TestRestoreRejectsInvalidListings(t *testing.T) {
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub3)
		squad, _ := w.Squad(userClub3)
		listFor(t, w, squad[0].Player, money.Units(1_000_000_000))
		for d := sim.GameInstant(1); len(w.TransferList()) < 2; d++ {
			mustContinue(t, w, d*day)
		}
		return w.Snapshot()
	}
	if _, err := Restore(build()); err != nil {
		t.Fatal(err)
	}
	aiListing := func(s *WorldSnapshot) int {
		return slices.IndexFunc(s.Transfers.Listings, func(l transfers.Listing) bool { return l.Club != userClub3 })
	}
	cases := map[string]func(*WorldSnapshot){
		"listed by another club": func(s *WorldSnapshot) { s.Transfers.Listings[aiListing(s)].Club = userClub3 },
		"listed in the future":   func(s *WorldSnapshot) { s.Transfers.Listings[0].ListedAt = s.Scheduler.Now + day },
		"listed twice":           func(s *WorldSnapshot) { s.Transfers.Listings[1].Player = s.Transfers.Listings[0].Player },
		"a moved player listed": func(s *WorldSnapshot) {
			for _, o := range s.Transfers.Offers {
				if o.Status == transfers.StatusCompleted {
					l := transfers.Listing{Player: o.Player, Club: o.Buyer, Asking: o.Fee, ListedAt: o.ClosedAt}
					i, _ := slices.BinarySearchFunc(s.Transfers.Listings, o.Player, func(l transfers.Listing, id ids.PlayerID) int { return int(l.Player) - int(id) })
					s.Transfers.Listings = slices.Insert(s.Transfers.Listings, i, l)
					return
				}
			}
			t.Fatal("no completed offer")
		},
		"listing record for another price": func(s *WorldSnapshot) { s.ListingCommands[0].Request.Asking++ },
		"negative listing record": func(s *WorldSnapshot) {
			s.ListingCommands[0].Request.Asking, s.ListingCommands[0].Result.Asking = -1, -1
		},
		"listing event edited": func(s *WorldSnapshot) {
			for i, e := range s.Events {
				if e.Kind == events.KindPlayerListed && e.Cause.Kind == events.CauseCommand {
					s.Events[i].PlayerListed.Asking++
					return
				}
			}
		},
		"a run listed the manager's player": func(s *WorldSnapshot) {
			for i, e := range s.Events {
				if e.Kind == events.KindPlayerListed && e.Cause.Kind == events.CauseTask {
					team := s.Events[slices.IndexFunc(s.Events, func(e events.Event) bool { return e.Cause.Kind == events.CauseCommand })].PlayerListed.Team
					s.Events[i].PlayerListed.Club, s.Events[i].PlayerListed.Team = userClub3, team
					return
				}
			}
		},
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Thirty years of an AI-only market: every window moves players, empties
// the transfer list at its close and leaves every AI squad full; transfer
// fees always sum to zero, the population stays put and the world
// validates every year.
func TestAIMarketKeepsSquadsFullForDecades(t *testing.T) {
	if testing.Short() {
		t.Skip("thirty seasons")
	}
	w := newWorld(t, 7)
	offers := 0
	for year := 1; year <= 30; year++ {
		// Seasons start a day earlier each year (52 weeks), so from about
		// year 28 the first round kicks off inside the window.
		playUntil(t, w, func(w *World) sim.GameInstant { return w.TransferWindow().Closes })
		completed := 0
		for _, o := range w.transfers.Offers()[offers:] {
			if o.Status == transfers.StatusCompleted {
				completed++
			}
		}
		offers = len(w.transfers.Offers())
		if completed == 0 || len(w.TransferList()) != 0 {
			t.Fatalf("year %d: %d transfers, %d players listed after the close", year, completed, len(w.TransferList()))
		}
		assertAISquadsFull(t, w)
		var fees money.Money
		for _, e := range w.finance.All() {
			if e.Kind == finance.KindTransfer {
				fees += e.Amount
			}
		}
		if fees != 0 {
			t.Fatalf("year %d: transfer fees sum to %s", year, fees)
		}
		if s := w.Summary(); s.Players < 600 || s.Players > 640+s.Clubs+s.FreeAgents {
			t.Fatalf("year %d: %d active players, %d free agents", year, s.Players, s.FreeAgents)
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("year %d: %v", year, err)
		}
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
	}
}

// AI clubs look at the transfer list first: the manager's best player,
// listed at a nominal price, draws a bid at the first run from the first
// club to act (club 1), over every larger improvement off the list, and
// only that one (clubs leave a player another club has bid for alone).
func TestAIClubsLookAtTheListFirst(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	squad, _ := w.Squad(userClub3)
	best := slices.MaxFunc(squad, func(a, b SquadPlayer) int { return a.Overall - b.Overall })
	listFor(t, w, best.Player, money.Units(10_000))
	mustContinue(t, w, day)
	var bids []transfers.Offer
	for _, o := range w.transfers.Open() {
		if o.Player == best.Player {
			bids = append(bids, o)
		}
	}
	if len(bids) != 1 || bids[0].Buyer != 1 || bids[0].Fee != money.Units(10_000) || bids[0].MadeAt != day {
		t.Fatalf("bids for the listed player: %+v", bids)
	}
}

// An AI club still holding a player beyond its roster count (its listed
// player went unsold) lists him and buys no upgrade until he is gone, so no
// AI squad grows beyond one extra player.
func TestAClubWithSurplusBuysNoUpgrade(t *testing.T) {
	w := newWorld(t, 42)
	// Move a defender from club 2 to club 1, as if club 1 had bought him in
	// an earlier window.
	squad, _ := w.Squad(2)
	i := slices.IndexFunc(squad, func(p SquadPlayer) bool { return p.Position == players.Defender })
	a, _ := w.employment.Assignment(squad[i].Player)
	team, _ := w.registry.SeniorTeam(1)
	plan, err := w.employment.Plan(employment.Changes{
		Departures: []ids.PlayerID{a.Player},
		Signings:   []employment.Assignment{{Player: a.Player, Club: 1, Team: team, Contract: a.Contract}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.applyEmployment(plan)
	mustContinue(t, w, day)
	assertAIListings(t, w)
	if !slices.ContainsFunc(w.transfers.Listings(), func(l transfers.Listing) bool { return l.Club == 1 }) {
		t.Fatal("club 1 listed no one")
	}
	for _, o := range w.transfers.Offers() {
		if o.Buyer == 1 {
			t.Fatalf("club 1 bid %+v while holding a surplus", o)
		}
	}
	for d := sim.GameInstant(2); d*day < w.TransferWindow().Closes; d++ {
		mustContinue(t, w, d*day)
		assertAISquadsFullOrShort(t, w)
	}
}
