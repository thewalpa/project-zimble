package app

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// Balance sweeps run long seeded careers and log what they measure; the
// numbers go to docs/balance.md. They skip unless ZIMBLE_BALANCE=1:
//
//	ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalance -v -count=1
func requireBalanceSweep(t *testing.T) {
	t.Helper()
	if os.Getenv("ZIMBLE_BALANCE") != "1" {
		t.Skip("balance sweep: set ZIMBLE_BALANCE=1")
	}
}

// marketYear is what one year of an AI transfer market looks like, measured
// at the close of its window (the market) and at the next contract-year end
// (money and the free-agent pool after expiries and AI signings).
type marketYear struct {
	Bids, Completed, Rejected, Expired, Collapsed int
	Refused                                       int // accepted, but the player refused to join the buyer
	Short                                         int // AI players missing from the rosters at the close, summed over clubs
	ShortClubs                                    int // AI clubs with at least one missing
	MaxClubMoves                                  int // most completed transfers in and out of one AI club
	ToWeaker                                      int // completed transfers to a club with a lower squad average before the window
	StarsMoved                                    int // of the 16 best active players at the close, those who moved in the window
	Listed, Unsold                                int // AI listings in the window, and those whose player stayed
	FreeAgentsOpen, FreeAgentsClose               int // pool when the window opens and when it closes
	BestFreeAgentClose                            int // best overall in the pool at the close, 0 when empty
	AverageTop, AverageBottom, AverageMean        int // AI clubs' squad averages at the close
	BalanceMin, BalanceMedian, BalanceMax         money.Money
	Negative                                      int // AI clubs below zero at the contract-year end
	UserBids, UserBidsExpired                     int // bids for the manager's players, and those left unanswered
	UserAverage                                   int
	UserSigned, UserBought, UserSold, UserRenewed int // the manager's signings, purchases, sales and renewals in the year
	UserFailed                                    int // his commands the world refused
	UserBalance                                   money.Money
}

// marketRun is one seeded career.
type marketRun struct {
	Seed  uint64
	Years []marketYear
	// Per player: completed transfers, and the longest run of consecutive
	// windows he moved in.
	Moves, Streak map[ids.PlayerID]int
	// Per AI club: completed purchases, and net transfer spend.
	Buys  map[ids.ClubID]int
	Spend map[ids.ClubID]money.Money
	// Per league: titles per club.
	Titles map[string]map[ids.ClubID]int
	// AI clubs by squad average, strongest first, after the first and the
	// last window.
	FirstRank, LastRank []ids.ClubID
	Players             []int // active players after each year
}

