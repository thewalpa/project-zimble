package app

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// taskTransferRun is one day of the transfer window (see transferRun): due
// at 00:00 on each day after the window opens, up to and including its
// close. It runs in the Decisions phase, has no payload and reschedules
// itself, so exactly one is always queued, due at the next run.
const taskTransferRun sim.TaskKind = 7

var (
	ErrWindowClosed    = errors.New("app: the transfer window is closed")
	ErrNotTransferable = errors.New("app: the player cannot be bought")
	ErrAlreadyBid      = errors.New("app: your club has already bid for the player in this window")
	ErrCannotAfford    = errors.New("app: the club cannot afford the fee")
	ErrNoSuchOffer     = errors.New("app: no open offer for one of your players has that ID")
	ErrSquadMinimum    = errors.New("app: the squad would fall below its minimum at that position")
	ErrNotListed       = errors.New("app: the player is not on the transfer list")
	ErrInvalidPrice    = errors.New("app: an asking price must be positive")
	ErrPlayerRefuses   = errors.New("app: the player refuses to join the buying club")
)

// Transfer windows. A window opens at each contract-year end and closes
// Transfers.WindowDays days later: [open, close). Clubs answer bids at the
// transfer runs, 00:00 on each day after the opening up to the close; a bid
// is answered at the first run after it, and a manager answers a bid for
// one of their players within Transfers.ResponseDays days, never beyond the
// close. A transfer completes only inside the window.

// transferWindow returns the window opened at the latest contract-year end
// at or before t.
func (w *World) transferWindow(t sim.GameInstant) (open, close sim.GameInstant, err error) {
	next, err := w.contractYearEnd(t)
	if err != nil {
		return 0, 0, err
	}
	if open, err = w.addYears(next, -1); err != nil {
		return 0, 0, err
	}
	return open, open + sim.GameInstant(w.defs.Transfers.WindowDays)*sim.GameInstant(sim.Day), nil
}

// nextTransferRun returns the first transfer run strictly after t.
func (w *World) nextTransferRun(t sim.GameInstant) (sim.GameInstant, error) {
	open, close, err := w.transferWindow(t)
	if err != nil {
		return 0, err
	}
	day := sim.GameInstant(sim.Day)
	if t < close {
		return open + ((t-open)/day+1)*day, nil
	}
	next, err := w.contractYearEnd(t)
	return next + day, err
}

// replaceable reports whether an AI club that sells a player it needs at
// the run at `at` can still replace him in the window closing at close: it
// can bid at that run and, should that bid fail, once more at the next.
func (w *World) replaceable(at, close sim.GameInstant) (bool, error) {
	next, err := w.nextTransferRun(at)
	if err != nil {
		return false, err
	}
	after, err := w.nextTransferRun(next)
	return after < close, err
}

// managerDeadline is when a bid made at t for one of the manager's players
// expires: ResponseDays days later, never beyond the close.
func (w *World) managerDeadline(t, close sim.GameInstant) sim.GameInstant {
	return min(t+sim.GameInstant(w.defs.Transfers.ResponseDays)*sim.GameInstant(sim.Day), close)
}

func (w *World) scheduleTransferRun(at sim.GameInstant) error {
	if _, err := w.scheduler.Schedule(sim.TaskSpec{DueAt: at, Phase: sim.PhaseDecisions, Kind: taskTransferRun}); err != nil {
		return fmt.Errorf("app: schedule transfer run at %d: %w", at, err)
	}
	return nil
}

// contractYearsLeft counts the contract-year ends in (t, expires]: 1 for a
// contract in its final year.
func (w *World) contractYearsLeft(expires, t sim.GameInstant) (int, error) {
	end, err := w.contractYearEnd(t)
	if err != nil {
		return 0, err
	}
	a, err := w.yearOf(end)
	if err != nil {
		return 0, err
	}
	b, err := w.yearOf(expires)
	return b - a + 1, err
}

// valuation is ai.Valuation of an employed player at t.
func (w *World) valuation(player ids.PlayerID, t sim.GameInstant) (money.Money, error) {
	a, ok := w.employment.Assignment(player)
	p, profiled := w.players.Profile(player)
	if !ok || !profiled {
		return 0, fmt.Errorf("app: player %d has no club or profile", player)
	}
	age, err := w.age(player, t)
	if err != nil {
		return 0, err
	}
	years, err := w.contractYearsLeft(a.Contract.Expires, t)
	if err != nil {
		return 0, err
	}
	return ai.Valuation(p.Overall(), age, years), nil
}

// squadAverage is the mean overall of a club's senior squad, rounded half
// up; 0 for an empty squad.
func (w *World) squadAverage(club ids.ClubID) int {
	team, _ := w.registry.SeniorTeam(club)
	total, n := 0, 0
	for _, id := range w.employment.Squad(team) {
		p, _ := w.players.Profile(id)
		total, n = total+p.Overall(), n+1
	}
	if n == 0 {
		return 0
	}
	return (2*total + n) / (2 * n)
}

// sellingPrice is the fee at which an employed player's club sells him at
// t: his asking price if he is listed; else, at an AI club,
// ai.SellingPrice of its valuation against its squad average; else (the
// manager's player) the valuation.
func (w *World) sellingPrice(player ids.PlayerID, t sim.GameInstant) (money.Money, error) {
	if l, ok := w.transfers.Listing(player); ok {
		return l.Asking, nil
	}
	value, err := w.valuation(player, t)
	if err != nil {
		return 0, err
	}
	a, _ := w.employment.Assignment(player)
	p, _ := w.players.Profile(player)
	return w.clubPrice(a.Club, p.Overall(), value, w.squadAverage(a.Club)), nil
}

// clubPrice is the fee at which a club with a squad average sells an
// unlisted player it values at value: ai.SellingPrice at an AI club, the
// valuation at the manager's.
func (w *World) clubPrice(club ids.ClubID, overall int, value money.Money, average int) money.Money {
	if club == w.userClub {
		return value
	}
	return ai.SellingPrice(value, overall, average)
}

// transferContract is the contract a transfer at t starts: Terms.Years
// contract years, the current one included, at Terms.WeeklyWage.
func (w *World) transferContract(t sim.GameInstant, terms transfers.Terms) (employment.Contract, error) {
	end, err := w.contractYearEnd(t)
	if err != nil {
		return employment.Contract{}, err
	}
	expires, err := w.addYears(end, terms.Years-1)
	return employment.Contract{Expires: expires, WeeklyWage: terms.WeeklyWage}, err
}

// windowTerms are the terms an AI club offers in the window opened at open:
// aiOffer for the contract year under way.
func (w *World) windowTerms(player ids.PlayerID, open sim.GameInstant) (transfers.Terms, error) {
	year, err := w.yearOf(open)
	if err != nil {
		return transfers.Terms{}, err
	}
	o, err := w.aiOffer(player, year)
	return transfers.Terms(o), err
}

// market is the transfer state staged by one command or transfer run:
// employers, squads, balances and wage bills as they will be after the
// staged changes, the open offers, the transfer list, and the changes
// themselves. Decisions later in a run see the earlier ones.
type market struct {
	w           *World
	at          sim.GameInstant
	open, close sim.GameInstant

	employer map[ids.PlayerID]ids.ClubID
	position map[ids.PlayerID]players.Position
	counts   map[ids.ClubID]map[players.Position]int
	balances map[ids.ClubID]money.Money
	wages    map[ids.ClubID]money.Money
	moved    map[ids.PlayerID]bool               // completed a transfer in this window
	settling map[ids.PlayerID]bool               // joined his club by transfer in the previous window
	averages map[ids.ClubID]int                  // squad averages before the staged changes
	overalls map[ids.ClubID]int                  // sums of the staged squads' overalls
	bought   map[ids.ClubID]bool                 // bought a player in this window
	bidFor   map[[2]uint64]bool                  // (buyer, player) offers made in this window
	offers   map[ids.OfferID]transfers.Offer     // open offers
	openBids map[ids.ClubID]int                  // open offers by buyer
	openFor  map[ids.PlayerID]int                // open offers by player
	pool     []ai.FreeAgent                      // free agents not signed yet, best first
	terms    map[ids.OfferID]employment.Contract // completed offers' contracts
	listings map[ids.PlayerID]transfers.Listing  // the transfer list
	values   map[ids.PlayerID]money.Money        // valuations at m.at, computed once

	jobs     employment.Changes
	postings []finance.Posting
	changes  transfers.Changes
	actions  []marketAction // AI bids and signings, in order
	// Listing changes to announce, in order; a listing that ends because the
	// player leaves or the window closes is not announced.
	listed   []transfers.Listing
	unlisted []transfers.Listing
}

