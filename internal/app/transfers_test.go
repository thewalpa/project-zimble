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
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

const userClub3 ids.ClubID = 3

// release frees a club's weakest player at a position (ties: lowest ID)
// directly in employment, to make room: the manager cannot release players.
// The squad must stay at or above its minimum there.
func release(t *testing.T, w *World, club ids.ClubID, pos players.Position) ids.PlayerID {
	t.Helper()
	squad, _ := w.Squad(club)
	var at []SquadPlayer
	for _, p := range squad {
		if p.Position == pos {
			at = append(at, p)
		}
	}
	if len(at) <= w.defs.Quota(pos).Min {
		t.Fatalf("club %d has only %d %s", club, len(at), pos)
	}
	weakest := slices.MinFunc(at, func(a, b SquadPlayer) int { return a.Overall - b.Overall })
	plan, err := w.employment.Plan(employment.Changes{Departures: []ids.PlayerID{weakest.Player}})
	if err != nil {
		t.Fatal(err)
	}
	w.applyEmployment(plan)
	return weakest.Player
}

// bestAt returns the best player at a position (ties: lowest ID) of a club
// other than except whose squad is above its minimum there.
func bestAt(t *testing.T, w *World, pos players.Position, except ids.ClubID) SquadPlayer {
	t.Helper()
	var best []SquadPlayer
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		if c.ID == except || w.squadCounts(team)[pos] <= w.defs.Quota(pos).Min {
			continue
		}
		squad, _ := w.Squad(c.ID)
		for _, p := range squad {
			if p.Position == pos {
				best = append(best, p)
			}
		}
	}
	if len(best) == 0 {
		t.Fatalf("no %s for sale", pos)
	}
	return slices.MinFunc(best, func(a, b SquadPlayer) int { return b.Overall - a.Overall })
}

func clubOf(w *World, p ids.PlayerID) ids.ClubID {
	a, _ := w.employment.Assignment(p)
	return a.Club
}

func bidFor(t *testing.T, w *World, player ids.PlayerID, fee money.Money) TransferOfferMade {
	t.Helper()
	res, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player, Fee: fee, Offer: suggest(t, w, player)})
	if err != nil {
		t.Fatalf("bid for player %d: %v", player, err)
	}
	return res
}

func offerOf(t *testing.T, w *World, id ids.OfferID) transfers.Offer {
	t.Helper()
	o, ok := w.transfers.Offer(id)
	if !ok {
		t.Fatalf("no offer %d", id)
	}
	return o
}

func transferTask(t *testing.T, w *World) sim.Task {
	t.Helper()
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskTransferRun {
			return task
		}
	}
	t.Fatal("no transfer-run task queued")
	return sim.Task{}
}

func totalBalance(w *World) money.Money {
	var sum money.Money
	for _, c := range w.finance.Accounts() {
		b, _ := w.finance.Balance(c)
		sum += b
	}
	return sum
}

// transferEvents returns the journal's transfer events and ledger postings.
func transferEvents(w *World) []events.Event {
	var out []events.Event
	for _, e := range w.Events() {
		switch e.Kind {
		case events.KindTransferOffered, events.KindTransferCompleted, events.KindOfferClosed:
			out = append(out, e)
		}
	}
	return out
}

func lastMessage(w *World) InboxItem {
	items := w.Inbox()
	return items[len(items)-1]
}