// sweepMarket plays years of a career (AI-only when club is zero, else with
// a passive manager at club: he submits nothing, answers no bid and lists
// no one) and measures its transfer market.
func sweepMarket(t *testing.T, seed uint64, club ids.ClubID, years int, policy *managerPolicy) marketRun {
	t.Helper()
	var w *World
	if club == 0 {
		w = newWorld(t, seed)
	} else {
		w = userWorld(t, seed, club)
	}
	run := marketRun{
		Seed: seed, Moves: map[ids.PlayerID]int{}, Streak: map[ids.PlayerID]int{},
		Buys: map[ids.ClubID]int{}, Spend: map[ids.ClubID]money.Money{}, Titles: map[string]map[ids.ClubID]int{},
	}
	lastMoved := map[ids.PlayerID]int{}
	streak := map[ids.PlayerID]int{}
	offers, seen := 0, events.ID(0) // the last event read
	for year := 1; year <= years; year++ {
		var y marketYear
		y.FreeAgentsOpen = len(w.FreeAgents())
		before := map[ids.ClubID]int{}
		for _, row := range w.Summary().ClubRows {
			before[row.ID] = row.AverageOverall
		}
		// The journal keeps only the latest events, and a window of 32 clubs
		// can overflow it: read it after every day.
		listed := map[ids.PlayerID]ids.ClubID{}
		for closes := w.TransferWindow().Closes; w.Now() < closes; {
			policy.window(t, w, &y)
			mustContinue(t, w, min(closes, w.Now()+sim.GameInstant(sim.Day)))
			if len(w.journal) > 0 && w.journal[0].ID > seen+1 {
				t.Fatalf("seed %d year %d: the journal dropped events %d..%d before they were read", seed, year, seen+1, w.journal[0].ID-1)
			}
			for _, e := range w.journal {
				if e.ID > seen && e.Kind == events.KindPlayerListed && e.PlayerListed.Club != w.userClub {
					listed[e.PlayerListed.Player] = e.PlayerListed.Club
				}
			}
			seen = w.lastEvent
		}
		y.Listed = len(listed)

		clubMoves := map[ids.ClubID]int{}
		moved := map[ids.PlayerID]bool{}
		for _, o := range w.transfers.Offers()[offers:] {
			y.Bids++
			if o.Seller == w.userClub {
				y.UserBids++
				if o.Status == transfers.StatusExpired {
					y.UserBidsExpired++
				}
			}
			switch o.Status {
			case transfers.StatusCompleted:
				if club != 0 && o.Buyer == w.userClub {
					y.UserBought++
					continue
				}
				if club != 0 && o.Seller == w.userClub {
					y.UserSold++
					continue
				}
				y.Completed++
				moved[o.Player] = true
				if before[o.Buyer] < before[o.Seller] {
					y.ToWeaker++
				}
				clubMoves[o.Buyer]++
				clubMoves[o.Seller]++
				run.Buys[o.Buyer]++
				run.Moves[o.Player]++
				if lastMoved[o.Player] == year-1 {
					streak[o.Player]++
				} else {
					streak[o.Player] = 1
				}
				lastMoved[o.Player] = year
				run.Streak[o.Player] = max(run.Streak[o.Player], streak[o.Player])
			case transfers.StatusRejected:
				y.Rejected++
			case transfers.StatusExpired:
				y.Expired++
			case transfers.StatusCollapsed:
				y.Collapsed++
			case transfers.StatusRefused:
				y.Refused++
			}
		}
		offers = len(w.transfers.Offers())
		for id, c := range listed {
			if a, ok := w.employment.Assignment(id); ok && a.Club == c {
				y.Unsold++
			}
		}

		var active []SquadPlayer
		for _, id := range w.activePlayers() {
			active = append(active, w.squadPlayer(id))
		}
		slices.SortStableFunc(active, func(a, b SquadPlayer) int { return b.Overall - a.Overall })
		for _, p := range active[:16] {
			if moved[p.Player] {
				y.StarsMoved++
			}
		}
		for _, c := range w.registry.Clubs() {
			if c.ID == w.userClub {
				continue
			}
			team, _ := w.registry.SeniorTeam(c.ID)
			counts := w.squadCounts(team)
			missing := 0
			for _, q := range w.defs.Roster {
				missing += max(0, q.Count-counts[q.Position])
			}
			y.Short += missing
			if missing > 0 {
				y.ShortClubs++
			}
		}
		pool := w.FreeAgents()
		y.FreeAgentsClose = len(pool)
		for _, p := range pool {
			y.BestFreeAgentClose = max(y.BestFreeAgentClose, p.Overall)
		}
		var averages []int
		var rank []ClubSummary
		for _, row := range w.Summary().ClubRows {
			if c := row.ID; c != w.userClub {
				averages = append(averages, row.AverageOverall)
				rank = append(rank, row)
				y.MaxClubMoves = max(y.MaxClubMoves, clubMoves[c])
			} else {
				y.UserAverage = row.AverageOverall
			}
		}
		slices.Sort(averages)
		y.AverageBottom, y.AverageTop, y.AverageMean = averages[0], averages[len(averages)-1], mean(averages)
		slices.SortStableFunc(rank, func(a, b ClubSummary) int { return b.AverageOverall - a.AverageOverall })
		order := make([]ids.ClubID, len(rank))
		for i, r := range rank {
			order[i] = r.ID
		}
		if year == 1 {
			run.FirstRank = order
		}
		run.LastRank = order

		playSeason(t, w)
		policy.renew(t, w, &y)
		mustContinue(t, w, w.ContractYearEnd())
		seen = w.lastEvent // listings happen only inside a window

		var balances []money.Money
		for _, c := range w.registry.Clubs() {
			f, _ := w.Finances(c.ID)
			if c.ID == w.userClub {
				y.UserBalance = f.Balance
				continue
			}
			balances = append(balances, f.Balance)
			if f.Balance < 0 {
				y.Negative++
			}
		}
		slices.Sort(balances)
		y.BalanceMin, y.BalanceMedian, y.BalanceMax = balances[0], balances[len(balances)/2], balances[len(balances)-1]
		run.Players = append(run.Players, w.Summary().Players)
		run.Years = append(run.Years, y)
		if err := w.Validate(); err != nil {
			t.Fatalf("seed %d year %d: %v", seed, year, err)
		}
	}
	for _, e := range w.finance.All() {
		if e.Kind == finance.KindTransfer && e.Club != w.userClub {
			run.Spend[e.Club] -= e.Amount
		}
	}
	for _, rec := range w.History() {
		if rec.Champion == nil {
			continue
		}
		if run.Titles[rec.CompetitionName] == nil {
			run.Titles[rec.CompetitionName] = map[ids.ClubID]int{}
		}
		run.Titles[rec.CompetitionName][rec.Champion.Club]++
	}
	return run
}

