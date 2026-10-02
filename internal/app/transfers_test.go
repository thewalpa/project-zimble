package app

import (
	"errors"
	"maps"
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
// directly in employment, without a payoff, to make a vacancy (AI clubs
// never release players); commitMoves records it as an expired contract.
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
	commitMoves(t, w, employment.Changes{Departures: []ids.PlayerID{weakest.Player}})
	return weakest.Player
}

// bestAt returns the best player at a position (ties: lowest ID) of a club
// other than buyer whose squad is above its minimum there, whom no club has
// an open bid for (the earlier bid would be answered first), who would join
// buyer (ai.Joins) and whose price buyer can afford.
func bestAt(t *testing.T, w *World, pos players.Position, buyer ids.ClubID) SquadPlayer {
	t.Helper()
	var best []SquadPlayer
	funds, _ := w.finance.Balance(buyer)
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		if c.ID == buyer || w.squadCounts(team)[pos] <= w.defs.Quota(pos).Min {
			continue
		}
		squad, _ := w.Squad(c.ID)
		for _, p := range squad {
			if p.Position == pos && p.Value <= funds && ai.Joins(p.Overall, w.squadAverage(c.ID), w.squadAverage(buyer)) &&
				!slices.ContainsFunc(w.transfers.Open(), func(o transfers.Offer) bool { return o.Player == p.Player }) {
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
	if !win.Open || win.Opens != 0 || win.Closes != closes || win.BidsClose != closes-day || win.FreeAgentsOpen != 14*day || win.NextRun != day {
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
	if win.Open || win.Opens != next || win.Closes != next+closes || win.FreeAgentsOpen != next+14*day || win.NextRun != next+day || transferTask(t, w).DueAt != next+day {
		t.Fatalf("window after the close %+v, next contract-year end %d", win, next)
	}
	if _, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
		Fee: target.Value, Offer: suggest(t, w, target.Player)}); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("bid after the close: %v", err)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	// Without a manager, AI clubs trade among themselves: they buy
	// upgrades, list the players these replace and buy listed players to
	// fill their vacancies. The transfer list is cleared at the close.
	ai := newWorld(t, 42)
	listed := 0
	for d := sim.GameInstant(1); d < 28; d++ {
		mustContinue(t, ai, d*day)
		listed = max(listed, len(ai.TransferList()))
	}
	if listed == 0 || !slices.ContainsFunc(ai.Offers(), func(o OfferView) bool { return o.Status == transfers.StatusCompleted }) {
		t.Fatalf("an AI-only world made %d offers and listed at most %d players", len(ai.Offers()), listed)
	}
	mustContinue(t, ai, closes)
	if len(ai.TransferList()) != 0 || len(ai.transfers.Open()) != 0 {
		t.Fatalf("after the close %d players are listed and %d offers open", len(ai.TransferList()), len(ai.transfers.Open()))
	}
	if err := ai.Validate(); err != nil {
		t.Fatal(err)
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
	if want, _ := w.sellingPrice(target.Player, 0); target.Value != want || target.Value <= 0 {
		t.Fatalf("asking price %s, selling price %s", target.Value, want)
	}
	if r := w.BidRefusal(target.Player); r != RefusalNone {
		t.Fatalf("BidRefusal = %d", r)
	}
	terms := suggest(t, w, target.Player)
	buyerBefore, sellerBefore, total := balance(t, w, userClub3), balance(t, w, seller), totalBalance(w)
	userTeam, sellerTeam := mustUserTeam(t, w), w.competitionsTeam(seller)
	messages := len(w.Inbox()) // the released player's

	res := bidFor(t, w, target.Player, target.Value)
	o := offerOf(t, w, res.Offer)
	if res.Deadline != day || o.Status != transfers.StatusOpen || o.Seller != seller || o.Buyer != userClub3 || o.Terms != transfers.Terms(terms) {
		t.Fatalf("result %+v, offer %+v", res, o)
	}
	evs := transferEvents(w)
	if len(evs) != 1 || evs[0].Kind != events.KindTransferOffered || evs[0].Cause != commandCause(res.Command) || evs[0].TransferOffered.Deadline != day {
		t.Fatalf("events after the bid %+v", evs)
	}
	if len(w.Inbox()) != messages {
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
	m := w.Inbox()[messages]
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

// Late in the window an AI club keeps a player it needs: a bid answered
// with too few runs left for it to replace him (see replaceable) is
// rejected, while it still sells a player it can spare.
func TestLateBidForANeededPlayerIsRejected(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	release(t, w, userClub3, players.Forward)
	window := w.TransferWindow()
	late := window.NeededClose
	if late != window.Closes-3*day {
		t.Fatalf("needed players are sold until %d, window closes at %d", late, window.Closes)
	}
	mustContinue(t, w, late)
	// The cheapest forward of an AI club holding exactly its roster count.
	count := w.defs.Quota(players.Forward).Count
	var target SquadPlayer
	for _, c := range w.registry.Clubs() {
		if c.ID == userClub3 || w.squadCounts(w.competitionsTeam(c.ID))[players.Forward] != count {
			continue
		}
		squad, _ := w.Squad(c.ID)
		for _, p := range squad {
			if p.Position == players.Forward && biddable(w, p.Player) && (target.Player == 0 || p.Value < target.Value) &&
				!slices.ContainsFunc(w.transfers.Open(), func(o transfers.Offer) bool { return o.Player == p.Player }) {
				target = p
			}
		}
	}
	if target.Player == 0 {
		t.Fatal("no forward to bid for")
	}
	seller := clubOf(w, target.Player)
	if r := w.BidRefusal(target.Player); r != RefusalNeeded {
		t.Fatalf("BidRefusal = %d", r)
	}
	res := bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, late+day)
	if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusRejected || clubOf(w, target.Player) != seller {
		t.Fatalf("offer %+v, player at club %d", o, clubOf(w, target.Player))
	}
	// A bid made the day before would have been answered in time.
	if ok, err := w.replaceable(late, window.Closes); !ok || err != nil {
		t.Fatalf("a sale answered at %d is not replaceable: %v", late, err)
	}
	if ok, _ := w.replaceable(late+day, window.Closes); ok {
		t.Fatalf("a sale answered at %d is replaceable", late+day)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A star (ai.StarMargin above his club's squad average) joins only a club
// at least as strong: the selling club accepts the manager's bid at his
// price from a weaker club, but the star refuses to join, and no weaker AI
// club bids for a star.
func TestAStarRefusesAWeakerClub(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	averages := map[ids.ClubID]int{}
	for _, c := range w.registry.Clubs() {
		averages[c.ID] = w.squadAverage(c.ID)
	}
	funds := balance(t, w, userClub3)
	var star SquadPlayer
	for _, c := range w.registry.Clubs() {
		if averages[c.ID] <= averages[userClub3] {
			continue
		}
		squad, _ := w.Squad(c.ID)
		for _, p := range squad {
			if star.Player == 0 && p.Overall >= averages[c.ID]+ai.StarMargin && p.Value <= funds &&
				w.squadCounts(w.competitionsTeam(c.ID))[p.Position] > w.defs.Quota(p.Position).Min {
				star = p
			}
		}
	}
	if star.Player == 0 {
		t.Fatal("no affordable star at a stronger club")
	}
	seller := clubOf(w, star.Player)
	if r := w.BidRefusal(star.Player); r != RefusalStar {
		t.Fatalf("BidRefusal = %d", r)
	}
	res := bidFor(t, w, star.Player, star.Value)
	mustContinue(t, w, day)
	if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusRefused || clubOf(w, star.Player) != seller {
		t.Fatalf("offer %+v, star at club %d", o, clubOf(w, star.Player))
	}
	// The AI bids of the first run were chosen against the squads as they
	// were before it.
	bids := 0
	for _, o := range w.transfers.Offers() {
		if o.Buyer == userClub3 || o.MadeAt != day {
			continue
		}
		bids++
		p, _ := w.players.Profile(o.Player)
		if !ai.Joins(p.Overall(), averages[o.Seller], averages[o.Buyer]) {
			t.Fatalf("club %d (average %d) bid for player %d (%d) of club %d (average %d)",
				o.Buyer, averages[o.Buyer], o.Player, p.Overall(), o.Seller, averages[o.Seller])
		}
	}
	if bids == 0 {
		t.Fatal("no AI bids at the first run")
	}
}

// lowerAverage releases a club's best players (ties: lowest ID) at
// positions other than keep, while it stays above its minimum there, until
// its squad average is below ceiling; commitMoves records each as an
// expired contract.
func lowerAverage(t *testing.T, w *World, club ids.ClubID, keep players.Position, ceiling int) {
	t.Helper()
	for w.squadAverage(club) >= ceiling {
		team, _ := w.registry.SeniorTeam(club)
		counts := w.squadCounts(team)
		var best SquadPlayer
		squad, _ := w.Squad(club)
		for _, p := range squad {
			if p.Position != keep && counts[p.Position] > w.defs.Quota(p.Position).Min && (best.Player == 0 || p.Overall > best.Overall) {
				best = p
			}
		}
		if best.Player == 0 {
			t.Fatalf("club %d cannot lower its average %d below %d", club, w.squadAverage(club), ceiling)
		}
		commitMoves(t, w, employment.Changes{Departures: []ids.PlayerID{best.Player}})
	}
}

// consentUnchanged fails unless a refused offer left its player at the
// seller and moved no fee.
func consentUnchanged(t *testing.T, w *World, o transfers.Offer) {
	t.Helper()
	if c := offerOf(t, w, o.ID); c.Status != transfers.StatusRefused || c.ClosedAt != w.Now() || clubOf(w, o.Player) != o.Seller {
		t.Fatalf("offer %+v, player at club %d", c, clubOf(w, o.Player))
	}
	for _, e := range w.finance.All() {
		if e.Kind == finance.KindTransfer && e.Offer == o.ID {
			t.Fatalf("a refused offer moved a fee: %+v", e)
		}
	}
	assertLedgersConsistent(t, w)
}

// A player's consent (ai.Joins) binds at completion, whoever the seller: a
// star who would join the buyer when the bid is made refuses once the
// squads have changed, and the offer closes as refused, moving nobody and
// no money. For the manager's player too, answered before the deadline.
func TestConsentAtCompletion(t *testing.T) {
	t.Run("manager sells", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		squad, _ := w.Squad(userClub3)
		star := slices.MaxFunc(squad, func(a, b SquadPlayer) int { return a.Overall - b.Overall })
		mine := w.squadAverage(userClub3)
		if star.Overall < mine+ai.StarMargin+2 {
			t.Fatalf("best player %d rates %d, squad average %d", star.Player, star.Overall, mine)
		}
		// The weakest AI club at least as strong as the manager's, with room
		// for him.
		var buyer ids.ClubID
		for _, c := range w.registry.Clubs() {
			if avg := w.squadAverage(c.ID); c.ID != userClub3 && avg >= mine && (buyer == 0 || avg < w.squadAverage(buyer)) {
				buyer = c.ID
			}
		}
		if buyer == 0 {
			t.Fatal("no AI club as strong as the manager's")
		}
		release(t, w, buyer, star.Position)
		if !ai.Joins(star.Overall, mine, w.squadAverage(buyer)) {
			t.Fatal("the star would not join the buyer when it bids")
		}
		o := plantBid(t, w, star.Player, buyer, star.Value)
		// The buyer's squad grows weaker than the manager's before he answers.
		lowerAverage(t, w, buyer, star.Position, mine)
		if ai.Joins(star.Overall, w.squadAverage(userClub3), w.squadAverage(buyer)) {
			t.Fatal("the star would still join")
		}
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
		mineBefore, buyerBefore := balance(t, w, userClub3), balance(t, w, buyer)
		cmd := RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true}
		res, err := w.RespondToOffer(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != transfers.StatusRefused || res.Contract != (employment.Contract{}) {
			t.Fatalf("result %+v", res)
		}
		consentUnchanged(t, w, o)
		if balance(t, w, userClub3) != mineBefore || balance(t, w, buyer) != buyerBefore {
			t.Fatal("balances changed")
		}
		evs := w.Events()
		if e := evs[len(evs)-1]; e.Kind != events.KindOfferClosed || e.OfferClosed.Outcome != uint8(transfers.StatusRefused) || e.Cause != commandCause(cmd.ID) {
			t.Fatalf("event %+v", e)
		}
		if m := lastMessage(w); m.Kind != inbox.KindOfferClosed || m.Outcome != uint8(transfers.StatusRefused) || !m.Selling || m.Offer != o.ID {
			t.Fatalf("inbox %+v", m)
		}
		snap := w.Snapshot()
		if again, err := w.RespondToOffer(cmd); err != nil || again != res || !reflect.DeepEqual(w.Snapshot(), snap) {
			t.Fatalf("retry: %+v %v", again, err)
		}
		consentUnchanged(t, roundTrip(t, w), o)
	})

	t.Run("AI club sells", func(t *testing.T) {
		w := userWorld(t, 42, userClub3)
		funds, mine := balance(t, w, userClub3), w.squadAverage(userClub3)
		// A star of the strongest AI club no stronger than the manager's, which
		// it sells at its price.
		var star SquadPlayer
		strongest := 0
		for _, c := range w.registry.Clubs() {
			avg := w.squadAverage(c.ID)
			if c.ID == userClub3 || avg > mine || avg <= strongest {
				continue
			}
			team, _ := w.registry.SeniorTeam(c.ID)
			squad, _ := w.Squad(c.ID)
			for _, p := range squad {
				if p.Overall >= avg+ai.StarMargin+2 && p.Value <= funds && w.squadCounts(team)[p.Position] > w.defs.Quota(p.Position).Min {
					star, strongest = p, avg
					break
				}
			}
		}
		if star.Player == 0 {
			t.Fatal("no affordable star at a weaker club")
		}
		seller := clubOf(w, star.Player)
		release(t, w, userClub3, star.Position)
		if r := w.BidRefusal(star.Player); r != RefusalNone {
			t.Fatalf("BidRefusal = %d", r)
		}
		res := bidFor(t, w, star.Player, star.Value)
		// The manager's squad grows weaker than the seller's before the run.
		lowerAverage(t, w, userClub3, star.Position, w.squadAverage(seller))
		if w.BidRefusal(star.Player) != RefusalStar {
			t.Fatal("the star would still join")
		}
		before := balance(t, w, userClub3)
		mustContinue(t, w, day)
		consentUnchanged(t, w, offerOf(t, w, res.Offer))
		if balance(t, w, userClub3) != before {
			t.Fatal("the manager's balance changed")
		}
		if m := lastMessage(w); m.Kind != inbox.KindOfferClosed || m.Outcome != uint8(transfers.StatusRefused) || m.Selling {
			t.Fatalf("inbox %+v", m)
		}
	})
}

// Consent is judged against the squads as the run has staged them: a player
// who would join the manager's club when the bid is made refuses once his
// club has completed a purchase earlier in the same run that makes him one
// of the stars of a stronger club.
func TestConsentFollowsEarlierCompletions(t *testing.T) {
	// Generation v9: seed 3 supplies a star whose club overtakes the
	// manager after an earlier purchase; seed 42's target now moves away.
	w, bid := bidScenarioWithSeed(t, 3)(t)
	if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: bid.ID}); err != nil {
		t.Fatal(err)
	}
	release(t, w, userClub3, players.Forward)
	target := bestAt(t, w, players.Forward, userClub3)
	seller, average := clubOf(w, target.Player), w.squadAverage(clubOf(w, target.Player))
	if w.BidRefusal(target.Player) != RefusalNone {
		t.Fatal("the target refuses before the run")
	}
	res := bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, 2*day)
	consentUnchanged(t, w, offerOf(t, w, res.Offer))
	if !slices.ContainsFunc(w.transfers.Offers(), func(o transfers.Offer) bool {
		return o.ID < res.Offer && o.Buyer == seller && o.Status == transfers.StatusCompleted && o.ClosedAt == 2*day
	}) || w.squadAverage(seller) <= average {
		t.Fatalf("club %d (average %d, was %d) bought nobody earlier in the run", seller, w.squadAverage(seller), average)
	}
}

// A club keeps an unlisted player it bought in the previous window: the
// manager's bid at his price is rejected, and no AI club bids for him.
func TestANewSigningIsNotSoldOn(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	playUntil(t, w, func(w *World) sim.GameInstant { return w.TransferWindow().Closes })
	playSeason(t, w)
	for end := w.ContractYearEnd(); w.Now() < end; {
		mustContinue(t, w, end)
	}
	open := w.Now()
	settling := map[ids.PlayerID]bool{}
	for _, o := range w.transfers.Offers() {
		if o.Status == transfers.StatusCompleted && o.Buyer != userClub3 && clubOf(w, o.Player) == o.Buyer {
			settling[o.Player] = true
		}
	}
	funds, mine := balance(t, w, userClub3), w.squadAverage(userClub3)
	var target SquadPlayer
	for _, id := range slices.Sorted(maps.Keys(settling)) {
		p, seller := w.squadPlayer(id), clubOf(w, id)
		if target.Player == 0 && !p.Listed && p.Value <= funds && ai.Joins(p.Overall, w.squadAverage(seller), mine) &&
			w.squadCounts(w.competitionsTeam(seller))[p.Position] > w.defs.Quota(p.Position).Min {
			target = p
		}
	}
	if target.Player == 0 {
		t.Fatal("no affordable player bought in the first window")
	}
	seller := clubOf(w, target.Player)
	if r := w.BidRefusal(target.Player); r != RefusalSettling {
		t.Fatalf("BidRefusal = %d", r)
	}
	res := bidFor(t, w, target.Player, target.Value)
	mustContinue(t, w, open+day)
	if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusRejected || clubOf(w, target.Player) != seller {
		t.Fatalf("offer %+v, player at club %d", o, clubOf(w, target.Player))
	}
	for _, o := range w.transfers.Offers() {
		if _, listed := w.transfers.Listing(o.Player); o.MadeAt == open+day && o.Buyer != userClub3 && settling[o.Player] && !listed {
			t.Fatalf("club %d bid for player %d, bought by club %d in the previous window", o.Buyer, o.Player, o.Seller)
		}
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
		{"no user club", ErrNoUserClub, func(t *testing.T, s *setup) {
			s.w = newWorld(t, 42)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"own player", ErrNotTransferable, func(t *testing.T, s *setup) {
			squad, _ := s.w.Squad(userClub3)
			s.cmd.Player = squad[0].Player
		}},
		{"free agent", ErrNotTransferable, func(t *testing.T, s *setup) { s.cmd.Player = s.w.FreeAgents()[0].Player }},
		{"unknown player", ErrNotTransferable, func(_ *testing.T, s *setup) { s.cmd.Player = 99_999 }},
		{"squad full", ErrSquadFull, func(t *testing.T, s *setup) {
			s.w.defs.SquadLimit = len(s.w.employment.Squad(mustUserTeam(t, s.w)))
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

func listFor(t *testing.T, w *World, player ids.PlayerID, asking money.Money) PlayerListed {
	t.Helper()
	res, err := w.ListPlayer(ListPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player, Asking: asking})
	if err != nil {
		t.Fatalf("list player %d at %s: %v", player, asking, err)
	}
	return res
}

// bidScenario finds, by simulation, a manager's player whom an AI club bids
// for at the first run once the manager lists him at half his valuation:
// the world after that run and the bid. Every fresh world is the same, so the
// search is deterministic.
func bidScenario(t *testing.T) func(*testing.T) (*World, transfers.Offer) {
	return bidScenarioWithSeed(t, 42)
}

func bidScenarioWithSeed(t *testing.T, seed uint64) func(*testing.T) (*World, transfers.Offer) {
	t.Helper()
	squad, _ := userWorld(t, seed, userClub3).Squad(userClub3)
	slices.SortStableFunc(squad, func(a, b SquadPlayer) int { return b.Overall - a.Overall })
	for _, p := range squad {
		build := func(t *testing.T) (*World, transfers.Offer) {
			w := userWorld(t, seed, userClub3)
			listFor(t, w, p.Player, p.Value/2)
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
	t.Fatal("no AI club bids for a manager's listed player")
	return nil
}

// An AI club bids the asking price for the manager's listed player; the
// manager accepts, declines or lets the bid expire.
func TestManagerAnswersBids(t *testing.T) {
	scenario := bidScenario(t)
	w, o := scenario(t)
	if l, _ := w.transfers.Listing(o.Player); o.Fee != l.Asking || o.MadeAt != day || o.Deadline != 4*day {
		t.Fatalf("bid %+v, listing %+v", o, l)
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
	terms, _ := w.windowTerms(buyer, player, m.open)
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
	// An AI club buys one player in the window, and after that only listed
	// players (their clubs are left without a vacancy).
	listedAt := map[ids.PlayerID]sim.GameInstant{}
	for _, e := range w.Events() {
		if e.Kind == events.KindPlayerListed {
			listedAt[e.PlayerListed.Player] = e.OccurredAt
		}
	}
	bought := map[ids.ClubID]int{}
	completed := 0
	for _, o := range w.Offers() {
		if o.Status == transfers.StatusCompleted {
			completed++
			if bought[o.Buyer]++; o.Buyer != userClub3 && bought[o.Buyer] > 1 {
				if at, ok := listedAt[o.Player]; !ok || at > o.MadeAt {
					t.Fatalf("AI club %d bought a second, unlisted player %d", o.Buyer, o.Player)
				}
			}
		}
		if o.Status == transfers.StatusOpen {
			t.Fatalf("offer %d is still open after the close", o.ID)
		}
	}
	if completed < 3 || len(listedAt) == 0 {
		t.Fatalf("%d transfers completed, %d players listed", completed, len(listedAt))
	}
	if len(w.TransferList()) != 0 {
		t.Fatal("players are still listed after the close")
	}
	assertAISquadsFull(t, w)
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

// An AI club's asking price is its valuation, with a premium for a player
// above its squad average; the manager's players are shown at their
// valuation. The suggested terms are an AI club's for the contract year
// under way.
func TestAskingPriceAndSuggestedTerms(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	premium := 0
	for _, club := range []ids.ClubID{1, userClub3} {
		squad, _ := w.Squad(club)
		for _, p := range squad {
			age, _ := w.age(p.Player, 0)
			years, _ := w.contractYearsLeft(p.Contract.Expires, 0)
			want := ai.Valuation(p.Overall, age, years)
			if club != userClub3 {
				want = ai.SellingPrice(want, p.Overall, w.squadAverage(club))
			}
			if p.Value != want {
				t.Fatalf("club %d player %d asks %s, want %s", club, p.Player, p.Value, want)
			}
			if club != userClub3 && p.Value > ai.Valuation(p.Overall, age, years) {
				premium++
			}
		}
	}
	if premium == 0 {
		t.Fatal("no player of club 1 is priced above his valuation")
	}
	squad, _ := w.Squad(1)
	for _, p := range squad {
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
	o, err := w.aiOffer(w.userClub, player, year)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestRestoreRejectsInvalidTransfers(t *testing.T) {
	scenario := bidScenarioWithSeed(t, 3)
	build := func() WorldSnapshot {
		w, bid := scenario(t)
		// The manager declines one bid and bids for a forward, who refuses
		// to join once his club has bought a player earlier in the run
		// (TestConsentFollowsEarlierCompletions); AI clubs complete purchases.
		if _, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: bid.ID}); err != nil {
			t.Fatal(err)
		}
		release(t, w, userClub3, players.Forward)
		target := bestAt(t, w, players.Forward, userClub3)
		res := bidFor(t, w, target.Player, target.Value)
		mustContinue(t, w, 2*day)
		if o := offerOf(t, w, res.Offer); o.Status != transfers.StatusRefused {
			t.Fatalf("the manager's bid is %s", o.Status)
		}
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
		"fee edited": func(s *WorldSnapshot) { s.Transfers.Offers[index(s, transfers.StatusCompleted, 0)].Fee++ },
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
			snap.ContentFingerprint = contentFingerprint(snap.Content, leagueDefsOf(&snap), snap.Cups, snap.Promotions)
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