// The window opens at every contract-year end, the career start included,
// and lasts WindowDays; clubs answer bids at a run each day after the
// opening, up to the close.
func TestTransferWindowAndRuns(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	win := w.TransferWindow()
	closes := 28 * day
	if !win.Open || win.Opens != 0 || win.Closes != closes || win.BidsClose != closes-day || win.NextRun != day {
		t.Fatalf("window at the start %+v", win)
	}
	if task := transferTask(t, w); task.DueAt != day || task.Phase != sim.PhaseDecisions || task.PayloadID != 0 {
		t.Fatalf("transfer task %+v", task)
	}
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)

	// A bid on the last day would be answered at the close: refused.
	mustContinue(t, w, closes-day)
	before := w.Snapshot()
	if _, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
		Fee: target.Value, Offer: suggest(t, w, target.Player)}); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("bid on the last day: %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("a refused bid changed the world")
	}
	// Every run was queued exactly once, one day after the other.
	if task := transferTask(t, w); task.DueAt != closes {
		t.Fatalf("run due %d, want the close %d", task.DueAt, closes)
	}
	mustContinue(t, w, closes)
	next := w.ContractYearEnd()
	win = w.TransferWindow()
	if win.Open || win.Opens != next || win.Closes != next+closes || win.NextRun != next+day || transferTask(t, w).DueAt != next+day {
		t.Fatalf("window after the close %+v, next contract-year end %d", win, next)
	}
	if _, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
		Fee: target.Value, Offer: suggest(t, w, target.Player)}); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("bid after the close: %v", err)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	// Without a manager, full squads have no vacancies: the window passes
	// without an offer.
	ai := newWorld(t, 42)
	mustContinue(t, ai, closes+day)
	if len(ai.Offers()) != 0 || len(transferEvents(ai)) != 0 {
		t.Fatalf("an AI-only world made %d offers", len(ai.Offers()))
	}
}

// A bid at the asking price is answered at the next run: the player moves
// on the offered terms, the fee moves from the buyer's ledger to the
// seller's, and the events and inbox describe it.
func TestManagerBuysAPlayer(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	seller := clubOf(w, target.Player)
	if want, _ := w.valuation(target.Player, 0); target.Value != want || target.Value <= 0 {
		t.Fatalf("asking price %s, valuation %s", target.Value, want)
	}
	terms := suggest(t, w, target.Player)
	buyerBefore, sellerBefore, total := balance(t, w, userClub3), balance(t, w, seller), totalBalance(w)
	userTeam, sellerTeam := mustUserTeam(t, w), w.competitionsTeam(seller)

	res := bidFor(t, w, target.Player, target.Value)
	o := offerOf(t, w, res.Offer)
	if res.Deadline != day || o.Status != transfers.StatusOpen || o.Seller != seller || o.Buyer != userClub3 || o.Terms != transfers.Terms(terms) {
		t.Fatalf("result %+v, offer %+v", res, o)
	}
	evs := transferEvents(w)
	if len(evs) != 1 || evs[0].Kind != events.KindTransferOffered || evs[0].Cause != commandCause(res.Command) || evs[0].TransferOffered.Deadline != day {
		t.Fatalf("events after the bid %+v", evs)
	}
	if len(w.Inbox()) != 0 {
		t.Fatal("the manager's own bid reached the inbox")
	}

	mustContinue(t, w, day)
	o = offerOf(t, w, res.Offer)
	a, _ := w.employment.Assignment(target.Player)
	contract, _ := w.transferContract(day, o.Terms)
	switch {
	case o.Status != transfers.StatusCompleted || o.ClosedAt != day:
		t.Fatalf("offer %+v", o)
	case a.Club != userClub3 || a.Team != userTeam || a.Contract != contract:
		t.Fatalf("assignment %+v, want contract %+v", a, contract)
	case balance(t, w, userClub3) != buyerBefore-target.Value || balance(t, w, seller) != sellerBefore+target.Value:
		t.Fatal("the fee did not move from buyer to seller")
	case totalBalance(w) != total:
		t.Fatal("money was created or destroyed")
	}
	if n := w.squadCounts(userTeam)[players.Forward]; n != 4 {
		t.Fatalf("the manager has %d forwards", n)
	}
	if n := w.squadCounts(sellerTeam)[players.Forward]; n != 3 {
		t.Fatalf("the seller has %d forwards", n)
	}
	done := eventsAt(w, day, events.KindTransferCompleted)
	ledger := eventsAt(w, day, events.KindLedgerPosted)
	if len(done) != 1 || done[0].TransferCompleted.Offer != o.ID || done[0].TransferCompleted.Expires != contract.Expires ||
		len(ledger) != 1 || len(ledger[0].LedgerPosted.Entries) != 2 || ledger[0].ID != done[0].ID+1 || done[0].Cause.Kind != events.CauseTask {
		t.Fatalf("events at the run: %+v %+v", done, ledger)
	}
	for _, e := range ledger[0].LedgerPosted.Entries {
		if e.Kind != uint8(finance.KindTransfer) || e.Offer != o.ID {
			t.Fatalf("ledger entry %+v", e)
		}
	}
	m := w.Inbox()[0]
	if m.Kind != inbox.KindTransferIn || m.Player != target.Player || m.Fee != target.Value || m.Club != seller ||
		m.ClubName == "" || m.PlayerName != target.Name || m.Expires != contract.Expires {
		t.Fatalf("inbox %+v", m)
	}
	assertLedgersConsistent(t, w)
	roundTrip(t, w)
}