// marketAction is one AI action of a run: a bid (index into changes.Bids)
// or a free-agent signing.
type marketAction struct {
	bid     int // -1 for a signing
	signing employment.Assignment
}

// newMarket stages the current state at instant at.
func (w *World) newMarket(at sim.GameInstant) (*market, error) {
	open, close, err := w.transferWindow(at)
	if err != nil {
		return nil, err
	}
	m := &market{
		w: w, at: at, open: open, close: close,
		employer: map[ids.PlayerID]ids.ClubID{}, position: map[ids.PlayerID]players.Position{},
		counts: map[ids.ClubID]map[players.Position]int{}, balances: map[ids.ClubID]money.Money{},
		wages: map[ids.ClubID]money.Money{}, moved: map[ids.PlayerID]bool{}, settling: map[ids.PlayerID]bool{}, averages: map[ids.ClubID]int{},
		overalls: map[ids.ClubID]int{}, bought: map[ids.ClubID]bool{}, bidFor: map[[2]uint64]bool{},
		offers: map[ids.OfferID]transfers.Offer{}, openBids: map[ids.ClubID]int{}, openFor: map[ids.PlayerID]int{},
		terms: map[ids.OfferID]employment.Contract{}, listings: map[ids.PlayerID]transfers.Listing{},
		values: map[ids.PlayerID]money.Money{},
	}
	previous, err := w.addYears(open, -1)
	if err != nil {
		return nil, err
	}
	for _, c := range w.registry.Clubs() {
		m.counts[c.ID] = map[players.Position]int{}
		m.averages[c.ID] = w.squadAverage(c.ID)
		m.balances[c.ID], _ = w.finance.Balance(c.ID)
		if m.wages[c.ID], err = w.employment.WageBill(c.ID); err != nil {
			return nil, err
		}
	}
	for _, a := range w.employment.Assignments() {
		p, _ := w.players.Profile(a.Player)
		m.employer[a.Player], m.position[a.Player] = a.Club, p.Position
		m.counts[a.Club][p.Position]++
		m.overalls[a.Club] += p.Overall()
	}
	for _, o := range w.transfers.Offers() {
		if o.MadeAt >= open {
			m.bidFor[[2]uint64{uint64(o.Buyer), uint64(o.Player)}] = true
		}
		if o.Status == transfers.StatusCompleted && o.ClosedAt >= open {
			m.moved[o.Player], m.bought[o.Buyer] = true, true
		}
		if o.Status == transfers.StatusCompleted && o.ClosedAt >= previous && o.ClosedAt < open && m.employer[o.Player] == o.Buyer {
			m.settling[o.Player] = true
		}
		if o.Status == transfers.StatusOpen {
			m.offers[o.ID] = o
			m.openBids[o.Buyer]++
			m.openFor[o.Player]++
		}
	}
	for _, l := range w.transfers.Listings() {
		m.listings[l.Player] = l
	}
	return m, nil
}

// value is ai.Valuation of a player employed since before the market, at
// m.at.
func (m *market) value(player ids.PlayerID) (money.Money, error) {
	if v, ok := m.values[player]; ok {
		return v, nil
	}
	v, err := m.w.valuation(player, m.at)
	if err != nil {
		return 0, err
	}
	m.values[player] = v
	return v, nil
}

// overall is a player's overall.
func (m *market) overall(player ids.PlayerID) int {
	p, _ := m.w.players.Profile(player)
	return p.Overall()
}

// average is a club's squad average as staged, rounded half up like
// squadAverage; 0 for an empty squad.
func (m *market) average(club ids.ClubID) int {
	n := squadSize(m.counts[club])
	if n == 0 {
		return 0
	}
	return (2*m.overalls[club] + n) / (2 * n)
}

// joins reports whether a player agrees to move from his staged club to
// buyer (ai.Joins), against the squads as staged: his consent.
func (m *market) joins(player ids.PlayerID, buyer ids.ClubID) bool {
	return ai.Joins(m.overall(player), m.average(m.employer[player]), m.average(buyer))
}

// price is the fee at which a player's club sells him (see sellingPrice),
// against the squad averages before the staged changes.
func (m *market) price(player ids.PlayerID) (money.Money, error) {
	if l, ok := m.listings[player]; ok {
		return l.Asking, nil
	}
	value, err := m.value(player)
	if err != nil {
		return 0, err
	}
	a, _ := m.w.employment.Assignment(player) // not moved in this window: the store's assignment
	p, _ := m.w.players.Profile(player)
	return m.w.clubPrice(a.Club, p.Overall(), value, m.averages[a.Club]), nil
}

// list stages a listing at m.at, replacing the player's listing if he has
// one, and announces it.
func (m *market) list(player ids.PlayerID, club ids.ClubID, asking money.Money) {
	if _, ok := m.listings[player]; ok {
		m.unlist(player, false)
	}
	l := transfers.Listing{Player: player, Club: club, Asking: asking, ListedAt: m.at}
	m.changes.List = append(m.changes.List, l)
	m.listings[player] = l
	m.listed = append(m.listed, l)
}

// unlist stages the end of a player's listing, if he has one. Only a club
// taking him off the list is announced: when he leaves the club or the
// window closes, his listing ends with that event.
func (m *market) unlist(player ids.PlayerID, announce bool) {
	l, ok := m.listings[player]
	if !ok {
		return
	}
	m.changes.Unlist = append(m.changes.Unlist, player)
	delete(m.listings, player)
	if announce {
		m.unlisted = append(m.unlisted, l)
	}
}

// ruleErrors are the reasons an accepted offer can fail to complete; any
// other error from complete is a broken invariant.
var ruleErrors = []error{ErrWindowClosed, ErrNotTransferable, ErrSquadMinimum, ErrPlayerRefuses, ErrSquadFull, ErrCannotAfford}

func isRuleError(err error) bool {
	return slices.ContainsFunc(ruleErrors, func(r error) bool { return errors.Is(err, r) })
}

// failedStatus is how an accepted offer closes when complete fails with a
// rule error: refused if the player refused to join, else collapsed.
func failedStatus(err error) transfers.Status {
	if errors.Is(err, ErrPlayerRefuses) {
		return transfers.StatusRefused
	}
	return transfers.StatusCollapsed
}

