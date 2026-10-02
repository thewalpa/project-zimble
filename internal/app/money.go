package app

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

// taskWages posts every club's weekly wage bill. It has no payload and
// reschedules itself a week later, so exactly one is always queued, due in
// (Now, Now+Week] on a whole week since the epoch.
const taskWages sim.TaskKind = 4

// contractExpiry anchors generated contract years to the calendar: a
// contract of N years ends at 00:00 on the first day of the epoch's month, N
// years after the epoch (2025-07-01 + 2 years = 2027-07-01). The end is
// exclusive.
func contractExpiry(cal sim.Calendar, years int) (sim.GameInstant, error) {
	e := cal.Epoch()
	return cal.Instant(sim.CivilTime{Year: e.Year + years, Month: e.Month, Day: 1})
}

// withContracts returns the generated assignments with their contract terms
// anchored to the calendar.
func withContracts(cal sim.Calendar, snap worldgen.Snapshot) ([]employment.Assignment, error) {
	terms := map[ids.PlayerID]worldgen.ContractTerms{}
	for _, c := range snap.Contracts {
		terms[c.Player] = c
	}
	out := make([]employment.Assignment, len(snap.Assignments))
	for i, a := range snap.Assignments {
		t, ok := terms[a.Player]
		if !ok {
			return nil, fmt.Errorf("app: player %d has no generated contract", a.Player)
		}
		expires, err := contractExpiry(cal, t.Years)
		if err != nil {
			return nil, fmt.Errorf("app: player %d contract: %w", a.Player, err)
		}
		a.Contract = employment.Contract{Expires: expires, WeeklyWage: t.WeeklyWage}
		out[i] = a
	}
	return out, nil
}

// openAccounts gives every club its opening balance at the career start.
func openAccounts(clubs []ids.ClubID, opening money.Money) (*finance.Store, error) {
	store, err := finance.New(finance.Snapshot{})
	if err != nil {
		return nil, err
	}
	var postings []finance.Posting
	for _, c := range clubs {
		postings = append(postings, finance.Posting{Club: c, Kind: finance.KindOpening, Amount: opening})
	}
	plan, err := store.Plan(0, postings)
	if err != nil {
		return nil, err
	}
	return store, store.Apply(plan)
}

func (w *World) scheduleWages(at sim.GameInstant) error {
	if _, err := w.scheduler.Schedule(sim.TaskSpec{DueAt: at, Phase: sim.PhasePreparation, StableOrder: 1, Kind: taskWages}); err != nil {
		return fmt.Errorf("app: schedule wages at %d: %w", at, err)
	}
	return nil
}

// payWages posts each club's weekly wage bill (clubs in ID order), queues
// next week's wages and emits one LedgerPosted event. It plans first, then
// schedules (which changes nothing when it fails), then applies.
func (w *World) payWages(at sim.GameInstant, cohort []sim.Task) error {
	if len(cohort) != 1 || cohort[0].PayloadID != 0 {
		return fmt.Errorf("app: wage cohort at %d has %d tasks, first payload %d", at, len(cohort), cohort[0].PayloadID)
	}
	var postings []finance.Posting
	for _, c := range w.registry.Clubs() {
		bill, err := w.employment.WageBill(c.ID)
		if err != nil {
			return fmt.Errorf("app: club %d wage bill: %w", c.ID, err)
		}
		if bill == 0 {
			continue
		}
		cost, err := bill.Neg()
		if err != nil {
			return err
		}
		postings = append(postings, finance.Posting{Club: c.ID, Kind: finance.KindWages, Amount: cost})
	}
	plan, err := w.finance.Plan(at, postings)
	if err != nil {
		return fmt.Errorf("app: wages at %d: %w", at, err)
	}
	if err := w.scheduleWages(at + sim.GameInstant(sim.Week)); err != nil {
		return err
	}
	w.applyFinance(plan)
	w.emitLedger(at, taskCause(cohort[0].ID), plan)
	return nil
}

// gatePostings pays the home club its receipts for each planned match.
func (w *World) gatePostings(plan []plannedMatch) ([]finance.Posting, error) {
	gate := w.defs.Economy.GatePerHomeMatch
	if gate == 0 {
		return nil, nil
	}
	var out []finance.Posting
	for _, p := range plan {
		t, ok := w.registry.Team(p.fixture.Home)
		if !ok {
			return nil, fmt.Errorf("app: fixture %d home team %d unknown", p.fixture.ID, p.fixture.Home)
		}
		out = append(out, finance.Posting{Club: t.Club, Kind: finance.KindGate, Amount: gate, Fixture: p.fixture.ID})
	}
	return out, nil
}