// competitionsTeam is a club's senior team.
func (w *World) competitionsTeam(club ids.ClubID) ids.TeamID {
	team, _ := w.registry.SeniorTeam(club)
	return team
}

// A bid below the valuation is rejected at the run; nothing moves, and the
// manager may not bid for him again in the window.
func TestBidBelowValuationIsRejected(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	seller, fin := clubOf(w, target.Player), w.finance.Snapshot()
	res := bidFor(t, w, target.Player, target.Value-money.Units(ai.ValueStepUnits))
	mustContinue(t, w, day)
	if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusRejected || o.ClosedAt != day {
		t.Fatalf("offer %+v", o)
	}
	if clubOf(w, target.Player) != seller {
		t.Fatal("the player moved")
	}
	for _, e := range w.finance.All()[len(fin.Entries):] {
		if e.Kind == finance.KindTransfer {
			t.Fatal("a rejected bid moved money")
		}
	}
	closed := eventsAt(w, day, events.KindOfferClosed)
	if len(closed) != 1 || closed[0].OfferClosed.Outcome != uint8(transfers.StatusRejected) {
		t.Fatalf("events %+v", closed)
	}
	if m := lastMessage(w); m.Kind != inbox.KindOfferClosed || m.Outcome != uint8(transfers.StatusRejected) || m.Selling || m.Club != seller {
		t.Fatalf("inbox %+v", m)
	}
	if _, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
		Fee: target.Value, Offer: suggest(t, w, target.Player)}); !errors.Is(err, ErrAlreadyBid) {
		t.Fatalf("second bid: %v", err)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Every refused bid is reported and changes nothing.
func TestTransferOfferRejectionsChangeNothing(t *testing.T) {
	type setup struct {
		w   *World
		cmd MakeTransferOffer
	}
	fresh := func(t *testing.T) setup {
		w := userWorld(t, 42, userClub3)
		release(t, w, userClub3, players.Forward)
		target := bestAt(t, w, players.Forward, userClub3)
		return setup{w, MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
			Fee: target.Value, Offer: suggest(t, w, target.Player)}}
	}
	cases := []struct {
		name string
		want error
		edit func(*testing.T, *setup)
	}{
		{"zero ID", ErrInvalidCommand, func(_ *testing.T, s *setup) { s.cmd.ID = 0 }},
		{"stale revision", ErrStaleRevision, func(_ *testing.T, s *setup) { s.cmd.ExpectedRevision++ }},
		{"no user club", ErrNoUserClub, func(t *testing.T, s *setup) { s.w = newWorld(t, 42) }},
		{"own player", ErrNotTransferable, func(t *testing.T, s *setup) {
			squad, _ := s.w.Squad(userClub3)
			s.cmd.Player = squad[0].Player
		}},
		{"free agent", ErrNotTransferable, func(t *testing.T, s *setup) { s.cmd.Player = s.w.FreeAgents()[0].Player }},
		{"unknown player", ErrNotTransferable, func(_ *testing.T, s *setup) { s.cmd.Player = 99_999 }},
		{"squad full", ErrSquadFull, func(t *testing.T, s *setup) {
			s.cmd.Player = bestAt(t, s.w, players.Defender, userClub3).Player
			s.cmd.Offer = suggest(t, s.w, s.cmd.Player)
		}},
		{"seller at its minimum", ErrSquadMinimum, func(t *testing.T, s *setup) {
			release(t, s.w, clubOf(s.w, s.cmd.Player), players.Forward)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"no fee", ErrCannotAfford, func(_ *testing.T, s *setup) { s.cmd.Fee = 0 }},
		{"fee above the balance", ErrCannotAfford, func(t *testing.T, s *setup) { s.cmd.Fee = balance(t, s.w, userClub3) + 1 }},
		{"wage below demand", ErrOfferRejected, func(_ *testing.T, s *setup) { s.cmd.Offer.WeeklyWage-- }},
		{"contract too long", ErrOfferRejected, func(_ *testing.T, s *setup) { s.cmd.Offer.Years = 5 }},
		{"already bid", ErrAlreadyBid, func(t *testing.T, s *setup) {
			bidFor(t, s.w, s.cmd.Player, s.cmd.Fee-money.Units(ai.ValueStepUnits))
			s.cmd.ID, s.cmd.ExpectedRevision = s.w.NextCommandID(), s.w.Revision()
		}},
		{"moved in this window", ErrNotTransferable, func(t *testing.T, s *setup) {
			// The seller replaces the player it sold with another club's.
			bidFor(t, s.w, s.cmd.Player, s.cmd.Fee)
			mustContinue(t, s.w, 2*day)
			var moved ids.PlayerID
			for _, o := range s.w.Offers() {
				if o.Status == transfers.StatusCompleted && o.Buyer != userClub3 {
					moved = o.Player
				}
			}
			if moved == 0 {
				t.Fatal("no AI transfer to test with")
			}
			release(t, s.w, userClub3, s.w.squadPlayer(moved).Position)
			s.cmd = MakeTransferOffer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: moved,
				Fee: money.Units(100_000), Offer: suggest(t, s.w, moved)}
		}},
		{"rounds pending", ErrSquadsLocked, func(t *testing.T, s *setup) {
			s.w.defs.Transfers.WindowDays = 60 // open past the first kickoff
			if _, ok := mustContinue(t, s.w, firstKickoff(t, s.w)).(FixtureRoundReady); !ok {
				t.Fatal("no round pending")
			}
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fresh(t)
			c.edit(t, &s)
			before := s.w.Snapshot()
			if _, err := s.w.MakeTransferOffer(s.cmd); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !reflect.DeepEqual(s.w.Snapshot(), before) {
				t.Fatal("a refused bid changed the world")
			}
		})
	}
}