// complete stages an accepted offer's transfer at m.at: the player leaves
// the seller and joins the buyer's senior team on the offer's terms, the
// fee moves between the ledgers, and every other open offer for the player
// collapses and his listing ends. It revalidates the rules first: the
// window is open, the seller still employs the player and he has not moved
// in this window, the seller keeps its minimum at his position, the player
// agrees to join the buyer (joins, against the squads as staged now: his
// consent binds only at completion, whoever the seller), the buyer has room
// for him (hasRoom) and can pay the fee. A rule failure returns one of
// ruleErrors and stages nothing.
func (m *market) complete(o transfers.Offer) error {
	pos := m.position[o.Player]
	q := m.w.defs.Quota(pos)
	if err := m.w.checkAdmission(m.counts[o.Buyer], pos); err != nil {
		return fmt.Errorf("%w: club %d has %d players, %d %s", err, o.Buyer, squadSize(m.counts[o.Buyer]), m.counts[o.Buyer][pos], pos)
	}
	switch {
	case m.at < m.open || m.at >= m.close:
		return fmt.Errorf("%w at %d", ErrWindowClosed, m.at)
	case m.employer[o.Player] != o.Seller || m.moved[o.Player]:
		return fmt.Errorf("%w: player %d is not at club %d or has moved", ErrNotTransferable, o.Player, o.Seller)
	case m.counts[o.Seller][pos] <= q.Min:
		return fmt.Errorf("%w: club %d has %d %s", ErrSquadMinimum, o.Seller, m.counts[o.Seller][pos], pos)
	case !m.joins(o.Player, o.Buyer):
		return fmt.Errorf("%w: player %d (%d) of club %d (average %d) to club %d (average %d)", ErrPlayerRefuses,
			o.Player, m.overall(o.Player), o.Seller, m.average(o.Seller), o.Buyer, m.average(o.Buyer))
	case m.balances[o.Buyer] < o.Fee:
		return fmt.Errorf("%w: club %d has %s, fee %s", ErrCannotAfford, o.Buyer, m.balances[o.Buyer], o.Fee)
	}
	contract, err := m.w.transferContract(m.at, o.Terms)
	if err != nil {
		return err
	}
	old, _ := m.w.employment.Assignment(o.Player) // not moved in this window: the store's assignment
	buyerBalance, err1 := m.balances[o.Buyer].Add(-o.Fee)
	sellerBalance, err2 := m.balances[o.Seller].Add(o.Fee)
	buyerWages, err3 := m.wages[o.Buyer].Add(contract.WeeklyWage)
	if err := errors.Join(err1, err2, err3); err != nil {
		return err
	}
	team, _ := m.w.registry.SeniorTeam(o.Buyer)
	m.jobs.Departures = append(m.jobs.Departures, o.Player)
	m.jobs.Signings = append(m.jobs.Signings, employment.Assignment{Player: o.Player, Club: o.Buyer, Team: team, Contract: contract})
	m.postings = append(m.postings,
		finance.Posting{Club: o.Buyer, Kind: finance.KindTransfer, Amount: -o.Fee, Offer: o.ID},
		finance.Posting{Club: o.Seller, Kind: finance.KindTransfer, Amount: o.Fee, Offer: o.ID})
	m.balances[o.Buyer], m.balances[o.Seller] = buyerBalance, sellerBalance
	m.wages[o.Buyer], m.wages[o.Seller] = buyerWages, m.wages[o.Seller]-old.Contract.WeeklyWage
	m.counts[o.Seller][pos]--
	m.counts[o.Buyer][pos]++
	m.overalls[o.Seller] -= m.overall(o.Player)
	m.overalls[o.Buyer] += m.overall(o.Player)
	m.employer[o.Player], m.moved[o.Player], m.bought[o.Buyer] = o.Buyer, true, true
	m.terms[o.ID] = contract
	m.unlist(o.Player, false)
	m.closeOffer(o, transfers.StatusCompleted)
	for _, id := range slices.Sorted(maps.Keys(m.offers)) {
		if other := m.offers[id]; other.Player == o.Player {
			m.closeOffer(other, transfers.StatusCollapsed)
		}
	}
	return nil
}

// closeOffer stages an open offer's closure.
func (m *market) closeOffer(o transfers.Offer, status transfers.Status) {
	m.changes.Close = append(m.changes.Close, transfers.Closure{Offer: o.ID, Status: status})
	delete(m.offers, o.ID)
	m.openBids[o.Buyer]--
	m.openFor[o.Player]--
}

// bid stages a new open offer with the deadline its seller has: the next
// run for an AI club, managerDeadline for the manager.
func (m *market) bid(player ids.PlayerID, buyer ids.ClubID, fee money.Money, terms transfers.Terms) (transfers.Bid, error) {
	seller := m.employer[player]
	deadline, err := m.w.nextTransferRun(m.at)
	if err != nil {
		return transfers.Bid{}, err
	}
	if seller == m.w.userClub {
		deadline = m.w.managerDeadline(m.at, m.close)
	}
	b := transfers.Bid{Player: player, Seller: seller, Buyer: buyer, Fee: fee, Terms: terms, Deadline: deadline}
	m.changes.Bids = append(m.changes.Bids, b)
	m.bidFor[[2]uint64{uint64(buyer), uint64(player)}] = true
	m.openBids[buyer]++
	m.openFor[player]++
	return b, nil
}

// sign stages an AI club signing a free agent in the window, on
// windowTerms.
func (m *market) sign(club ids.ClubID, player ids.PlayerID) error {
	p, _ := m.w.players.Profile(player)
	if err := m.w.checkAdmission(m.counts[club], p.Position); err != nil {
		return err
	}
	terms, err := m.w.windowTerms(player, m.open)
	if err != nil {
		return err
	}
	contract, err := m.w.transferContract(m.at, terms)
	if err != nil {
		return err
	}
	team, _ := m.w.registry.SeniorTeam(club)
	a := employment.Assignment{Player: player, Club: club, Team: team, Contract: contract}
	wages, err := m.wages[club].Add(contract.WeeklyWage)
	if err != nil {
		return err
	}
	m.jobs.Signings = append(m.jobs.Signings, a)
	m.wages[club] = wages
	m.counts[club][p.Position]++
	m.overalls[club] += p.Overall()
	m.employer[player], m.position[player] = club, p.Position
	m.pool = slices.DeleteFunc(m.pool, func(f ai.FreeAgent) bool { return f.Player == player })
	m.actions = append(m.actions, marketAction{bid: -1, signing: a})
	return nil
}

// freeAgentGrace is how long after the window opens the manager has the
// free-agent pool to himself: AI clubs sign free agents for vacancies above
// their roster minimum only after it (and at the close, see fillSquads).
func freeAgentGrace(windowDays int) sim.GameInstant {
	return sim.GameInstant(windowDays/2) * sim.GameInstant(sim.Day)
}

// needs lists a club's vacancies: positions below the roster count.
func (m *market) needs(club ids.ClubID) []ai.RoleCount {
	var out []ai.RoleCount
	for _, q := range m.w.defs.Roster {
		if n := q.Count - m.counts[club][q.Position]; n > 0 {
			out = append(out, ai.RoleCount{Role: roleOf(q.Position), Count: n})
		}
	}
	return out
}