// prizePostings pays each entrant its pinned stage award, naming the tie
// where it went out (the final for both finalists). Nothing is paid until
// the whole edition has official results. Clubs are posted in ID order.
func (w *World) prizePostings(ref competitions.SeasonRef) ([]finance.Posting, error) {
	ci, ok := w.cupIndex(ref.Competition)
	if !ok {
		return nil, fmt.Errorf("app: %s is not a cup edition", ref)
	}
	exits, ok := w.competitions.Exits(ref)
	if !ok {
		return nil, fmt.Errorf("app: %s has no completed cup exits", ref)
	}
	prizes := w.cups[ci].Prizes
	fixtures := w.competitions.Fixtures(ref)
	var out []finance.Posting
	for _, exit := range exits {
		if exit.Stage >= len(prizes) || prizes[exit.Stage] == 0 {
			continue
		}
		team, ok := w.registry.Team(exit.Team)
		if !ok {
			return nil, fmt.Errorf("app: %s prize team %d unknown", ref, exit.Team)
		}
		var fixture ids.FixtureID
		for _, f := range fixtures {
			if f.Round == exit.Round && (f.Home == exit.Team || f.Away == exit.Team) {
				fixture = f.ID
				break
			}
		}
		if !fixture.Valid() {
			return nil, fmt.Errorf("app: %s prize team %d has no exit fixture", ref, exit.Team)
		}
		out = append(out, finance.Posting{Club: team.Club, Kind: finance.KindPrize, Amount: prizes[exit.Stage], Fixture: fixture})
	}
	slices.SortFunc(out, func(a, b finance.Posting) int { return cmp.Compare(a.Club, b.Club) })
	return out, nil
}