// bidScenario finds, by simulation, an AI club whose vacancy leads it to
// bid for one of the manager's players at the first run: the world after
// that run and the bid. Every fresh world is the same, so the search is
// deterministic.
func bidScenario(t *testing.T) func(*testing.T) (*World, transfers.Offer) {
	t.Helper()
	for _, c := range newWorld(t, 42).registry.Clubs() {
		if c.ID == userClub3 {
			continue
		}
		for _, pos := range players.Positions() {
			build := func(t *testing.T) (*World, transfers.Offer) {
				w := userWorld(t, 42, userClub3)
				release(t, w, c.ID, pos)
				mustContinue(t, w, day)
				for _, o := range w.transfers.Open() {
					if o.Seller == userClub3 {
						return w, o
					}
				}
				return nil, transfers.Offer{}
			}
			if w, _ := build(t); w != nil {
				return func(t *testing.T) (*World, transfers.Offer) {
					w, o := build(t)
					if w == nil {
						t.Fatal("scenario not reproduced")
					}
					return w, o
				}
			}
		}
	}
	t.Fatal("no AI club bids for a manager's player")
	return nil
}

// An AI club with a vacancy bids its valuation for the manager's player;
// the manager accepts, declines or lets the bid expire.
func TestManagerAnswersBids(t *testing.T) {
	scenario := bidScenario(t)
	w, o := scenario(t)
	if value, _ := w.valuation(o.Player, day); o.Fee != value || o.MadeAt != day || o.Deadline != 4*day {
		t.Fatalf("bid %+v, valuation %s", o, value)
	}
	if m := lastMessage(w); m.Kind != inbox.KindBidReceived || m.Offer != o.ID || m.Deadline != o.Deadline || !m.Selling || m.Club != o.Buyer || m.ClubName == "" {
		t.Fatalf("inbox %+v", m)
	}

	t.Run("accept", func(t *testing.T) {
		w, o := scenario(t)
		userBefore, total := balance(t, w, userClub3), totalBalance(w)
		cmd := RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true}
		res, err := w.RespondToOffer(cmd)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := w.employment.Assignment(o.Player)
		contract, _ := w.transferContract(day, o.Terms)
		if res.Status != transfers.StatusCompleted || res.Contract != contract || a.Club != o.Buyer || a.Contract != contract {
			t.Fatalf("result %+v, assignment %+v", res, a)
		}
		if balance(t, w, userClub3) != userBefore+o.Fee || totalBalance(w) != total {
			t.Fatal("the fee did not reach the manager's ledger")
		}
		if m := lastMessage(w); m.Kind != inbox.KindTransferOut || m.Player != o.Player || m.Fee != o.Fee {
			t.Fatalf("inbox %+v", m)
		}
		evs := w.Events()
		if n := len(evs); evs[n-2].Kind != events.KindTransferCompleted || evs[n-1].Kind != events.KindLedgerPosted || evs[n-1].Cause != commandCause(cmd.ID) {
			t.Fatalf("events %+v %+v", evs[n-2], evs[n-1])
		}
		// A retry returns the recorded result and pays nothing more.
		snap := w.Snapshot()
		if again, err := w.RespondToOffer(cmd); err != nil || again != res || !reflect.DeepEqual(w.Snapshot(), snap) {
			t.Fatalf("retry: %+v %v", again, err)
		}
		if _, err := w.RespondToOffer(RespondToOffer{ID: cmd.ID, ExpectedRevision: cmd.ExpectedRevision, Offer: o.ID}); !errors.Is(err, ErrCommandIDReused) {
			t.Fatalf("reused ID: %v", err)
		}
		assertLedgersConsistent(t, roundTrip(t, w))
	})

	t.Run("decline", func(t *testing.T) {
		w, o := scenario(t)
		res, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID})
		if err != nil || res.Status != transfers.StatusRejected || res.Contract != (employment.Contract{}) || clubOf(w, o.Player) != userClub3 {
			t.Fatalf("result %+v, %v", res, err)
		}
		if m := lastMessage(w); m.Kind != inbox.KindOfferClosed || m.Outcome != uint8(transfers.StatusRejected) || !m.Selling {
			t.Fatalf("inbox %+v", m)
		}
		roundTrip(t, w)
	})

	t.Run("no answer", func(t *testing.T) {
		w, o := scenario(t)
		mustContinue(t, w, o.Deadline-1)
		if offerOf(t, w, o.ID).Status != transfers.StatusOpen {
			t.Fatal("the bid closed before its deadline")
		}
		mustContinue(t, w, o.Deadline)
		if c := offerOf(t, w, o.ID); c.Status != transfers.StatusExpired || c.ClosedAt != o.Deadline || clubOf(w, o.Player) != userClub3 {
			t.Fatalf("offer %+v", c)
		}
		if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true}); !errors.Is(err, ErrNoSuchOffer) {
			t.Fatalf("answer after expiry: %v", err)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		w, o := scenario(t)
		var aiOffer ids.OfferID // an offer between other clubs, if any, or the manager's own bid
		for _, other := range w.transfers.Open() {
			if other.Seller != userClub3 {
				aiOffer = other.ID
			}
		}
		if aiOffer == 0 {
			release(t, w, userClub3, players.Forward)
			aiOffer = bidFor(t, w, bestAt(t, w, players.Forward, userClub3).Player, money.Units(10_000)).Offer
		}
		for _, id := range []ids.OfferID{aiOffer, 99, 0} {
			before := w.Snapshot()
			if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: id, Accept: true}); !errors.Is(err, ErrNoSuchOffer) {
				t.Fatalf("offer %d: %v", id, err)
			}
			if !reflect.DeepEqual(w.Snapshot(), before) {
				t.Fatal("a refused answer changed the world")
			}
		}
		// Selling would leave the manager below the minimum there.
		pos := w.squadPlayer(o.Player).Position
		for w.squadCounts(mustUserTeam(t, w))[pos] > w.defs.Quota(pos).Min {
			release(t, w, userClub3, pos)
		}
		if clubOf(w, o.Player) != userClub3 {
			t.Skip("the bid's player was released to reach the minimum")
		}
		before := w.Snapshot()
		if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true}); !errors.Is(err, ErrSquadMinimum) {
			t.Fatalf("accept at the minimum: %v", err)
		}
		if !reflect.DeepEqual(w.Snapshot(), before) {
			t.Fatal("a refused acceptance changed the world")
		}
	})
}