// commit plans every module's part, lets extra (such as scheduling the next
// run) run before anything is applied, then applies the plans. Nothing
// after extra can fail. It returns the transfers and finance plans for the
// events.
func (m *market) commit(extra func() error) (transfers.Plan, finance.Plan, error) {
	w := m.w
	offerPlan, err := w.transfers.Plan(m.at, m.changes)
	if err != nil {
		return transfers.Plan{}, finance.Plan{}, err
	}
	jobPlan, err := w.employment.Plan(m.jobs)
	if err != nil {
		return transfers.Plan{}, finance.Plan{}, err
	}
	moneyPlan, err := w.finance.Plan(m.at, m.postings)
	if err != nil {
		return transfers.Plan{}, finance.Plan{}, err
	}
	if extra != nil {
		if err := extra(); err != nil {
			return transfers.Plan{}, finance.Plan{}, err
		}
	}
	// Committed. Nothing below can fail.
	if err := w.transfers.Apply(offerPlan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	w.applyEmployment(jobPlan)
	w.applyFinance(moneyPlan)
	return offerPlan, moneyPlan, nil
}

// emit stages the events of a committed market: each closed offer in
// closure order (TransferCompleted or OfferClosed), the fee postings, the
// announced listing changes (PlayerUnlisted, then PlayerListed), then the
// AI actions in order (TransferOffered or PlayerSigned).
func (m *market) emit(cause events.Cause, offerPlan transfers.Plan, moneyPlan finance.Plan) {
	w := m.w
	for _, o := range offerPlan.Closed() {
		deal := w.deal(o)
		if o.Status == transfers.StatusCompleted {
			c := m.terms[o.ID]
			w.emit(m.at, cause, events.Event{Kind: events.KindTransferCompleted, TransferCompleted: &events.TransferCompleted{
				Deal: deal, Expires: c.Expires, WeeklyWage: c.WeeklyWage,
			}})
			continue
		}
		w.emit(m.at, cause, events.Event{Kind: events.KindOfferClosed, OfferClosed: &events.OfferClosed{Deal: deal, Outcome: uint8(o.Status)}})
	}
	w.emitLedger(m.at, cause, moneyPlan)
	for _, l := range m.unlisted {
		team, _ := w.registry.SeniorTeam(l.Club)
		w.emit(m.at, cause, events.Event{Kind: events.KindPlayerUnlisted, PlayerUnlisted: &events.PlayerUnlisted{Player: l.Player, Club: l.Club, Team: team}})
	}
	for _, l := range m.listed {
		team, _ := w.registry.SeniorTeam(l.Club)
		w.emit(m.at, cause, events.Event{Kind: events.KindPlayerListed, PlayerListed: &events.PlayerListed{Player: l.Player, Club: l.Club, Team: team, Asking: l.Asking}})
	}
	made := offerPlan.Made()
	for _, a := range m.actions {
		if a.bid < 0 {
			s := a.signing
			w.emit(m.at, cause, events.Event{Kind: events.KindPlayerSigned, PlayerSigned: &events.PlayerSigned{
				Player: s.Player, Club: s.Club, Team: s.Team, Expires: s.Contract.Expires, WeeklyWage: s.Contract.WeeklyWage,
			}})
			continue
		}
		o := made[a.bid]
		w.emit(m.at, cause, events.Event{Kind: events.KindTransferOffered, TransferOffered: &events.TransferOffered{
			Deal: w.deal(o), Deadline: o.Deadline,
		}})
	}
}

// deal names an offer's parties for an event.
func (w *World) deal(o transfers.Offer) events.Deal {
	sellerTeam, _ := w.registry.SeniorTeam(o.Seller)
	buyerTeam, _ := w.registry.SeniorTeam(o.Buyer)
	return events.Deal{Offer: o.ID, Player: o.Player, Seller: o.Seller, SellerTeam: sellerTeam, Buyer: o.Buyer, BuyerTeam: buyerTeam, Fee: o.Fee}
}

// transferRun handles one day of the transfer window at `at`, as one
// all-or-nothing change:
//
//  1. Every open offer to an AI club due now is answered, in offer order
//     (ai.AcceptBid): the club accepts a fee of at least its price for the
//     player (see sellingPrice), for a player it can spare or can still
//     replace (see replaceable) and, unless it listed him, who did not join
//     it by transfer in the previous window. An accepted offer completes at
//     once, or the player refuses to join or it collapses (see complete).
//  2. Every open offer to the manager due now expires.
//  3. At the close, every AI club with a vacancy (a position below its
//     roster count) signs free agents to fill it, as at the contract year
//     (ai.Signings), and the transfer list is cleared.
//  4. Otherwise every AI club lists the players it does not need (see
//     listSurplus), then every AI club with no open bid, in club ID order,
//     acts once (see aiActions): for its largest need if it has a vacancy,
//     else for an upgrade.
//
// Then the next run is queued, the plans are applied, and the events are
// emitted.
func (w *World) transferRun(at sim.GameInstant, cohort []sim.Task) error {
	if len(cohort) != 1 || cohort[0].PayloadID != 0 {
		return fmt.Errorf("app: transfer cohort at %d has %d tasks, first payload %d", at, len(cohort), cohort[0].PayloadID)
	}
	m, err := w.newMarket(at)
	if err != nil {
		return err
	}
	replaceable, err := w.replaceable(at, m.close)
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(m.offers)) {
		o, open := m.offers[id]
		if !open || o.Deadline > at {
			continue // collapsed by an earlier completion, or not due
		}
		if o.Seller == w.userClub || at >= m.close {
			m.closeOffer(o, transfers.StatusExpired)
			continue
		}
		price, err := m.price(o.Player)
		if err != nil {
			return err
		}
		pos := m.position[o.Player]
		_, listed := m.listings[o.Player]
		if !ai.AcceptBid(ai.Sale{
			Fee: o.Fee, Price: price, Spare: m.counts[o.Seller][pos] > m.w.defs.Quota(pos).Count, Replaceable: replaceable,
			Listed: listed, Settling: m.settling[o.Player],
		}) {
			m.closeOffer(o, transfers.StatusRejected)
			continue
		}
		if err := m.complete(o); err != nil {
			if !isRuleError(err) {
				return err
			}
			m.closeOffer(o, failedStatus(err))
		}
	}
	m.pool = w.freeAgentPool()
	slices.SortStableFunc(m.pool, func(a, b ai.FreeAgent) int { return b.Overall - a.Overall })
	if at >= m.close {
		if err := m.fillSquads(); err != nil {
			return err
		}
		for _, id := range slices.Sorted(maps.Keys(m.listings)) {
			m.unlist(id, false)
		}
	} else {
		if err := m.listSurplus(); err != nil {
			return err
		}
		if err := m.aiActions(); err != nil {
			return err
		}
	}
	next, err := w.nextTransferRun(at)
	if err != nil {
		return err
	}
	offerPlan, moneyPlan, err := m.commit(func() error { return w.scheduleTransferRun(next) })
	if err != nil {
		return fmt.Errorf("app: transfer run at %d: %w", at, err)
	}
	m.emit(taskCause(cohort[0].ID), offerPlan, moneyPlan)
	return nil
}

// fillSquads signs free agents for every AI club's vacancies at the close.
func (m *market) fillSquads() error {
	var needs []ai.ClubNeeds
	for _, c := range m.w.registry.Clubs() {
		if c.ID == m.w.userClub {
			continue
		}
		if n := m.needs(c.ID); len(n) > 0 {
			needs = append(needs, ai.ClubNeeds{Club: c.ID, Needs: n})
		}
	}
	if len(needs) == 0 {
		return nil
	}
	signings, err := ai.Signings(needs, m.pool)
	if err != nil {
		return err
	}
	for _, s := range signings {
		if err := m.sign(s.Club, s.Player); err != nil {
			return err
		}
	}
	return nil
}

// members returns a club's players at a position, as staged.
func (m *market) members(club ids.ClubID, pos players.Position) []ai.Member {
	var out []ai.Member
	for _, id := range slices.Sorted(maps.Keys(m.employer)) {
		if m.employer[id] == club && m.position[id] == pos {
			p, _ := m.w.players.Profile(id)
			out = append(out, ai.Member{Player: id, Overall: p.Overall()})
		}
	}
	return out
}

// hasSurplus reports whether a club holds more players than its roster
// count at some position.
func (m *market) hasSurplus(club ids.ClubID) bool {
	return slices.ContainsFunc(m.w.defs.Roster, func(q content.Quota) bool { return m.counts[club][q.Position] > q.Count })
}

// listSurplus brings every AI club's transfer list in line with the players
// it does not need: at each position where it holds more than its roster
// count, everyone but its best (ai.Surplus) is listed at ai.ListingPrice of
// his valuation, except a player who moved in this window (he cannot be
// sold again in it); a listed player it needs again is taken off the list.
func (m *market) listSurplus() error {
	w := m.w
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub {
			continue
		}
		surplus := map[ids.PlayerID]bool{}
		for _, q := range w.defs.Roster {
			for _, id := range ai.Surplus(m.members(c.ID, q.Position), q.Count) {
				surplus[id] = !m.moved[id]
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.listings)) {
			if m.listings[id].Club == c.ID && !surplus[id] {
				m.unlist(id, true)
			}
		}
		for _, id := range slices.Sorted(maps.Keys(surplus)) {
			if _, listed := m.listings[id]; listed || !surplus[id] {
				continue
			}
			value, err := m.value(id)
			if err != nil {
				return err
			}
			m.list(id, c.ID, ai.ListingPrice(value))
		}
	}
	return nil
}

