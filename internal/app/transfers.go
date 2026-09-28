package app

import (
	"errors"
	"fmt"
	"maps"
	"slices"

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
// staged changes, the open offers, and the changes themselves. Decisions
// later in a run see the earlier ones.
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
	bought   map[ids.ClubID]bool                 // bought a player in this window
	bidFor   map[[2]uint64]bool                  // (buyer, player) offers made in this window
	offers   map[ids.OfferID]transfers.Offer     // open offers
	openBids map[ids.ClubID]int                  // open offers by buyer
	pool     []ai.FreeAgent                      // free agents not signed yet, best first
	terms    map[ids.OfferID]employment.Contract // completed offers' contracts

	jobs     employment.Changes
	postings []finance.Posting
	changes  transfers.Changes
	actions  []marketAction // AI bids and signings, in order
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
		wages: map[ids.ClubID]money.Money{}, moved: map[ids.PlayerID]bool{}, bought: map[ids.ClubID]bool{}, bidFor: map[[2]uint64]bool{},
		offers: map[ids.OfferID]transfers.Offer{}, openBids: map[ids.ClubID]int{},
		terms: map[ids.OfferID]employment.Contract{},
	}
	for _, c := range w.registry.Clubs() {
		m.counts[c.ID] = map[players.Position]int{}
		m.balances[c.ID], _ = w.finance.Balance(c.ID)
		if m.wages[c.ID], err = w.employment.WageBill(c.ID); err != nil {
			return nil, err
		}
	}
	for _, a := range w.employment.Assignments() {
		p, _ := w.players.Profile(a.Player)
		m.employer[a.Player], m.position[a.Player] = a.Club, p.Position
		m.counts[a.Club][p.Position]++
	}
	for _, o := range w.transfers.Offers() {
		if o.MadeAt >= open {
			m.bidFor[[2]uint64{uint64(o.Buyer), uint64(o.Player)}] = true
		}
		if o.Status == transfers.StatusCompleted && o.ClosedAt >= open {
			m.moved[o.Player], m.bought[o.Buyer] = true, true
		}
		if o.Status == transfers.StatusOpen {
			m.offers[o.ID] = o
			m.openBids[o.Buyer]++
		}
	}
	return m, nil
}

// ruleErrors are the reasons an accepted offer can fail to complete; any
// other error from complete is a broken invariant.
var ruleErrors = []error{ErrWindowClosed, ErrNotTransferable, ErrSquadMinimum, ErrSquadFull, ErrCannotAfford}

func isRuleError(err error) bool {
	return slices.ContainsFunc(ruleErrors, func(r error) bool { return errors.Is(err, r) })
}