// plantBid adds an open offer directly to the store, as a buyer would have
// made it now: the deadline its seller would have.
func plantBid(t *testing.T, w *World, player ids.PlayerID, buyer ids.ClubID, fee money.Money) transfers.Offer {
	t.Helper()
	m, err := w.newMarket(w.Now())
	if err != nil {
		t.Fatal(err)
	}
	terms, _ := w.windowTerms(player, m.open)
	if _, err := m.bid(player, buyer, fee, terms); err != nil {
		t.Fatal(err)
	}
	plan, err := w.transfers.Plan(w.Now(), m.changes)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.transfers.Apply(plan); err != nil {
		t.Fatal(err)
	}
	return plan.Made()[0]
}

// Once a player moves, every other open offer for him collapses in the
// same commit: when the manager accepts one of two bids, and when two bids
// for an AI club's player are answered at the same run.
func TestCompetingOffersCollapse(t *testing.T) {
	t.Run("manager accepts one of two", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		squad, _ := w.Squad(userClub3)
		player := squad[0].Player // a goalkeeper
		for _, c := range []ids.ClubID{1, 2} {
			release(t, w, c, players.Goalkeeper)
		}
		first := plantBid(t, w, player, 1, money.Units(300_000))
		second := plantBid(t, w, player, 2, money.Units(400_000))
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: second.ID, Accept: true}); err != nil {
			t.Fatal(err)
		}
		if a, b := offerOf(t, w, first.ID), offerOf(t, w, second.ID); a.Status != transfers.StatusCollapsed || b.Status != transfers.StatusCompleted || clubOf(w, player) != 2 {
			t.Fatalf("offers %+v %+v", a, b)
		}
		evs := w.Events()
		n := len(evs)
		if evs[n-3].Kind != events.KindTransferCompleted || evs[n-2].Kind != events.KindOfferClosed ||
			evs[n-2].OfferClosed.Outcome != uint8(transfers.StatusCollapsed) || evs[n-1].Kind != events.KindLedgerPosted {
			t.Fatalf("events %+v", evs[n-3:])
		}
		assertLedgersConsistent(t, roundTrip(t, w))
	})

	t.Run("two bids answered at one run", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		release(t, w, userClub3, players.Forward)
		target := bestAt(t, w, players.Forward, userClub3)
		release(t, w, 16, players.Forward)
		if clubOf(w, target.Player) == 16 {
			t.Fatal("pick another rival")
		}
		mine := bidFor(t, w, target.Player, target.Value)
		rival := plantBid(t, w, target.Player, 16, target.Value)
		mustContinue(t, w, day)
		if a, b := offerOf(t, w, mine.Offer), offerOf(t, w, rival.ID); a.Status != transfers.StatusCompleted || b.Status != transfers.StatusCollapsed || b.ClosedAt != day {
			t.Fatalf("offers %+v %+v", a, b)
		}
		assertLedgersConsistent(t, w)
	})
}