// aiActions lets every AI club with no open bid act once, in club ID order:
//
//   - With a vacancy, for its largest need: it bids its price for the best
//     player it can afford who is clearly better than the best free agent
//     at the position (ai.ChooseTarget), if the answer can still come inside
//     the window; else it signs that free agent. Once it has bought a player
//     in this window, it bids only for listed players: buying one leaves his
//     club without a vacancy, so no chain of purchases follows.
//   - Without one, for an upgrade: if the answer can still come inside the
//     window, it has not bought a player in this window and holds no
//     surplus, it bids its price for the player it can afford who most
//     improves on its weakest player in his role (ai.ChooseUpgrade). Once he
//     joins, it lists the player he replaces (see listSurplus).
//
// It bids only for players it may buy (see candidates).
func (m *market) aiActions() error {
	w := m.w
	next, err := w.nextTransferRun(m.at)
	if err != nil {
		return err
	}
	canBid := next < m.close
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub || m.openBids[c.ID] > 0 {
			continue
		}
		role, ok := ai.LargestNeed(m.needs(c.ID))
		if !ok {
			if canBid && !m.bought[c.ID] && !m.hasSurplus(c.ID) {
				if err := m.upgrade(c.ID); err != nil {
					return err
				}
			}
			continue
		}
		var pos players.Position
		for _, q := range w.defs.Roster {
			if roleOf(q.Position) == role {
				pos = q.Position
			}
		}
		best := -1 // the best free agent at the position, index into the pool
		for i, f := range m.pool {
			if f.Role == role {
				best = i
				break
			}
		}
		if canBid {
			floor := 0
			if best >= 0 {
				floor = m.pool[best].Overall
			}
			listedOnly := m.bought[c.ID]
			cands, err := m.candidates(c.ID, func(id ids.PlayerID) bool {
				_, listed := m.listings[id]
				return m.position[id] == pos && (listed || !listedOnly)
			})
			if err != nil {
				return err
			}
			budget := ai.TransferBudget(m.balances[c.ID], m.wages[c.ID])
			if target, ok := ai.ChooseTarget(c.ID, cands, floor, budget); ok {
				if err := m.aiBid(c.ID, target); err != nil {
					return err
				}
				continue
			}
		}
		if best >= 0 && m.at >= m.open+freeAgentGrace(w.defs.Transfers.WindowDays) {
			if err := m.sign(c.ID, m.pool[best].Player); err != nil {
				return err
			}
		}
	}
	return nil
}

// upgrade lets an AI club bid for an upgrade, if it finds one.
func (m *market) upgrade(club ids.ClubID) error {
	weakest := map[matches.Role]int{}
	for _, q := range m.w.defs.Roster {
		for i, mem := range m.members(club, q.Position) {
			if r := roleOf(q.Position); i == 0 || mem.Overall < weakest[r] {
				weakest[r] = mem.Overall
			}
		}
	}
	cands, err := m.candidates(club, func(ids.PlayerID) bool { return true })
	if err != nil {
		return err
	}
	target, ok := ai.ChooseUpgrade(club, cands, weakest, ai.TransferBudget(m.balances[club], m.wages[club]))
	if !ok {
		return nil
	}
	return m.aiBid(club, target)
}

// aiBid stages an AI club's bid of its target's price, on windowTerms.
func (m *market) aiBid(club ids.ClubID, target ai.TransferCandidate) error {
	terms, err := m.w.windowTerms(target.Player, m.open)
	if err != nil {
		return err
	}
	if _, err := m.bid(target.Player, club, target.Value, terms); err != nil {
		return err
	}
	m.actions = append(m.actions, marketAction{bid: len(m.changes.Bids) - 1})
	return nil
}

// candidates lists the players ok accepts that an AI club may bid for, at
// their clubs' prices: of another club that keeps its minimum at the
// position, not moved in this window, not bid for by the club in this
// window before, and, of the manager's players, only those he has listed.
// It leaves out players another club has an open bid for (the earlier bid
// is answered first), and players an AI club would refuse to sell: unlisted
// ones who joined it by transfer in the previous window, and ones it needs
// when the answer comes too late for it to replace them (see replaceable).
func (m *market) candidates(club ids.ClubID, ok func(ids.PlayerID) bool) ([]ai.TransferCandidate, error) {
	w := m.w
	answer, err := w.nextTransferRun(m.at)
	if err != nil {
		return nil, err
	}
	replaceable, err := w.replaceable(answer, m.close)
	if err != nil {
		return nil, err
	}
	var cands []ai.TransferCandidate
	for _, id := range slices.Sorted(maps.Keys(m.employer)) {
		seller, pos := m.employer[id], m.position[id]
		_, listed := m.listings[id]
		q := w.defs.Quota(pos)
		if seller == club || !ok(id) || m.moved[id] || m.openFor[id] > 0 || m.bidFor[[2]uint64{uint64(club), uint64(id)}] ||
			m.counts[seller][pos] <= q.Min || (seller == w.userClub && !listed) ||
			(seller != w.userClub && !replaceable && m.counts[seller][pos] <= q.Count) ||
			(seller != w.userClub && !listed && m.settling[id]) || !m.joins(id, club) {
			continue
		}
		if _, stored := w.employment.Assignment(id); !stored {
			continue // signed in this run
		}
		price, err := m.price(id)
		if err != nil {
			return nil, err
		}
		p, _ := w.players.Profile(id)
		cands = append(cands, ai.TransferCandidate{Player: id, Club: seller, Role: roleOf(pos), Overall: p.Overall(), Value: price, Listed: listed})
	}
	return cands, nil
}

// MakeTransferOffer bids Fee for another club's player on behalf of the
// user club, offering the player a contract of Offer.Years contract years
// (the current one included) at Offer.WeeklyWage. The selling club answers
// at the next transfer run: an AI club accepts a fee of at least its price
// for the player (see sellingPrice), unless it bought him in the previous
// window and has not listed him, late in the window only for a player it
// can spare (see replaceable); the transfer then completes at once if the
// rules still allow it and the player agrees to join (see complete). A bid
// needs an open window with a run left before the close, a player of
// another club who has not moved in this window, and it must be
// the user club's first bid for him in this window. The user club must be
// below the squad limit, at any position, and have the fee in hand; the
// seller must keep its minimum there; the player accepts a wage from his
// demand up to the offer ceiling.
type MakeTransferOffer struct {
	ID               CommandID
	ExpectedRevision Revision
	Player           ids.PlayerID
	Fee              money.Money
	Offer            ContractOffer
}

// TransferOfferMade is the recorded result of MakeTransferOffer.
type TransferOfferMade struct {
	Command  CommandID
	Revision Revision
	Offer    ids.OfferID
	Deadline sim.GameInstant // the run that answers it
}

type OfferRecord struct {
	Request MakeTransferOffer
	Result  TransferOfferMade
}

// RespondToOffer answers an open bid for one of the user club's players
// before its deadline. Accepting completes the transfer at once (see
// complete): it is refused, changing nothing, if the sale would leave the
// squad below its minimum at the player's position; if the player now
// refuses to join the buyer (ai.Joins against the current squads, as for an
// AI seller), the offer closes as refused; if the buyer can no longer
// complete it, it collapses. Declining rejects it.
type RespondToOffer struct {
	ID               CommandID
	ExpectedRevision Revision
	Offer            ids.OfferID
	Accept           bool
}

// OfferAnswered is the recorded result of RespondToOffer: the offer's final
// status (completed, rejected, refused or collapsed) and, when completed, the
// player's new contract.
type OfferAnswered struct {
	Command  CommandID
	Revision Revision
	Offer    ids.OfferID
	Status   transfers.Status
	Contract employment.Contract
}

type ResponseRecord struct {
	Request RespondToOffer
	Result  OfferAnswered
}