// applyFinance commits a finance plan made in the same operation; failure is
// a broken invariant.
func (w *World) applyFinance(plan finance.Plan) {
	if err := w.finance.Apply(plan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
}

// emitLedger stages a LedgerPosted event for an applied plan.
func (w *World) emitLedger(at sim.GameInstant, cause events.Cause, plan finance.Plan) {
	w.emitLedgerEntries(at, cause, plan.Entries())
}

// emitLedgerEntries stages a LedgerPosted event for applied entries.
func (w *World) emitLedgerEntries(at sim.GameInstant, cause events.Cause, entries []finance.Entry) {
	if len(entries) == 0 {
		return
	}
	p := &events.LedgerPosted{}
	for _, e := range entries {
		p.Entries = append(p.Entries, events.LedgerEntry{
			Entry: uint64(e.ID), Club: e.Club, Kind: uint8(e.Kind), Amount: e.Amount,
			Balance: w.balanceAfter(e), Fixture: e.Fixture, Offer: e.Offer, Player: e.Player,
		})
	}
	w.emit(at, cause, events.Event{Kind: events.KindLedgerPosted, LedgerPosted: p})
}

// balanceAfter is a club's balance right after one of its entries.
func (w *World) balanceAfter(entry finance.Entry) money.Money {
	var b money.Money
	for _, e := range w.finance.Entries(entry.Club) {
		if e.ID > entry.ID {
			break
		}
		b, _ = b.Add(e.Amount) // balances were overflow-checked when posted
	}
	return b
}

// validateFinance checks the ledgers against the world:
//
//   - every registered club, and only those, has an account opened at the
//     career start with the content's opening balance;
//   - no entry is after now; wage entries fall on whole weeks, at most one
//     per club and week;
//   - gate receipts pay exactly the content's amount, once, to the home club
//     of a fixture with an official result, when that result was recorded,
//     and every official result has been paid for;
//   - ended cup editions have exactly their pinned stage prizes, posted
//     at the final kickoff and naming each paid entrant's exit fixture;
//     editions whose season-end task is still queued have no prizes;
//   - exactly one wage task is queued, due in (Now, Now+Week] on a whole
//     week, with no payload.
func (w *World) validateFinance() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	var clubs []ids.ClubID
	for _, c := range w.registry.Clubs() {
		clubs = append(clubs, c.ID)
	}
	if !slices.Equal(w.finance.Accounts(), clubs) {
		fail("finance accounts %v, clubs %v", w.finance.Accounts(), clubs)
	}
	type prizeKey struct {
		club    ids.ClubID
		fixture ids.FixtureID
	}
	wantPrizes := map[prizeKey]finance.Entry{}
	var prizeOrder []prizeKey
	ending := map[competitions.SeasonRef]bool{}
	for _, ref := range w.seasonEnds {
		ending[ref] = true
	}
	for _, ref := range w.competitions.Seasons() {
		if _, cup := w.cupIndex(ref.Competition); !cup || ending[ref] || !w.competitions.SeasonCompleted(ref) {
			continue
		}
		postings, err := w.prizePostings(ref)
		if err != nil {
			fail("cup prizes: %v", err)
			continue
		}
		rounds := w.competitions.Rounds(ref)
		at := rounds[len(rounds)-1].Kickoff
		for _, p := range postings {
			key := prizeKey{p.Club, p.Fixture}
			wantPrizes[key] = finance.Entry{Club: p.Club, Fixture: p.Fixture, Amount: p.Amount, At: at}
			prizeOrder = append(prizeOrder, key)
		}
	}
	prizesPaid := map[prizeKey]bool{}
	week := sim.GameInstant(sim.Week)
	wagesPaid := map[[2]int64]bool{}
	paid := map[ids.FixtureID]bool{}
	for _, e := range w.finance.All() {
		if e.At > w.Now() {
			fail("ledger entry %d is in the future", e.ID)
		}
		switch e.Kind {
		case finance.KindOpening:
			if e.At != 0 || e.Amount != w.defs.Economy.OpeningBalance {
				fail("opening entry %d: %s at %d", e.ID, e.Amount, e.At)
			}
		case finance.KindWages:
			key := [2]int64{int64(e.Club), int64(e.At)}
			if e.At <= 0 || e.At%week != 0 || wagesPaid[key] {
				fail("wage entry %d at %d is off the weekly schedule or repeated", e.ID, e.At)
			}
			wagesPaid[key] = true
		case finance.KindPrize:
			key := prizeKey{e.Club, e.Fixture}
			want, ok := wantPrizes[key]
			if !ok || prizesPaid[key] || e.Amount != want.Amount || e.At != want.At {
				fail("prize entry %d for club %d fixture %d does not match one ended cup award", e.ID, e.Club, e.Fixture)
			}
			prizesPaid[key] = true
		case finance.KindGate:
			r, ok := w.competitions.Result(e.Fixture)
			home, _ := w.registry.Team(r.Home)
			if !ok || paid[e.Fixture] || home.Club != e.Club || e.Amount != w.defs.Economy.GatePerHomeMatch || e.At != r.RecordedAt {
				fail("gate entry %d for fixture %d does not match one official home result", e.ID, e.Fixture)
			}
			paid[e.Fixture] = true
		}
	}
	if w.defs.Economy.GatePerHomeMatch > 0 {
		for _, ref := range w.competitions.Seasons() {
			for _, r := range w.competitions.Results(ref) {
				if !paid[r.Fixture] {
					fail("fixture %d has an official result but no gate receipts", r.Fixture)
				}
			}
		}
	}

	// Check in canonical season/posting order, never map order.
	for _, key := range prizeOrder {
		if !prizesPaid[key] {
			fail("club %d has no cup prize for fixture %d", key.club, key.fixture)
		}
	}

	wageTasks := 0
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskWages {
			continue
		}
		wageTasks++
		if t.PayloadID != 0 || t.Phase != sim.PhasePreparation || t.DueAt <= w.Now() || t.DueAt > w.Now()+week || t.DueAt%week != 0 {
			fail("wage task %d due %d phase %d payload %d, now %d", t.ID, t.DueAt, t.Phase, t.PayloadID, w.Now())
		}
	}
	if wageTasks != 1 {
		fail("%d wage tasks queued, want 1", wageTasks)
	}
	return errs
}

// checkLedgerEvent compares a LedgerPosted event with the ledger.
func (w *World) checkLedgerEvent(p *events.LedgerPosted) error {
	for _, le := range p.Entries {
		e, ok := w.finance.Entry(finance.EntryID(le.Entry))
		if !ok || e.Club != le.Club || uint8(e.Kind) != le.Kind || e.Amount != le.Amount || e.Fixture != le.Fixture || e.Offer != le.Offer ||
			e.Player != le.Player || w.balanceAfter(e) != le.Balance {
			return fmt.Errorf("ledger entry %d differs from the ledger", le.Entry)
		}
	}
	return nil
}

// ClubFinances is a derived view of one club's money.
type ClubFinances struct {
	Club       ids.ClubID
	Balance    money.Money
	WeeklyWage money.Money     // current weekly wage bill
	Entries    []finance.Entry // oldest first
}

// Finances returns a club's balance, wage bill and ledger. Read-only.
func (w *World) Finances(club ids.ClubID) (ClubFinances, bool) {
	balance, ok := w.finance.Balance(club)
	if !ok {
		return ClubFinances{}, false
	}
	bill, err := w.employment.WageBill(club)
	if err != nil {
		return ClubFinances{}, false
	}
	return ClubFinances{Club: club, Balance: balance, WeeklyWage: bill, Entries: w.finance.Entries(club)}, true
}