// complete stages an accepted offer's transfer at m.at: the player leaves
// the seller and joins the buyer's senior team on the offer's terms, the
// fee moves between the ledgers, and every other open offer for the player
// collapses. It revalidates the rules first: the window is open, the seller
// still employs the player and he has not moved in this window, the seller
// keeps its minimum at his position, the buyer has room for him (hasRoom)
// and can pay the fee. A rule failure returns one of ruleErrors and stages
// nothing.
func (m *market) complete(o transfers.Offer) error {
	pos := m.position[o.Player]
	q := m.w.defs.Quota(pos)
	switch {
	case m.at < m.open || m.at >= m.close:
		return fmt.Errorf("%w at %d", ErrWindowClosed, m.at)
	case m.employer[o.Player] != o.Seller || m.moved[o.Player]:
		return fmt.Errorf("%w: player %d is not at club %d or has moved", ErrNotTransferable, o.Player, o.Seller)
	case m.counts[o.Seller][pos] <= q.Min:
		return fmt.Errorf("%w: club %d has %d %s", ErrSquadMinimum, o.Seller, m.counts[o.Seller][pos], pos)
	case !m.w.hasRoom(o.Buyer, m.counts[o.Buyer], pos):
		return fmt.Errorf("%w: club %d has %d players, %d %s", ErrSquadFull, o.Buyer, squadSize(m.counts[o.Buyer]), m.counts[o.Buyer][pos], pos)
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
	m.employer[o.Player], m.moved[o.Player], m.bought[o.Buyer] = o.Buyer, true, true
	m.terms[o.ID] = contract
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
	return b, nil
}

// sign stages an AI club signing a free agent in the window, on
// windowTerms.
func (m *market) sign(club ids.ClubID, player ids.PlayerID) error {
	terms, err := m.w.windowTerms(player, m.open)
	if err != nil {
		return err
	}
	contract, err := m.w.transferContract(m.at, terms)
	if err != nil {
		return err
	}
	p, _ := m.w.players.Profile(player)
	team, _ := m.w.registry.SeniorTeam(club)
	a := employment.Assignment{Player: player, Club: club, Team: team, Contract: contract}
	wages, err := m.wages[club].Add(contract.WeeklyWage)
	if err != nil {
		return err
	}
	m.jobs.Signings = append(m.jobs.Signings, a)
	m.wages[club] = wages
	m.counts[club][p.Position]++
	m.employer[player], m.position[player] = club, p.Position
	m.pool = slices.DeleteFunc(m.pool, func(f ai.FreeAgent) bool { return f.Player == player })
	m.actions = append(m.actions, marketAction{bid: -1, signing: a})
	return nil
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
// closure order (TransferCompleted or OfferClosed), the fee postings, then
// the AI actions in order (TransferOffered or PlayerSigned).
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
//  1. Every open offer to an AI club due now is answered, in offer order:
//     the club accepts a fee of at least its valuation (ai.AcceptBid), and
//     an accepted offer completes at once or collapses (see complete).
//  2. Every open offer to the manager due now expires.
//  3. At the close, every AI club with a vacancy (a position below its
//     roster count) signs free agents to fill it, as at the contract year
//     (ai.Signings).
//  4. Otherwise every AI club with a vacancy and no open bid, in club ID
//     order, acts once for its largest need: it bids its valuation for the
//     best player it can afford who is clearly better than the best free
//     agent at the position (ai.ChooseTarget), if the answer can still come
//     inside the window and it has not bought a player in this window yet;
//     else it signs that free agent. It bids only for players it may buy:
//     of another club that keeps its minimum at the position, not moved in
//     this window, and not bid for by the club in this window before.
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
	for _, id := range slices.Sorted(maps.Keys(m.offers)) {
		o, open := m.offers[id]
		if !open || o.Deadline > at {
			continue // collapsed by an earlier completion, or not due
		}
		if o.Seller == w.userClub || at >= m.close {
			m.closeOffer(o, transfers.StatusExpired)
			continue
		}
		value, err := w.valuation(o.Player, at)
		if err != nil {
			return err
		}
		if !ai.AcceptBid(o.Fee, value) {
			m.closeOffer(o, transfers.StatusRejected)
			continue
		}
		if err := m.complete(o); err != nil {
			if !isRuleError(err) {
				return err
			}
			m.closeOffer(o, transfers.StatusCollapsed)
		}
	}
	m.pool = w.freeAgentPool()
	slices.SortStableFunc(m.pool, func(a, b ai.FreeAgent) int { return b.Overall - a.Overall })
	if at >= m.close {
		if err := m.fillSquads(); err != nil {
			return err
		}
	} else if err := m.aiActions(); err != nil {
		return err
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

// aiActions lets every AI club with a vacancy and no open bid act once (see
// transferRun).
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
		if canBid && !m.bought[c.ID] {
			floor := 0
			if best >= 0 {
				floor = m.pool[best].Overall
			}
			target, ok, err := m.target(c.ID, pos, floor)
			if err != nil {
				return err
			}
			if ok {
				terms, err := w.windowTerms(target.Player, m.open)
				if err != nil {
					return err
				}
				if _, err := m.bid(target.Player, c.ID, target.Value, terms); err != nil {
					return err
				}
				m.actions = append(m.actions, marketAction{bid: len(m.changes.Bids) - 1})
				continue
			}
		}
		if best >= 0 {
			if err := m.sign(c.ID, m.pool[best].Player); err != nil {
				return err
			}
		}
	}
	return nil
}

// target chooses the player an AI club bids for at a position, if any.
func (m *market) target(club ids.ClubID, pos players.Position, freeAgent int) (ai.TransferCandidate, bool, error) {
	w := m.w
	var cands []ai.TransferCandidate
	for _, id := range slices.Sorted(maps.Keys(m.employer)) {
		seller := m.employer[id]
		if seller == club || m.position[id] != pos || m.moved[id] || m.bidFor[[2]uint64{uint64(club), uint64(id)}] ||
			m.counts[seller][pos] <= w.defs.Quota(pos).Min {
			continue
		}
		if _, stored := w.employment.Assignment(id); !stored {
			continue // signed in this run
		}
		value, err := w.valuation(id, m.at)
		if err != nil {
			return ai.TransferCandidate{}, false, err
		}
		p, _ := w.players.Profile(id)
		cands = append(cands, ai.TransferCandidate{Player: id, Club: seller, Overall: p.Overall(), Value: value})
	}
	budget := ai.TransferBudget(m.balances[club], m.wages[club])
	c, ok := ai.ChooseTarget(club, cands, freeAgent, budget)
	return c, ok, nil
}

// MakeTransferOffer bids Fee for another club's player on behalf of the
// user club, offering the player a contract of Offer.Years contract years
// (the current one included) at Offer.WeeklyWage. The selling club answers
// at the next transfer run: an AI club accepts a fee of at least its
// valuation, and the transfer then completes at once if the rules still
// allow it. A bid needs an open window with a run left before the close, a
// player of another club who has not moved in this window, and it must be
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
// squad below its minimum at the player's position; if the buyer can no
// longer complete it, the offer collapses. Declining rejects it.
type RespondToOffer struct {
	ID               CommandID
	ExpectedRevision Revision
	Offer            ids.OfferID
	Accept           bool
}

// OfferAnswered is the recorded result of RespondToOffer: the offer's final
// status (completed, rejected or collapsed) and, when completed, the
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
	case !w.hasRoom(w.userClub, m.counts[w.userClub], pos):
		return TransferOfferMade{}, fmt.Errorf("%w: %d players of %d", ErrSquadFull, squadSize(m.counts[w.userClub]), w.defs.SquadLimit)
	case m.counts[seller][pos] <= q.Min:
		return TransferOfferMade{}, fmt.Errorf("%w: the selling club has %d %s", ErrSquadMinimum, m.counts[seller][pos], pos)
	case cmd.Fee <= 0 || cmd.Fee > m.balances[w.userClub]:
		return TransferOfferMade{}, fmt.Errorf("%w: fee %s, balance %s", ErrCannotAfford, cmd.Fee, m.balances[w.userClub])
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
			status = transfers.StatusCollapsed
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

// TransferWindow describes the transfer window under way, or the next one.
// Bids are made until BidsClose (exclusive), so that the answer comes
// inside the window; NextRun is when clubs next answer bids.
type TransferWindow struct {
	Open      bool
	Opens     sim.GameInstant
	Closes    sim.GameInstant // exclusive
	BidsClose sim.GameInstant // exclusive
	NextRun   sim.GameInstant
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
	return TransferWindow{Open: now >= open, Opens: open, Closes: close, BidsClose: close - sim.GameInstant(sim.Day), NextRun: next}
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
//   - exactly one transfer-run task is queued, in the Decisions phase with
//     no payload, due at the next run.
func (w *World) validateTransfers() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	now := w.Now()
	currentOpen, _, err := w.transferWindow(now)
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