func mean(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	total := 0
	for _, x := range xs {
		total += x
	}
	return (2*total + len(xs)) / (2 * len(xs))
}

// thousands renders money in thousands of whole units, rounded down.
func thousands(m money.Money) string {
	return fmt.Sprintf("%dk", int64(m)/money.MinorPerUnit/1000)
}

func sweepSeeds(t *testing.T, seeds []uint64, club ids.ClubID, years int, policy *managerPolicy) []marketRun {
	runs := make([]marketRun, len(seeds))
	t.Run("seeds", func(t *testing.T) {
		for i, s := range seeds {
			t.Run(fmt.Sprint(s), func(t *testing.T) {
				t.Parallel()
				runs[i] = sweepMarket(t, s, club, years, policy)
			})
		}
	})
	return runs
}

// reportMarket logs a sweep: per-year means over the seeds at a few
// checkpoints, then whole-career figures per seed.
func reportMarket(t *testing.T, runs []marketRun, user bool) {
	var b strings.Builder
	years := len(runs[0].Years)
	fmt.Fprintf(&b, "\n%d seeds, %d years, means over seeds\n", len(runs), years)
	fmt.Fprintln(&b, "year  bids done rej exp col refused short shortclubs maxclub weaker stars16 listed unsold  fa-open fa-close best-fa  avg-top avg-bot avg-mean  bal-min bal-med bal-max neg")
	all := make([]int, years)
	for i := range all {
		all[i] = i + 1
	}
	rows := [][]int{{1}, {2}, {3}, {5}, {10}, {15}, {20}, {25}, {30}, all}
	for _, row := range rows {
		if row[len(row)-1] > years {
			continue
		}
		col := func(f func(marketYear) int) int {
			var xs []int
			for _, r := range runs {
				for _, yr := range row {
					xs = append(xs, f(r.Years[yr-1]))
				}
			}
			return mean(xs)
		}
		nonzero := func(f func(marketYear) int) int { // mean over the runs where it is not zero
			var xs []int
			for _, r := range runs {
				for _, yr := range row {
					if x := f(r.Years[yr-1]); x != 0 {
						xs = append(xs, x)
					}
				}
			}
			return mean(xs)
		}
		mcol := func(f func(marketYear) money.Money) string {
			var xs []int
			for _, r := range runs {
				for _, yr := range row {
					xs = append(xs, int(int64(f(r.Years[yr-1]))/money.MinorPerUnit/1000))
				}
			}
			return fmt.Sprintf("%dk", mean(xs))
		}
		label := fmt.Sprint(row[0])
		if len(row) > 1 {
			label = "all"
		}
		fmt.Fprintf(&b, "%4s  %4d %4d %3d %3d %3d %7d %5d %9d %7d %6d %7d %6d %6d  %7d %8d %7d  %7d %7d %8d  %7s %7s %7s %3d\n", label,
			col(func(y marketYear) int { return y.Bids }), col(func(y marketYear) int { return y.Completed }),
			col(func(y marketYear) int { return y.Rejected }), col(func(y marketYear) int { return y.Expired }),
			col(func(y marketYear) int { return y.Collapsed }), col(func(y marketYear) int { return y.Refused }),
			col(func(y marketYear) int { return y.Short }), col(func(y marketYear) int { return y.ShortClubs }),
			col(func(y marketYear) int { return y.MaxClubMoves }),
			col(func(y marketYear) int { return y.ToWeaker }), col(func(y marketYear) int { return y.StarsMoved }),
			col(func(y marketYear) int { return y.Listed }), col(func(y marketYear) int { return y.Unsold }),
			col(func(y marketYear) int { return y.FreeAgentsOpen }), col(func(y marketYear) int { return y.FreeAgentsClose }),
			nonzero(func(y marketYear) int { return y.BestFreeAgentClose }),
			col(func(y marketYear) int { return y.AverageTop }), col(func(y marketYear) int { return y.AverageBottom }),
			col(func(y marketYear) int { return y.AverageMean }),
			mcol(func(y marketYear) money.Money { return y.BalanceMin }), mcol(func(y marketYear) money.Money { return y.BalanceMedian }),
			mcol(func(y marketYear) money.Money { return y.BalanceMax }), col(func(y marketYear) int { return y.Negative }))
	}
	if user {
		fmt.Fprintln(&b, "\nmanager's club, mean over seeds:  year  bids-for-his-players expired  signed bought sold renewed refused  squad-avg  balance")
		for _, yr := range []int{1, 2, 3, 5, 10, 20, 30} {
			if yr > years {
				continue
			}
			col := func(f func(marketYear) int) int {
				var xs []int
				for _, r := range runs {
					xs = append(xs, f(r.Years[yr-1]))
				}
				return mean(xs)
			}
			fmt.Fprintf(&b, "  %4d  %4d %4d  %4d %4d %4d %4d %4d  %4d  %4dk\n", yr,
				col(func(y marketYear) int { return y.UserBids }), col(func(y marketYear) int { return y.UserBidsExpired }),
				col(func(y marketYear) int { return y.UserSigned }), col(func(y marketYear) int { return y.UserBought }),
				col(func(y marketYear) int { return y.UserSold }), col(func(y marketYear) int { return y.UserRenewed }),
				col(func(y marketYear) int { return y.UserFailed }), col(func(y marketYear) int { return y.UserAverage }),
				col(func(y marketYear) int { return int(int64(y.UserBalance) / money.MinorPerUnit / 1000) }))
		}
		fmt.Fprintln(&b, "manager's club per seed: average over the career (squad-avg), titles (all competitions), balance at year 30")
		for _, r := range runs {
			var avgs []int
			for _, y := range r.Years {
				avgs = append(avgs, y.UserAverage)
			}
			titles := 0
			for _, byClub := range r.Titles {
				titles += byClub[userClub3]
			}
			fmt.Fprintf(&b, "  %4d  avg %d (%d-%d)  titles %d  balance %s\n", r.Seed, mean(avgs), slices.Min(avgs), slices.Max(avgs), titles,
				thousands(r.Years[len(r.Years)-1].UserBalance))
		}
	}
	windows, short, missing := 0, 0, 0
	for _, r := range runs {
		for _, y := range r.Years {
			windows++
			if y.Short > 0 {
				short++
				missing += y.Short
			}
		}
	}
	fmt.Fprintf(&b, "\nwindows closing with an AI club short of its roster: %d of %d (%d players missing in all)\n", short, windows, missing)
	fmt.Fprintln(&b, "\nper seed over the career:")
	fmt.Fprintln(&b, "seed  transfers/window(min-max) listed unsold%  movers moved>=3 max-moves streak>=2 max-streak  buys/club(min-max)  spend/club(min..max)  top4-kept  players(min-max)  titles")
	for _, r := range runs {
		lo, hi, total, listed, unsold := 1<<30, 0, 0, 0, 0
		for _, y := range r.Years {
			lo, hi, total = min(lo, y.Completed), max(hi, y.Completed), total+y.Completed
			listed, unsold = listed+y.Listed, unsold+y.Unsold
		}
		movers, three, maxMoves, streaks, maxStreak := len(r.Moves), 0, 0, 0, 0
		for id, n := range r.Moves {
			if n >= 3 {
				three++
			}
			maxMoves = max(maxMoves, n)
			if r.Streak[id] >= 2 {
				streaks++
			}
			maxStreak = max(maxStreak, r.Streak[id])
		}
		var buys []int
		var spends []money.Money
		for _, rank := range r.FirstRank {
			buys = append(buys, r.Buys[rank])
			spends = append(spends, r.Spend[rank])
		}
		kept := 0
		for _, c := range r.FirstRank[:4] {
			if slices.Contains(r.LastRank[:4], c) {
				kept++
			}
		}
		unsoldPct := 0
		if listed > 0 {
			unsoldPct = 100 * unsold / listed
		}
		var titles []string
		names := make([]string, 0, len(r.Titles))
		for name := range r.Titles {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			most := 0
			for _, n := range r.Titles[name] {
				most = max(most, n)
			}
			titles = append(titles, fmt.Sprintf("%s %d champions, most %d", name, len(r.Titles[name]), most))
		}
		fmt.Fprintf(&b, "%4d  %2d (%d-%d)  %4d %3d%%  %4d %4d %3d  %4d %3d  (%d-%d)  (%s..%s)  %d/4  (%d-%d)  %s\n",
			r.Seed, total/len(r.Years), lo, hi, listed, unsoldPct, movers, three, maxMoves, streaks, maxStreak,
			slices.Min(buys), slices.Max(buys), thousands(slices.Min(spends)), thousands(slices.Max(spends)), kept,
			slices.Min(r.Players), slices.Max(r.Players), strings.Join(titles, "; "))
	}
	t.Log(b.String())
}