// playWindow bids for the best forward, then runs the window to its close
// one day at a time, optionally saving and loading before every day.
func playWindow(t *testing.T, save bool) *World {
	t.Helper()
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	bid := MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player, Fee: target.Value, Offer: suggest(t, w, target.Player)}
	res, err := w.MakeTransferOffer(bid)
	if err != nil {
		t.Fatal(err)
	}
	for d := sim.GameInstant(1); d <= 28; d++ {
		if save {
			w = roundTrip(t, w)
			// A retry after loading returns the recorded result.
			if again, err := w.MakeTransferOffer(bid); err != nil || again != res {
				t.Fatalf("retry after loading: %+v %v", again, err)
			}
		}
		mustContinue(t, w, d*day)
	}
	return w
}

// Saving and loading at every day of the window, and retrying the bid each
// time, continues exactly like never saving: nothing completes twice.
func TestSavesAndRetriesNeverCompleteTwice(t *testing.T) {
	a, b := playWindow(t, false), playWindow(t, true)
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("saving during the window changed its outcome")
	}
	completed := 0
	for _, o := range a.Offers() {
		if o.Status == transfers.StatusCompleted {
			completed++
		}
	}
	if completed < 2 {
		t.Fatalf("only %d transfers completed; the test needs the AI to react", completed)
	}
}