// MakeTransferOffer records a bid (see the MakeTransferOffer type). Retries
// follow ResolveRounds. On error nothing changes.
func (w *World) MakeTransferOffer(cmd MakeTransferOffer) (TransferOfferMade, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.offer != nil && r.offer.Request == cmd })
	if err != nil {
		return TransferOfferMade{}, err
	}
	if retry {
		return rec.offer.Result, nil
	}
	if _, pending := w.pendingRounds(); pending {
		return TransferOfferMade{}, ErrSquadsLocked
	}
	m, err := w.newMarket(w.Now())
	if err != nil {
		return TransferOfferMade{}, err
	}
	next, err := w.nextTransferRun(w.Now())
	if err != nil {
		return TransferOfferMade{}, err
	}
	if w.Now() >= m.close || next >= m.close {
		return TransferOfferMade{}, fmt.Errorf("%w: bids are answered only inside the window", ErrWindowClosed)
	}
	seller, employed := m.employer[cmd.Player]
	switch {
	case !employed || seller == w.userClub || m.moved[cmd.Player]:
		return TransferOfferMade{}, fmt.Errorf("%w: player %d is not at another club or has moved in this window", ErrNotTransferable, cmd.Player)
	case m.bidFor[[2]uint64{uint64(w.userClub), uint64(cmd.Player)}]:
		return TransferOfferMade{}, fmt.Errorf("%w: player %d", ErrAlreadyBid, cmd.Player)
	}
	pos := m.position[cmd.Player]
	q := w.defs.Quota(pos)
	switch {
	case m.counts[seller][pos] <= q.Min:
		return TransferOfferMade{}, fmt.Errorf("%w: the selling club has %d %s", ErrSquadMinimum, m.counts[seller][pos], pos)
	case cmd.Fee <= 0 || cmd.Fee > m.balances[w.userClub]:
		return TransferOfferMade{}, fmt.Errorf("%w: fee %s, balance %s", ErrCannotAfford, cmd.Fee, m.balances[w.userClub])
	}
	if err := w.checkAdmission(m.counts[w.userClub], pos); err != nil {
		return TransferOfferMade{}, fmt.Errorf("%w: %d players of %d", err, squadSize(m.counts[w.userClub]), w.defs.SquadLimit)
	}
	if err := w.checkOffer(cmd.Player, cmd.Offer); err != nil {
		return TransferOfferMade{}, err
	}
	b, err := m.bid(cmd.Player, w.userClub, cmd.Fee, transfers.Terms(cmd.Offer))
	if err != nil {
		return TransferOfferMade{}, err
	}
	plan, err := w.transfers.Plan(w.Now(), m.changes)
	if err != nil {
		return TransferOfferMade{}, err
	}

	// Committed. Nothing below can fail.
	if err := w.transfers.Apply(plan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	w.revision++
	o := plan.Made()[0]
	res := TransferOfferMade{Command: cmd.ID, Revision: w.revision, Offer: o.ID, Deadline: b.Deadline}
	w.commands[cmd.ID] = commandRecord{offer: &OfferRecord{Request: cmd, Result: res}}
	w.emit(w.Now(), commandCause(cmd.ID), events.Event{Kind: events.KindTransferOffered, TransferOffered: &events.TransferOffered{
		Deal: w.deal(o), Deadline: o.Deadline,
	}})
	w.publish()
	return res, nil
}

// RespondToOffer records an answer (see the RespondToOffer type). Retries
// follow ResolveRounds. On error nothing changes.
func (w *World) RespondToOffer(cmd RespondToOffer) (OfferAnswered, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.respond != nil && r.respond.Request == cmd })
	if err != nil {
		return OfferAnswered{}, err
	}
	if retry {
		return rec.respond.Result, nil
	}
	if _, pending := w.pendingRounds(); pending {
		return OfferAnswered{}, ErrSquadsLocked
	}
	m, err := w.newMarket(w.Now())
	if err != nil {
		return OfferAnswered{}, err
	}
	o, open := m.offers[cmd.Offer]
	if !open || o.Seller != w.userClub || o.Deadline <= w.Now() {
		return OfferAnswered{}, fmt.Errorf("%w: offer %d", ErrNoSuchOffer, cmd.Offer)
	}
	status := transfers.StatusRejected
	if cmd.Accept {
		status = transfers.StatusCompleted
		if err := m.complete(o); err != nil {
			if !isRuleError(err) || errors.Is(err, ErrSquadMinimum) {
				return OfferAnswered{}, err
			}
			status = failedStatus(err)
		}
	}
	if status != transfers.StatusCompleted {
		m.closeOffer(o, status)
	}
	offerPlan, moneyPlan, err := m.commit(nil)
	if err != nil {
		return OfferAnswered{}, err
	}
	w.revision++
	res := OfferAnswered{Command: cmd.ID, Revision: w.revision, Offer: o.ID, Status: status, Contract: m.terms[o.ID]}
	w.commands[cmd.ID] = commandRecord{respond: &ResponseRecord{Request: cmd, Result: res}}
	m.emit(commandCause(cmd.ID), offerPlan, moneyPlan)
	w.publish()
	return res, nil
}

// ListPlayer puts a user-club player on the transfer list at Asking, or
// changes his asking price if he is listed; Asking zero takes him off the
// list. AI clubs that need a player at his position (a vacancy, or an
// upgrade on their weakest there) may then bid Asking for him, and the
// manager answers as for any bid. Listing needs an open window with a run
// left before the close, like a bid, a player who has not moved in this
// window, and a squad above its minimum at his position (else no club may
// buy him); the listing ends when he leaves the club or the window closes.
type ListPlayer struct {
	ID               CommandID
	ExpectedRevision Revision
	Player           ids.PlayerID
	Asking           money.Money
}

// PlayerListed is the recorded result of ListPlayer.
type PlayerListed struct {
	Command  CommandID
	Revision Revision
	Player   ids.PlayerID
	Asking   money.Money // zero: taken off the list
}

type ListingRecord struct {
	Request ListPlayer
	Result  PlayerListed
}

// ListPlayer records a listing change (see the ListPlayer type). Retries
// follow ResolveRounds. On error nothing changes.
func (w *World) ListPlayer(cmd ListPlayer) (PlayerListed, error) {
	rec, retry, err := w.checkCommand(cmd.ID, cmd.ExpectedRevision, func(r commandRecord) bool { return r.listing != nil && r.listing.Request == cmd })
	if err != nil {
		return PlayerListed{}, err
	}
	if retry {
		return rec.listing.Result, nil
	}
	a, ok := w.employment.Assignment(cmd.Player)
	if !ok || a.Club != w.userClub {
		return PlayerListed{}, fmt.Errorf("%w: player %d", ErrNotUserPlayer, cmd.Player)
	}
	m, err := w.newMarket(w.Now())
	if err != nil {
		return PlayerListed{}, err
	}
	next, err := w.nextTransferRun(w.Now())
	if err != nil {
		return PlayerListed{}, err
	}
	_, listed := m.listings[cmd.Player]
	switch {
	case cmd.Asking < 0:
		return PlayerListed{}, fmt.Errorf("%w: %s", ErrInvalidPrice, cmd.Asking)
	case cmd.Asking == 0 && !listed:
		return PlayerListed{}, fmt.Errorf("%w: player %d", ErrNotListed, cmd.Player)
	case cmd.Asking == 0:
		m.unlist(cmd.Player, true)
	case w.Now() >= m.close || next >= m.close:
		return PlayerListed{}, fmt.Errorf("%w: players are listed only while bids can be answered", ErrWindowClosed)
	case m.moved[cmd.Player]:
		return PlayerListed{}, fmt.Errorf("%w: player %d has moved in this window", ErrNotTransferable, cmd.Player)
	case m.counts[w.userClub][m.position[cmd.Player]] <= w.defs.Quota(m.position[cmd.Player]).Min:
		pos := m.position[cmd.Player]
		return PlayerListed{}, fmt.Errorf("%w: %d %s, minimum %d", ErrSquadMinimum, m.counts[w.userClub][pos], pos, w.defs.Quota(pos).Min)
	default:
		m.list(cmd.Player, w.userClub, cmd.Asking)
	}
	offerPlan, moneyPlan, err := m.commit(nil)
	if err != nil {
		return PlayerListed{}, err
	}

	// Committed. Nothing below can fail.
	w.revision++
	res := PlayerListed{Command: cmd.ID, Revision: w.revision, Player: cmd.Player, Asking: cmd.Asking}
	w.commands[cmd.ID] = commandRecord{listing: &ListingRecord{Request: cmd, Result: res}}
	m.emit(commandCause(cmd.ID), offerPlan, moneyPlan)
	w.publish()
	return res, nil
}