var balanceSeeds = []uint64{1, 2, 3, 5, 7, 11, 13, 42, 99, 2026}

func TestBalanceAIMarketSweep(t *testing.T) {
	requireBalanceSweep(t)
	reportMarket(t, sweepSeeds(t, balanceSeeds, 0, 30, nil), false)
}

func TestBalanceAIMarketWithPassiveManager(t *testing.T) {
	requireBalanceSweep(t)
	reportMarket(t, sweepSeeds(t, []uint64{7, 42, 99, 2026}, userClub3, 30, nil), true)
}

func TestBalanceAIMarketWithRecruitingManager(t *testing.T) {
	requireBalanceSweep(t)
	reportMarket(t, sweepSeeds(t, []uint64{7, 42, 99, 2026}, userClub3, 30, &managerPolicy{}), true)
}

// managerPolicy is a scripted manager for the sweeps: it renews the players
// worth keeping, signs useful free agents, bids for listed upgrades, lists
// his surplus and accepts every bid for it. The nil policy is the passive
// manager, who does nothing. All of its commands may be refused (counted in
// UserFailed); it never stops the sweep.
type managerPolicy struct{}

func (p *managerPolicy) fail(y *marketYear, err error) bool {
	if err != nil {
		y.UserFailed++
	}
	return err == nil
}