// Over a whole window of transfers the proofs hold: money is conserved
// across ledgers, every player is at one club at most and moves at most
// once, squads stay within their limits every day, every AI club buys at
// most once, and every AI squad is full again after the close.
func TestWindowConservesMoneyPlayersAndSquads(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	var registry []ids.PlayerID
	for _, p := range w.registry.Players() {
		registry = append(registry, p.ID)
	}
	for _, pos := range players.Positions() {
		release(t, w, userClub3, pos)
	}
	for _, pos := range players.Positions() {
		target := bestAt(t, w, pos, userClub3)
		if target.Value <= balance(t, w, userClub3)/2 {
			bidFor(t, w, target.Player, target.Value)
		}
	}
	for d := sim.GameInstant(1); d <= 28; d++ {
		mustContinue(t, w, d*day)
		var fees money.Money
		for _, e := range w.finance.All() {
			if e.Kind == finance.KindTransfer {
				fees += e.Amount
			}
		}
		if fees != 0 {
			t.Fatalf("day %d: transfer fees sum to %s", d, fees)
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("day %d: %v", d, err)
		}
		assertEveryPlayerOnce(t, w, registry)
	}
	bought := map[ids.ClubID]int{}
	completed := 0
	for _, o := range w.Offers() {
		if o.Status == transfers.StatusCompleted {
			completed++
			bought[o.Buyer]++
		}
		if o.Status == transfers.StatusOpen {
			t.Fatalf("offer %d is still open after the close", o.ID)
		}
	}
	for c, n := range bought {
		if c != userClub3 && n > 1 {
			t.Fatalf("AI club %d bought %d players", c, n)
		}
	}
	if completed < 3 {
		t.Fatalf("%d transfers completed", completed)
	}
	for _, c := range w.registry.Clubs() {
		counts := w.squadCounts(w.competitionsTeam(c.ID))
		for _, q := range w.defs.Roster {
			if c.ID != userClub3 && counts[q.Position] != q.Count {
				t.Fatalf("club %d has %d %s after the close", c.ID, counts[q.Position], q.Position)
			}
		}
	}
	assertLedgersConsistent(t, roundTrip(t, w))
}

// A transfer run that fails changes nothing: offers, employment, ledgers,
// events and the inbox stay as they were, and the run stays queued.
func TestFailedTransferRunChangesNothing(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	res := bidFor(t, w, target.Player, target.Value)
	// The seller's balance would overflow when the fee arrives.
	seller := clubOf(w, target.Player)
	plan, err := w.finance.Plan(w.Now(), []finance.Posting{{Club: seller, Kind: finance.KindGate, Fixture: 1,
		Amount: money.Money(1<<63-1) - balance(t, w, seller) - 1}})
	if err != nil {
		t.Fatal(err)
	}
	w.applyFinance(plan)
	offers, jobs, fin, evs, box := w.transfers.Snapshot(), w.employment.Snapshot(), w.finance.Snapshot(), w.Events(), w.inbox.Snapshot()
	if _, err := w.Continue(day); err == nil {
		t.Fatal("the run succeeded")
	}
	if !reflect.DeepEqual(w.transfers.Snapshot(), offers) || !reflect.DeepEqual(w.employment.Snapshot(), jobs) ||
		!reflect.DeepEqual(w.finance.Snapshot(), fin) || !reflect.DeepEqual(w.Events(), evs) || !reflect.DeepEqual(w.inbox.Snapshot(), box) {
		t.Fatal("the failed run changed the world")
	}
	if task := transferTask(t, w); task.DueAt != day || offerOf(t, w, res.Offer).Status != transfers.StatusOpen {
		t.Fatalf("task %+v", task)
	}
}

// The asking price is the club's valuation; the suggested terms are an AI
// club's for the contract year under way.
func TestAskingPriceAndSuggestedTerms(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	squad, _ := w.Squad(1)
	for _, p := range squad {
		age, _ := w.age(p.Player, 0)
		years, _ := w.contractYearsLeft(p.Contract.Expires, 0)
		if p.Value != ai.Valuation(p.Overall, age, years) {
			t.Fatalf("player %d asks %s", p.Player, p.Value)
		}
		if o := suggest(t, w, p.Player); o != mustAIOffer(t, w, p.Player, 2025) {
			t.Fatalf("player %d suggested %+v", p.Player, o)
		}
	}
	free := release(t, w, 1, players.Forward)
	if p := w.squadPlayer(free); p.Value != 0 {
		t.Fatalf("free agent valued at %s", p.Value)
	}
}