// restoreListing validates a recorded ListPlayer: a fresh ID, a result
// within the revision range for the same registered player and asking
// price, which is not negative, and a user club. Later changes may have
// ended the listing, so it is not compared with the transfer list.
func (w *World) restoreListing(c ListingRecord, revision Revision) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if r.Revision <= q.ExpectedRevision || r.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", r.Revision, q.ExpectedRevision, revision)
	}
	if w.userClub == 0 || r.Player != q.Player || r.Asking != q.Asking || q.Asking < 0 {
		return fmt.Errorf("result %+v for player %d at %s, user club %d", r, q.Player, q.Asking, w.userClub)
	}
	if _, ok := w.registry.Player(q.Player); !ok {
		return fmt.Errorf("unknown player %d", q.Player)
	}
	return nil
}

// checkListingEvent compares a listing event with its cause: a manager's
// listing change matches its recorded command, and a transfer run lists and
// unlists only AI clubs' players.
func (w *World) checkListingEvent(e events.Event) error {
	var player ids.PlayerID
	var club ids.ClubID
	var team ids.TeamID
	var asking money.Money // zero: unlisted
	switch e.Kind {
	case events.KindPlayerListed:
		p := e.PlayerListed
		player, club, team, asking = p.Player, p.Club, p.Team, p.Asking
	case events.KindPlayerUnlisted:
		p := e.PlayerUnlisted
		player, club, team = p.Player, p.Club, p.Team
	}
	switch e.Cause.Kind {
	case events.CauseCommand:
		rec := w.commands[CommandID(e.Cause.ID)].listing
		if rec == nil || rec.Result.Player != player || rec.Result.Asking != asking || club != w.userClub {
			return errors.New("differs from the recorded listing")
		}
	default:
		if club == w.userClub {
			return errors.New("a transfer run listed a user-club player")
		}
	}
	return w.checkPlayerEvent(player, club, team)
}

// BidRefusal is why an AI club would refuse the manager's bid at its price
// for one of its players. Values are durable; never reorder.
type BidRefusal uint8

const (
	RefusalNone     BidRefusal = 0 // the club sells at its price
	RefusalStar     BidRefusal = 1 // a star of a stronger club (ai.Joins)
	RefusalSettling BidRefusal = 2 // unlisted, and bought in the previous window
	RefusalNeeded   BidRefusal = 3 // needed, and too late in the window to replace him (see replaceable)
)

// BidRefusal returns why the player's AI club would refuse a bid the
// manager made now at its price, or RefusalNone: also for the manager's
// own players, free agents and outside the bidding days of a window.
// Read-only.
func (w *World) BidRefusal(player ids.PlayerID) BidRefusal {
	m, err := w.newMarket(w.Now())
	if err != nil || w.Now() < m.open || w.Now() >= m.close {
		return RefusalNone
	}
	seller, employed := m.employer[player]
	if !employed || seller == w.userClub {
		return RefusalNone
	}
	pos := m.position[player]
	_, listed := m.listings[player]
	answer, err1 := w.nextTransferRun(w.Now())
	replaceable, err2 := w.replaceable(answer, m.close)
	switch {
	case err1 != nil || err2 != nil:
		return RefusalNone
	case !m.joins(player, w.userClub):
		return RefusalStar
	case !listed && m.settling[player]:
		return RefusalSettling
	case !replaceable && m.counts[seller][pos] <= w.defs.Quota(pos).Count:
		return RefusalNeeded
	}
	return RefusalNone
}

// ListedPlayer is a player on the transfer list, with his club and the
// time he was listed. Value is his asking price.
type ListedPlayer struct {
	SquadPlayer
	Club     ids.ClubID
	ClubName string
	ListedAt sim.GameInstant
}

// TransferList returns every player on the transfer list, in player ID
// order. Read-only.
func (w *World) TransferList() []ListedPlayer {
	var out []ListedPlayer
	for _, l := range w.transfers.Listings() {
		label, _ := w.ClubLabel(l.Club)
		out = append(out, ListedPlayer{SquadPlayer: w.squadPlayer(l.Player), Club: l.Club, ClubName: label.ClubName, ListedAt: l.ListedAt})
	}
	return out
}

// TransferWindow describes the transfer window under way, or the next one.
// Bids are made until BidsClose (exclusive), so that the answer comes
// inside the window; NextRun is when clubs next answer bids. An AI club
// sells a player it needs (it holds no more than its roster count at his
// position) only for a bid made before NeededClose: it rejects a later one,
// which leaves it too little time to replace him (see replaceable). Until
// FreeAgentsOpen only the manager signs free agents for vacancies above an
// AI club's roster minimum; AI clubs sign them from the first run at or
// after it (see freeAgentGrace).
type TransferWindow struct {
	Open           bool
	Opens          sim.GameInstant
	Closes         sim.GameInstant // exclusive
	BidsClose      sim.GameInstant // exclusive
	NeededClose    sim.GameInstant // exclusive
	FreeAgentsOpen sim.GameInstant
	NextRun        sim.GameInstant
}

// TransferWindow returns the window under way or the next one. Read-only.
func (w *World) TransferWindow() TransferWindow {
	now := w.Now()
	open, close, _ := w.transferWindow(now) // Now is always in range
	if now >= close {
		open, _ = w.contractYearEnd(now)
		close = open + sim.GameInstant(w.defs.Transfers.WindowDays)*sim.GameInstant(sim.Day)
	}
	next, _ := w.nextTransferRun(now)
	day := sim.GameInstant(sim.Day)
	return TransferWindow{Open: now >= open, Opens: open, Closes: close, BidsClose: close - day, NeededClose: max(close-3*day, open),
		FreeAgentsOpen: open + freeAgentGrace(w.defs.Transfers.WindowDays), NextRun: next}
}

// OfferView is a transfer offer with display names.
type OfferView struct {
	transfers.Offer
	PlayerName string
	SellerName string
	BuyerName  string
}

// Offers returns every transfer offer ever made, oldest first. Read-only.
func (w *World) Offers() []OfferView {
	var out []OfferView
	for _, o := range w.transfers.Offers() {
		v := OfferView{Offer: o}
		v.PlayerName, _ = w.PlayerName(o.Player)
		seller, _ := w.ClubLabel(o.Seller)
		buyer, _ := w.ClubLabel(o.Buyer)
		v.SellerName, v.BuyerName = seller.ClubName, buyer.ClubName
		out = append(out, v)
	}
	return out
}