// renew renews the expiring players who are at least the squad average, and
// those at a position that would otherwise fall below its roster count.
func (p *managerPolicy) renew(t *testing.T, w *World, y *marketYear) {
	if p == nil || w.userClub == 0 {
		return
	}
	end := w.ContractYearEnd()
	if end-sim.GameInstant(sim.Day) > w.Now() {
		mustContinue(t, w, end-sim.GameInstant(sim.Day))
	}
	squad, _ := w.Squad(w.userClub)
	counts := w.squadCounts(mustUserTeam(t, w))
	avg := squadAverage(w, w.userClub)
	for _, sp := range squad {
		if sp.Contract.Expires != end {
			continue
		}
		if sp.Overall < avg-2 && counts[sp.Position] > w.defs.Quota(sp.Position).Min {
			continue
		}
		offer, err := w.SuggestContract(sp.Player)
		if !p.fail(y, err) {
			continue
		}
		_, err = w.RenewContract(RenewContract{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: sp.Player, Offer: offer})
		if p.fail(y, err) {
			y.UserRenewed++
		}
	}
}

// window acts once a day while the transfer window is open.
func (p *managerPolicy) window(t *testing.T, w *World, y *marketYear) {
	if p == nil || w.userClub == 0 {
		return
	}
	tw := w.TransferWindow()
	if !tw.Open || w.Now()+sim.GameInstant(sim.Day) >= tw.BidsClose {
		return
	}
	team := mustUserTeam(t, w)
	// Accept every open bid for his players (the world refuses one that
	// would leave a position under its minimum).
	for _, o := range w.Offers() {
		if o.Seller == w.userClub && o.Status == transfers.StatusOpen {
			_, err := w.RespondToOffer(RespondToOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Offer: o.ID, Accept: true})
			p.fail(y, err)
		}
	}
	weakest := func(squad []SquadPlayer, pos players.Position) int {
		low := 101
		for _, sp := range squad {
			if sp.Position == pos {
				low = min(low, sp.Overall)
			}
		}
		return low
	}
	squad, _ := w.Squad(w.userClub)
	balance, _ := w.finance.Balance(w.userClub)
	// Sign the best useful free agent.
	if len(squad) < w.defs.SquadLimit && balance > money.Money(2_000_000*money.MinorPerUnit) {
		pool := w.FreeAgents()
		slices.SortStableFunc(pool, func(a, b SquadPlayer) int { return b.Overall - a.Overall })
		counts := w.squadCounts(team)
		for _, fa := range pool {
			if counts[fa.Position] >= w.defs.Quota(fa.Position).Min && fa.Overall < weakest(squad, fa.Position)+3 {
				continue
			}
			offer, err := w.SuggestContract(fa.Player)
			if !p.fail(y, err) {
				continue
			}
			if _, err = w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: fa.Player, Offer: offer}); p.fail(y, err) {
				y.UserSigned++
			}
			break
		}
	}
	// Bid for the best listed player who beats a position's weakest by 4,
	// for no more than a quarter of the balance. At most two bids a day,
	// and none while the squad plus his open bids would pass the limit.
	squad, _ = w.Squad(w.userClub)
	open := 0
	for _, o := range w.Offers() {
		if o.Buyer == w.userClub && o.Status == transfers.StatusOpen {
			open++
		}
	}
	listed := w.TransferList()
	slices.SortStableFunc(listed, func(a, b ListedPlayer) int { return b.Overall - a.Overall })
	bids := 0
	for _, lp := range listed {
		if bids == 2 || len(squad)+open+bids >= w.defs.SquadLimit {
			break
		}
		if lp.Club == w.userClub || !biddable(w, lp.Player) || lp.Overall < weakest(squad, lp.Position)+4 || lp.Value > balance/4 {
			continue
		}
		offer, err := w.SuggestContract(lp.Player)
		if !p.fail(y, err) {
			continue
		}
		if _, err = w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: lp.Player, Fee: lp.Value, Offer: offer}); p.fail(y, err) {
			bids++
		}
	}
	// List up to two surplus players (above a position's roster count),
	// the lowest rated first, when the squad is nearly full.
	if len(squad) >= w.defs.SquadLimit-2 {
		counts := w.squadCounts(team)
		slices.SortStableFunc(squad, func(a, b SquadPlayer) int { return a.Overall - b.Overall })
		listings := 0
		for _, sp := range squad {
			if listings == 2 {
				break
			}
			if sp.Listed || counts[sp.Position] <= w.defs.Quota(sp.Position).Count || !biddable(w, sp.Player) { // a player who moved in the window cannot be listed
				continue
			}
			if _, err := w.ListPlayer(ListPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: sp.Player, Asking: sp.Value}); p.fail(y, err) {
				listings++
				counts[sp.Position]--
			}
		}
	}
}