func mustAIOffer(t *testing.T, w *World, player ids.PlayerID, year int) ContractOffer {
	t.Helper()
	o, err := w.aiOffer(player, year)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestRestoreRejectsInvalidTransfers(t *testing.T) {
	scenario := bidScenario(t)
	build := func() WorldSnapshot {
		w, bid := scenario(t)
		// The manager declines one bid and completes a purchase.
		if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: bid.ID}); err != nil {
			t.Fatal(err)
		}
		release(t, w, userClub3, players.Forward)
		target := bestAt(t, w, players.Forward, userClub3)
		bidFor(t, w, target.Player, target.Value)
		mustContinue(t, w, 2*day)
		return w.Snapshot()
	}
	if _, err := Restore(build()); err != nil {
		t.Fatal(err)
	}
	index := func(s *WorldSnapshot, status transfers.Status, buyer ids.ClubID) int {
		for i, o := range s.Transfers.Offers {
			if o.Status == status && (buyer == 0 || o.Buyer == buyer) {
				return i
			}
		}
		t.Fatalf("no %s offer", status)
		return -1
	}
	cases := map[string]func(*WorldSnapshot){
		"fee edited": func(s *WorldSnapshot) { s.Transfers.Offers[index(s, transfers.StatusCompleted, userClub3)].Fee++ },
		"completed without a fee": func(s *WorldSnapshot) {
			i := index(s, transfers.StatusRejected, 0)
			s.Transfers.Offers[i].Status = transfers.StatusCompleted
		},
		"offer open past its deadline": func(s *WorldSnapshot) {
			i := index(s, transfers.StatusRejected, 0)
			s.Transfers.Offers[i].Status, s.Transfers.Offers[i].ClosedAt = transfers.StatusOpen, 0
		},
		"expired early": func(s *WorldSnapshot) {
			i := index(s, transfers.StatusRejected, 0)
			s.Transfers.Offers[i].Status = transfers.StatusExpired
		},
		"fee for no offer": func(s *WorldSnapshot) {
			for i, e := range s.Finance.Entries {
				if e.Kind == finance.KindTransfer {
					s.Finance.Entries[i].Offer = 999
				}
			}
		},
		"offer allocator behind": func(s *WorldSnapshot) { s.Transfers.LastOffer-- },
		"transfer task missing": func(s *WorldSnapshot) {
			s.Scheduler.Tasks = slices.DeleteFunc(s.Scheduler.Tasks, func(t sim.Task) bool { return t.Kind == taskTransferRun })
		},
		"transfer task late": func(s *WorldSnapshot) {
			for i, task := range s.Scheduler.Tasks {
				if task.Kind == taskTransferRun {
					s.Scheduler.Tasks[i].DueAt += day
				}
			}
		},
		"bid record for another fee": func(s *WorldSnapshot) { s.OfferCommands[0].Request.Fee++ },
		"answer record accepted":     func(s *WorldSnapshot) { s.ResponseCommands[0].Request.Accept = true },
		"answer record completed":    func(s *WorldSnapshot) { s.ResponseCommands[0].Result.Status = transfers.StatusCompleted },
		"event deal edited": func(s *WorldSnapshot) {
			for i, e := range s.Events {
				if e.Kind == events.KindTransferCompleted {
					s.Events[i].TransferCompleted.Fee++
					return
				}
			}
		},
		"no window days": func(s *WorldSnapshot) { s.Content.Transfers.WindowDays = 0 },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if name == "no window days" {
			snap.ContentFingerprint = contentFingerprint(snap.Content, leagueDefsOf(&snap), snap.Cups)
		}
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	snap := build()
	snap.Versions.Transfers++
	if _, err := Restore(snap); !errors.Is(err, ErrIncompatibleSave) {
		t.Errorf("other transfers version: %v", err)
	}
}