// validateTransfers checks offers and fees against the world:
//
//   - every offer names registered clubs and player and was made inside a
//     window before now, with a deadline no later than that window's close;
//   - an open offer belongs to the window under way, is due after now, its
//     seller still employs the player, and its deadline is the next run
//     after it (an AI seller) or managerDeadline (the manager); a buyer has
//     at most one open offer per player;
//   - a closed offer closed by now and by its deadline, an expired one
//     exactly at it; a player completes at most one transfer per window;
//   - every transfer fee belongs to a completed offer, and every completed
//     offer moved its fee exactly once: from the buyer's ledger to the
//     seller's, when it completed;
//   - every listed player is employed by the listing club and was listed
//     inside the window under way, which is still open, and has not moved
//     in it;
//   - exactly one transfer-run task is queued, in the Decisions phase with
//     no payload, due at the next run.
func (w *World) validateTransfers() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	now := w.Now()
	currentOpen, currentClose, err := w.transferWindow(now)
	if err != nil {
		return append(errs, err)
	}
	fees := map[ids.OfferID][]finance.Entry{}
	for _, e := range w.finance.All() {
		if e.Kind == finance.KindTransfer {
			fees[e.Offer] = append(fees[e.Offer], e)
		}
	}
	moves := map[[2]int64]bool{}
	openBids := map[[2]uint64]bool{}
	for _, o := range w.transfers.Offers() {
		_, sellerOK := w.registry.Club(o.Seller)
		_, buyerOK := w.registry.Club(o.Buyer)
		_, playerOK := w.registry.Player(o.Player)
		open, close, err := w.transferWindow(o.MadeAt)
		switch {
		case !sellerOK || !buyerOK || !playerOK:
			fail("offer %d names an unknown club or player", o.ID)
			continue
		case err != nil || o.MadeAt > now || o.MadeAt >= close || o.Deadline > close:
			fail("offer %d made at %d with deadline %d is outside a window", o.ID, o.MadeAt, o.Deadline)
			continue
		}
		switch o.Status {
		case transfers.StatusOpen:
			a, employed := w.employment.Assignment(o.Player)
			want, err := w.nextTransferRun(o.MadeAt)
			if o.Seller == w.userClub {
				want = w.managerDeadline(o.MadeAt, close)
			}
			key := [2]uint64{uint64(o.Buyer), uint64(o.Player)}
			switch {
			case open != currentOpen || o.Deadline <= now:
				fail("offer %d is open after its window or deadline", o.ID)
			case !employed || a.Club != o.Seller:
				fail("open offer %d for player %d, who is not at club %d", o.ID, o.Player, o.Seller)
			case err != nil || o.Deadline != want:
				fail("open offer %d has deadline %d, want %d", o.ID, o.Deadline, want)
			case openBids[key]:
				fail("club %d has two open offers for player %d", o.Buyer, o.Player)
			}
			openBids[key] = true
		default:
			if o.ClosedAt > now || o.ClosedAt > o.Deadline || (o.Status == transfers.StatusExpired && o.ClosedAt != o.Deadline) {
				fail("offer %d %s at %d, deadline %d", o.ID, o.Status, o.ClosedAt, o.Deadline)
			}
		}
		if o.Status != transfers.StatusCompleted {
			if len(fees[o.ID]) > 0 {
				fail("offer %d is %s but moved a fee", o.ID, o.Status)
			}
			continue
		}
		key := [2]int64{int64(open), int64(o.Player)}
		if moves[key] {
			fail("player %d completed two transfers in the window opened at %d", o.Player, open)
		}
		moves[key] = true
		if f := fees[o.ID]; len(f) != 2 || f[0].Club != o.Buyer || f[0].Amount != -o.Fee || f[1].Club != o.Seller ||
			f[1].Amount != o.Fee || f[0].At != o.ClosedAt || f[1].At != o.ClosedAt {
			fail("completed offer %d did not move its fee %s once from club %d to club %d", o.ID, o.Fee, o.Buyer, o.Seller)
		}
	}
	for id := range fees {
		if _, ok := w.transfers.Offer(id); !ok {
			fail("transfer fee for unknown offer %d", id)
		}
	}
	for _, l := range w.transfers.Listings() {
		a, employed := w.employment.Assignment(l.Player)
		open, _, err := w.transferWindow(l.ListedAt)
		switch {
		case !employed || a.Club != l.Club:
			fail("player %d is listed by club %d, which does not employ him", l.Player, l.Club)
		case err != nil || open != currentOpen || l.ListedAt > now || now >= currentClose:
			fail("player %d was listed at %d, outside the open window", l.Player, l.ListedAt)
		case moves[[2]int64{int64(currentOpen), int64(l.Player)}]:
			fail("player %d is listed after moving in this window", l.Player)
		}
	}

	next, err := w.nextTransferRun(now)
	if err != nil {
		return append(errs, err)
	}
	tasks := 0
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskTransferRun {
			continue
		}
		tasks++
		if t.PayloadID != 0 || t.Phase != sim.PhaseDecisions || t.DueAt != next {
			fail("transfer-run task %d due %d phase %d payload %d, want due %d", t.ID, t.DueAt, t.Phase, t.PayloadID, next)
		}
	}
	if tasks != 1 {
		fail("%d transfer-run tasks queued, want 1", tasks)
	}
	return errs
}

// checkTransferEvent compares a transfer event with the offer it names.
func (w *World) checkTransferEvent(e events.Event) error {
	var deal events.Deal
	switch e.Kind {
	case events.KindTransferOffered:
		deal = e.TransferOffered.Deal
	case events.KindTransferCompleted:
		deal = e.TransferCompleted.Deal
	case events.KindOfferClosed:
		deal = e.OfferClosed.Deal
	}
	o, ok := w.transfers.Offer(deal.Offer)
	if !ok || w.deal(o) != deal {
		return fmt.Errorf("deal %+v differs from offer %d", deal, deal.Offer)
	}
	switch e.Kind {
	case events.KindTransferOffered:
		if o.MadeAt != e.OccurredAt || o.Deadline != e.TransferOffered.Deadline {
			return fmt.Errorf("offer %d was made at %d with deadline %d", o.ID, o.MadeAt, o.Deadline)
		}
	case events.KindTransferCompleted:
		c, err := w.transferContract(o.ClosedAt, o.Terms)
		p := e.TransferCompleted
		if err != nil || o.Status != transfers.StatusCompleted || o.ClosedAt != e.OccurredAt || c.Expires != p.Expires || c.WeeklyWage != p.WeeklyWage {
			return fmt.Errorf("offer %d is %s at %d on terms %+v", o.ID, o.Status, o.ClosedAt, o.Terms)
		}
	case events.KindOfferClosed:
		if uint8(o.Status) != e.OfferClosed.Outcome || o.ClosedAt != e.OccurredAt {
			return fmt.Errorf("offer %d is %s at %d", o.ID, o.Status, o.ClosedAt)
		}
	}
	return nil
}

// restoreOffer validates a recorded MakeTransferOffer against the offer it
// made: a fresh ID, a result within the revision range, and an offer by the
// user club for that player with that fee, terms and deadline.
func (w *World) restoreOffer(c OfferRecord, revision Revision) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if r.Revision <= q.ExpectedRevision || r.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", r.Revision, q.ExpectedRevision, revision)
	}
	o, ok := w.transfers.Offer(r.Offer)
	if !ok || w.userClub == 0 || o.Buyer != w.userClub || o.Player != q.Player || o.Fee != q.Fee ||
		o.Terms != transfers.Terms(q.Offer) || o.Deadline != r.Deadline {
		return fmt.Errorf("offer %d does not match the request", r.Offer)
	}
	return nil
}

// restoreResponse validates a recorded RespondToOffer against the offer it
// answered: a fresh ID, a result within the revision range, an offer for a
// user-club player closed with the recorded status (rejected exactly when
// declined), and a completed one's contract on its terms.
func (w *World) restoreResponse(c ResponseRecord, revision Revision) error {
	q, r := c.Request, c.Result
	if err := w.checkRecordID(q.ID, r.Command); err != nil {
		return err
	}
	if r.Revision <= q.ExpectedRevision || r.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", r.Revision, q.ExpectedRevision, revision)
	}
	o, ok := w.transfers.Offer(q.Offer)
	switch {
	case !ok || w.userClub == 0 || o.Seller != w.userClub || r.Offer != q.Offer || o.Status != r.Status:
		return fmt.Errorf("offer %d does not match the answer", q.Offer)
	case (r.Status == transfers.StatusRejected) == q.Accept || r.Status == transfers.StatusOpen || r.Status == transfers.StatusExpired:
		return fmt.Errorf("offer %d answered %t closed as %s", q.Offer, q.Accept, r.Status)
	}
	var want employment.Contract
	if r.Status == transfers.StatusCompleted {
		var err error
		if want, err = w.transferContract(o.ClosedAt, o.Terms); err != nil {
			return err
		}
	}
	if r.Contract != want {
		return fmt.Errorf("offer %d answered with contract %+v, want %+v", q.Offer, r.Contract, want)
	}
	return nil
}
