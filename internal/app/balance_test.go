package app

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
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
	Refused                                       int      // accepted, but the player refused to join the buyer
	Short                                         int      // AI players missing from the rosters at the close, summed over clubs
	ShortClubs                                    int      // AI clubs with at least one missing
	ShortDetail                                   []string // "club N: POS-k ..." for each AI club short at the close
	MaxClubMoves                                  int      // most completed transfers in and out of one AI club
	ToWeaker                                      int      // completed transfers to a club with a lower squad average before the window
	StarsMoved                                    int      // of the 16 best active players at the close, those who moved in the window
	Listed, Unsold                                int      // AI listings in the window, and those whose player stayed
	FreeAgentsOpen, FreeAgentsClose               int      // pool when the window opens and when it closes
	BestFreeAgentClose                            int      // best overall in the pool at the close, 0 when empty
	AverageTop, AverageBottom, AverageMean        int      // AI clubs' squad averages at the close
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
			detail := ""
			for _, q := range w.defs.Roster {
				if m := max(0, q.Count-counts[q.Position]); m > 0 {
					missing += m
					detail += fmt.Sprintf(" %v-%d", q.Position, m)
				}
			}
			y.Short += missing
			if missing > 0 {
				y.ShortClubs++
				y.ShortDetail = append(y.ShortDetail, fmt.Sprintf("club %d:%s", c.ID, detail))
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
	var shortLines []string
	for _, r := range runs {
		for i, y := range r.Years {
			windows++
			if y.Short > 0 {
				short++
				missing += y.Short
				shortLines = append(shortLines, fmt.Sprintf("  seed %d year %d: free agents at the close %d; %s", r.Seed, i+1, y.FreeAgentsClose, strings.Join(y.ShortDetail, "; ")))
			}
		}
	}
	fmt.Fprintf(&b, "\nwindows closing with an AI club short of its roster: %d of %d (%d players missing in all)\n", short, windows, missing)
	for _, l := range shortLines {
		fmt.Fprintln(&b, l)
	}
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

// ---- Career seasons on every match engine ----

// sideStatsSum accumulates one side's match statistics over league matches
// (the tick package's balance_test.go profiles the same fields on synthetic
// teams).
type sideStatsSum struct {
	shots, onTarget, passes, completed, tackles, saves, offsides int
	possession                                                   int // permille summed over the matches
}

func (s *sideStatsSum) add(t matches.TeamStats) {
	s.shots += int(t.Shots)
	s.onTarget += int(t.ShotsOnTarget)
	s.passes += int(t.Passes)
	s.completed += int(t.PassesCompleted)
	s.tackles += int(t.Tackles)
	s.saves += int(t.Saves)
	s.offsides += int(t.Offsides)
	s.possession += int(t.PossessionPermille)
}

// careerSeason is one season of one seeded career on one engine: its league
// matches, final tables, cup shootouts and workload. Clubs are ranked by
// their squad average before the season for the upset and gap columns.
type careerSeason struct {
	Matches, Home, Draw, Away     int
	Goals                         [2]int
	Upsets                        int    // league matches won by the club with the lower pre-season average
	GapMatches, GapStrongWins     [4]int // by |average gap|: 0-1, 2-4, 5-8, 9+
	ChampPts, LastPts             []int  // per league, from its final table
	CupMatches, CupShootouts      int
	TieMatches, TieShootouts      int // promotion play-off matches and those level after 90 minutes
	Injuries, Recovered           int
	DaysLost                      int
	PlayerDays                    map[ids.PlayerID]int
	ClubInjuries                  map[ids.ClubID]int
	ClubDays                      map[ids.ClubID]int
	ShortOfFit                    int // club-batches whose fit players cannot field a legal lineup
	Emergencies                   int // starter appearances of players recorded injured before the batch
	CondSum, CondSamples          int // mean condition of active players before each batch
	StartSum, Starts              int // condition of each starter before his match, and the starts
	StartsBelow90, StartsBelow100 int
	Stats                         [2]sideStatsSum
	StatsN                        int // league matches whose statistics are available

	injuredBefore map[ids.PlayerID]bool
	condBefore    map[ids.PlayerID]int
	leagueRefs    []competitions.SeasonRef // in first-seen order
}

// careerRun is one seeded career on one engine.
type careerRun struct {
	Seed    uint64
	Engine  string
	Seasons []careerSeason
}

// aiEngineWorld starts an AI-only career (no user club) that plays every
// fixture on the named engine. engineWorld in resolve_test.go is the same
// with a managed club.
func aiEngineWorld(t *testing.T, seed uint64, engine string) *World {
	t.Helper()
	cfg := DefaultConfig(random.Seed(seed))
	cfg.Engine = engine
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// gapBucket is the |average gap| bucket of a match: 0-1, 2-4, 5-8 or 9+.
func gapBucket(gap int) int {
	switch {
	case gap <= 1:
		return 0
	case gap <= 4:
		return 1
	case gap <= 8:
		return 2
	}
	return 3
}

// matchPhase names a round's competition by its format: "league", "playoff"
// (promotion ties) or "cup". Since ScheduleVersion 4 a cup edition is played
// between the league matchdays of the next season, so a batch's phase is its
// competition's, not the sweep's stage.
func matchPhase(w *World, ref competitions.SeasonRef) string {
	f, _ := w.competitions.Format(ref)
	switch f {
	case competitions.FormatKnockout:
		return "cup"
	case competitions.FormatTies:
		return "playoff"
	}
	return "league"
}

// playCareerPhase plays the rounds that come due before target (a target
// already passed plays nothing) and measures each batch; stage names the
// sweep's stage for failures. Continue resolves a
// batch without a user fixture on the way, so the sweep stops the clock one
// minute before each kickoff to sample the squads, then continues to the
// kickoff and folds the batch in, whether Continue resolved it or stopped.
func playCareerPhase(t *testing.T, w *World, y *careerSeason, seen *events.ID, target func(*World) sim.GameInstant, phase string, avg map[ids.ClubID]int) {
	t.Helper()
	for {
		end := max(target(w), w.Now())
		kickoff, ok := nextKickoff(w)
		if !ok || kickoff > end {
			if res := mustContinue(t, w, end); len(asRounds(res)) > 0 {
				t.Fatalf("seed %d %s: Continue to %d resolved a batch with no kickoff scheduled", w.seed, phase, end)
			}
			return
		}
		if kickoff-1 > w.Now() {
			if res := mustContinue(t, w, kickoff-1); len(asRounds(res)) > 0 {
				t.Fatalf("seed %d %s: a batch was resolved before the next kickoff %d", w.seed, phase, kickoff)
			}
		}
		y.sampleBefore(w)
		res := mustContinue(t, w, kickoff)
		batches := asRounds(res)
		if ready, ok := res.(FixtureRoundReady); ok {
			resolved, err := w.ResolveRounds(commandFor(ready, w.NextCommandID()))
			if err != nil {
				t.Fatalf("seed %d %s: resolve at %d: %v", w.seed, phase, ready.At, err)
			}
			batches = append(batches, resolved)
		}
		if len(batches) != 1 {
			t.Fatalf("seed %d %s: %d batches at kickoff %d, want 1", w.seed, phase, len(batches), kickoff)
		}
		if len(w.journal) > 0 && w.journal[0].ID > *seen+1 {
			t.Fatalf("seed %d %s: the journal dropped events %d..%d before they were read", w.seed, phase, *seen+1, w.journal[0].ID-1)
		}
		y.addBatch(w, batches[0], avg, seen)
	}
}

// sampleBefore records the workload state just before a batch is played:
// who is injured (so a starter can be counted as an emergency), the mean
// condition of every active player, and the clubs too short of fit players
// to field a legal lineup without their injured.
func (y *careerSeason) sampleBefore(w *World) {
	y.injuredBefore = map[ids.PlayerID]bool{}
	y.condBefore = map[ids.PlayerID]int{}
	cond, n := 0, 0
	for _, id := range w.activePlayers() {
		if c, ok := w.medical.Condition(id); ok {
			cond += int(c)
			n++
			y.condBefore[id] = int(c)
		}
		if d, ok := w.medical.DaysOut(id); ok && d > 0 {
			y.injuredBefore[id] = true
		}
	}
	if n > 0 {
		y.CondSum += (cond + n/2) / n
		y.CondSamples++
	}
	for _, c := range w.registry.Clubs() {
		team, ok := w.registry.SeniorTeam(c.ID)
		if !ok {
			continue
		}
		fit := make([]ids.PlayerID, 0)
		for _, id := range w.employment.Squad(team) {
			if !y.injuredBefore[id] {
				fit = append(fit, id)
			}
		}
		if !w.canField(fit) {
			y.ShortOfFit++
		}
	}
}

// addBatch folds one resolved batch into the season: league results and
// statistics, cup shootouts, emergency appearances and the injury events
// since the last batch.
func (y *careerSeason) addBatch(w *World, resolved RoundsResolved, avg map[ids.ClubID]int, seen *events.ID) {
	for _, m := range resolved.Matches {
		level := m.Score[0] == m.Score[1]
		switch matchPhase(w, m.Round.Season) {
		case "league":
			y.Matches++
			y.Goals[0] += int(m.Score[0])
			y.Goals[1] += int(m.Score[1])
			switch {
			case m.Score[0] > m.Score[1]:
				y.Home++
			case m.Score[1] > m.Score[0]:
				y.Away++
			default:
				y.Draw++
			}
			if ah, aa := avg[m.Home.Club], avg[m.Away.Club]; ah != aa {
				gap := ah - aa
				if gap < 0 {
					gap = -gap
				}
				b := gapBucket(gap)
				y.GapMatches[b]++
				strongerHome, wonHome, wonAway := ah > aa, m.Score[0] > m.Score[1], m.Score[1] > m.Score[0]
				if (strongerHome && wonHome) || (!strongerHome && wonAway) {
					y.GapStrongWins[b]++
				} else if wonHome || wonAway {
					y.Upsets++
				}
			}
			if m.Stats.Available {
				y.StatsN++
				y.Stats[0].add(m.Stats.Teams[0])
				y.Stats[1].add(m.Stats.Teams[1])
			}
		case "cup":
			y.CupMatches++
			if level {
				y.CupShootouts++
			}
		case "playoff":
			y.TieMatches++
			if level {
				y.TieShootouts++
			}
		}
		for side := 0; side < 2; side++ {
			for _, slot := range m.Lineups[side].Starters {
				if y.injuredBefore[slot.Player] {
					y.Emergencies++
				}
				if c, ok := y.condBefore[slot.Player]; ok {
					y.StartSum += c
					y.Starts++
					if c < 90 {
						y.StartsBelow90++
					}
					if c < 100 {
						y.StartsBelow100++
					}
				}
			}
		}
	}
	for _, e := range w.Events() {
		if e.ID <= *seen {
			continue
		}
		if e.Kind == events.KindPlayerInjured && e.PlayerInjured != nil {
			p := e.PlayerInjured
			y.Injuries++
			y.DaysLost += int(p.Days)
			y.PlayerDays[p.Player] += int(p.Days)
			y.ClubInjuries[p.Club]++
			y.ClubDays[p.Club] += int(p.Days)
		}
		if e.Kind == events.KindPlayerRecovered {
			y.Recovered++
		}
	}
	*seen = w.lastEvent
	for _, r := range resolved.Rounds {
		if matchPhase(w, r.Season) == "league" {
			if !slices.Contains(y.leagueRefs, r.Season) {
				y.leagueRefs = append(y.leagueRefs, r.Season)
			}
		}
	}
}

// sweepCareer plays a seeded career for seasons on one engine and measures
// each season. The whole career is one world: promotions, cups and the
// market all carry over.
func sweepCareer(t *testing.T, seed uint64, engine string, seasons int) careerRun {
	t.Helper()
	w := aiEngineWorld(t, seed, engine)
	run := careerRun{Seed: seed, Engine: engine}
	seen := events.ID(0)
	for s := 1; s <= seasons; s++ {
		y := careerSeason{
			PlayerDays:   map[ids.PlayerID]int{},
			ClubInjuries: map[ids.ClubID]int{},
			ClubDays:     map[ids.ClubID]int{},
		}
		avg := map[ids.ClubID]int{}
		for _, row := range w.Summary().ClubRows {
			avg[row.ID] = row.AverageOverall
		}
		playCareerPhase(t, w, &y, &seen, seasonEnd, "league", avg)
		for _, ref := range y.leagueRefs {
			table, ok := w.Table(ref)
			if !ok || len(table.Rows) == 0 {
				t.Fatalf("seed %d %s season %d: no final table for %+v", seed, engine, s, ref)
			}
			y.ChampPts = append(y.ChampPts, table.Rows[0].Points)
			y.LastPts = append(y.LastPts, table.Rows[len(table.Rows)-1].Points)
		}
		playCareerPhase(t, w, &y, &seen, playoffEnd, "playoff", avg)
		playCareerPhase(t, w, &y, &seen, cupsUnderwayEnd, "cup", avg)
		// An empty sample means the sweep stopped seeing batches, not a quiet
		// season.
		// Season 1 has no cup: edition N is drawn from league season N and played
		// in season N+1.
		if y.Matches == 0 || (s > 1 && y.CupMatches == 0) || y.CondSamples == 0 {
			t.Fatalf("seed %d %s season %d: measured %d league and %d cup matches, %d samples", seed, engine, s, y.Matches, y.CupMatches, y.CondSamples)
		}
		run.Seasons = append(run.Seasons, y)
		if err := w.Validate(); err != nil {
			t.Fatalf("seed %d %s season %d: %v", seed, engine, s, err)
		}
	}
	return run
}

// TestBalanceCareerEngines plays the same seeded careers on every engine and
// logs a season profile for docs/balance.md.
//
//	ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1
func TestBalanceCareerEngines(t *testing.T) {
	requireBalanceSweep(t)
	const seasons = 3
	seeds := []uint64{7, 42, 2026}
	engines := Engines()
	runs := make([][]careerRun, len(engines))
	for i := range runs {
		runs[i] = make([]careerRun, len(seeds))
	}
	t.Run("runs", func(t *testing.T) {
		for ei, engine := range engines {
			for si, seed := range seeds {
				t.Run(fmt.Sprintf("%s-%d", engine, seed), func(t *testing.T) {
					t.Parallel()
					runs[ei][si] = sweepCareer(t, seed, engine, seasons)
				})
			}
		}
	})
	// The engine changes how a match is played, never who plays whom.
	for si, seed := range seeds {
		for s := range runs[0][si].Seasons {
			for ei := 1; ei < len(engines); ei++ {
				a, b := runs[0][si].Seasons[s], runs[ei][si].Seasons[s]
				if a.Matches != b.Matches || !slices.Equal(a.leagueRefs, b.leagueRefs) {
					t.Fatalf("seed %d season %d: %s played %d league matches, %s %d", seed, s+1, engines[0], a.Matches, engines[ei], b.Matches)
				}
			}
		}
	}
	t.Log(reportCareers(engines, runs, seasons))
}

// reportCareers formats the per-engine means over every seed and season as
// markdown rows for docs/balance.md.
func reportCareers(engines []string, runs [][]careerRun, seasons int) string {
	var b strings.Builder
	n := len(runs[0]) * seasons
	fmt.Fprintf(&b, "\n%d seeds x %d seasons per engine\n", len(runs[0]), seasons)
	b.WriteString("\nLeague matches:\n\n| Engine | per season | Goals | Home–away goals | Home % | Draw % | Away % | Upsets % | Stronger side win % by gap 0-1 / 2-4 / 5-8 / 9+ |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for ei, engine := range engines {
		var m, home, draw, away, upsets, goals [2]int
		var gapM, gapW [4]int
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				m[0] += y.Matches
				home[0] += y.Home
				draw[0] += y.Draw
				away[0] += y.Away
				upsets[0] += y.Upsets
				goals[0] += y.Goals[0]
				goals[1] += y.Goals[1]
				for i := range gapM {
					gapM[i] += y.GapMatches[i]
					gapW[i] += y.GapStrongWins[i]
				}
			}
		}
		total := float64(m[0])
		fmt.Fprintf(&b, "| %s | %.0f | %.2f | %.2f–%.2f | %.1f | %.1f | %.1f | %.1f | %.0f / %.0f / %.0f / %.0f |\n",
			engine, total/float64(n), float64(goals[0]+goals[1])/total, float64(goals[0])/total, float64(goals[1])/total,
			100*float64(home[0])/total, 100*float64(draw[0])/total, 100*float64(away[0])/total,
			100*float64(upsets[0])/total,
			pctMean(gapW[0], gapM[0]), pctMean(gapW[1], gapM[1]), pctMean(gapW[2], gapM[2]), pctMean(gapW[3], gapM[3]))
		fmt.Fprintf(&b, "| | | | | matches by gap: | %d | %d | %d | %d |\n", gapM[0], gapM[1], gapM[2], gapM[3])
	}
	b.WriteString("\nFinal tables (mean per league-season):\n\n| Engine | Champion points | Last points | Spread |\n| --- | --- | --- | --- |\n")
	for ei, engine := range engines {
		var champ, last, leagues int
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				for i, p := range y.ChampPts {
					champ += p
					last += y.LastPts[i]
					leagues++
				}
			}
		}
		c, l := float64(champ)/float64(leagues), float64(last)/float64(leagues)
		fmt.Fprintf(&b, "| %s | %.1f | %.1f | %.1f |\n", engine, c, l, c-l)
	}
	b.WriteString("\nShootouts (level after 90 minutes) and workload (mean per season):\n\n| Engine | Cup matches (per season with a cup) | Cup shootouts % | Play-off ties | Ties to pens % | Injuries | per club | Days lost | per injury | Short-of-fit club-batches | Emergency starts | Condition before rounds |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for ei, engine := range engines {
		var cup, cupPens, tie, tiePens, inj, days, short, emerg, cond, samples, cupSeasons int
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				cup += y.CupMatches
				if y.CupMatches > 0 {
					cupSeasons++ // season 1 has no cup edition
				}
				cupPens += y.CupShootouts
				tie += y.TieMatches
				tiePens += y.TieShootouts
				inj += y.Injuries
				days += y.DaysLost
				short += y.ShortOfFit
				emerg += y.Emergencies
				cond += y.CondSum
				samples += y.CondSamples
			}
		}
		f := float64(n)
		fmt.Fprintf(&b, "| %s | %.1f | %.0f | %.1f | %.0f | %.1f | %.2f | %.0f | %.1f | %.1f | %.1f | %.1f |\n",
			engine, float64(cup)/float64(max(cupSeasons, 1)), pctMean(cupPens, cup), float64(tie)/f, pctMean(tiePens, tie),
			float64(inj)/f, float64(inj)/f/32, float64(days)/f, float64(days)/float64(max(inj, 1)),
			float64(short)/f, float64(emerg)/f, float64(cond)/float64(max(samples, 1)))
	}
	// The starters' condition before their matches (the all-player mean above
	// includes the bench and free agents, who sit at full condition).
	for ei, engine := range engines {
		var sum, starts, b90, b100 int
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				sum += y.StartSum
				starts += y.Starts
				b90 += y.StartsBelow90
				b100 += y.StartsBelow100
			}
		}
		fmt.Fprintf(&b, "\n%s starters' condition at kickoff: mean %.1f over %d starts, %.0f%% below 100, %.0f%% below 90\n",
			engine, float64(sum)/float64(max(starts, 1)), starts, pctMean(b100, starts), pctMean(b90, starts))
	}
	// Days lost per injured player and per club, from the per-season maps.
	for ei, engine := range engines {
		player, playerMax, clubDays, clubInj, players, clubs := 0, 0, 0, 0, 0, 0
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				var pids []ids.PlayerID
				for id := range y.PlayerDays {
					pids = append(pids, id)
				}
				slices.Sort(pids)
				for _, id := range pids {
					player += y.PlayerDays[id]
					playerMax = max(playerMax, y.PlayerDays[id])
					players++
				}
				var cids []ids.ClubID
				for id := range y.ClubDays {
					cids = append(cids, id)
				}
				slices.Sort(cids)
				for _, id := range cids {
					clubDays += y.ClubDays[id]
					clubInj += y.ClubInjuries[id]
					clubs++
				}
			}
		}
		fmt.Fprintf(&b, "\n%s days lost: mean per injured player %.1f (max %d over one season), mean per injured club-season %.1f (%.2f injuries)\n",
			engine, float64(player)/float64(max(players, 1)), playerMax,
			float64(clubDays)/float64(max(clubs, 1)), float64(clubInj)/float64(max(clubs, 1)))
	}
	// The league match statistics of engines that report them.
	for ei, engine := range engines {
		var s [2]sideStatsSum
		statsN := 0
		for _, r := range runs[ei] {
			for _, y := range r.Seasons {
				s[0].shots += y.Stats[0].shots
				s[0].onTarget += y.Stats[0].onTarget
				s[0].passes += y.Stats[0].passes
				s[0].completed += y.Stats[0].completed
				s[0].tackles += y.Stats[0].tackles
				s[0].saves += y.Stats[0].saves
				s[0].offsides += y.Stats[0].offsides
				s[0].possession += y.Stats[0].possession
				s[1].shots += y.Stats[1].shots
				s[1].onTarget += y.Stats[1].onTarget
				s[1].passes += y.Stats[1].passes
				s[1].completed += y.Stats[1].completed
				s[1].tackles += y.Stats[1].tackles
				s[1].saves += y.Stats[1].saves
				s[1].offsides += y.Stats[1].offsides
				s[1].possession += y.Stats[1].possession
				statsN += y.StatsN
			}
		}
		if statsN == 0 {
			fmt.Fprintf(&b, "\n%s reports no match statistics (no DetailedStats capability).\n", engine)
			continue
		}
		fmt.Fprintf(&b, "\n%s league match statistics (%d matches):\n\n| Side | Shots | On target | Saves | Passes | Completion %% | Tackles | Offsides | Possession %% |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", engine, statsN)
		for side, name := range []string{"home", "away"} {
			v := s[side]
			f := float64(statsN)
			fmt.Fprintf(&b, "| %s | %.1f | %.1f | %.1f | %.0f | %.0f | %.1f | %.1f | %.1f |\n",
				name, float64(v.shots)/f, float64(v.onTarget)/f, float64(v.saves)/f,
				float64(v.passes)/f, 100*float64(v.completed)/float64(max(v.passes, 1)),
				float64(v.tackles)/f, float64(v.offsides)/f, float64(v.possession)/f/10)
		}
	}
	return b.String()
}

// pctMean is k as a percentage of n, or zero when n is.
func pctMean(k, n int) float64 {
	if n == 0 {
		return 0
	}
	return 100 * float64(k) / float64(n)
}

// popYear is the population and the money of an AI-only world at one
// contract-year end (year 0: as generated).
type popYear struct {
	Active, FreeAgents       int
	Retired, Youth           int   // retirements and youth intake in the year
	RetireAges               []int // ages of the year's retirees at the contract-year end
	SquadMin, SquadMax       int
	Ages, Overalls           []int // every active player, sorted
	Division                 map[ids.ClubID]int
	ClubAverage              map[ids.ClubID]int
	Balance                  map[ids.ClubID]money.Money
	Gate, Wages, Net, Payoff map[ids.ClubID]money.Money // the year's postings per club (Net: transfer fees)
	Other                    map[ids.ClubID]money.Money // any other posting (cup prizes, once they are paid)
}

// popRun is one seeded AI-only career: a popYear per year, with every active
// player kept at the attribute checkpoints.
type popRun struct {
	Seed      uint64
	Years     []popYear
	Snapshots map[int][]SquadPlayer
}

var popCheckpoints = []int{0, 10, 20, 30}

// sweepPopulation plays an AI-only career for years and measures, at each
// contract-year end, its players and its clubs' money. A club's division is
// the one it plays in the coming season (promotion moves clubs).
func sweepPopulation(t *testing.T, seed uint64, years int) popRun {
	t.Helper()
	w := newWorld(t, seed)
	run := popRun{Seed: seed, Snapshots: map[int][]SquadPlayer{}}
	upper := map[ids.CompetitionID]bool{}
	for _, p := range w.Promotions() {
		upper[p.Upper] = true
	}
	measure := func(year int, from sim.GameInstant, active map[ids.PlayerID]bool) popYear {
		s := w.Summary()
		y := popYear{
			Active: s.Players, FreeAgents: s.FreeAgents, SquadMin: 1 << 30,
			Division: map[ids.ClubID]int{}, ClubAverage: map[ids.ClubID]int{}, Balance: map[ids.ClubID]money.Money{},
			Gate: map[ids.ClubID]money.Money{}, Wages: map[ids.ClubID]money.Money{}, Net: map[ids.ClubID]money.Money{}, Payoff: map[ids.ClubID]money.Money{},
			Other: map[ids.ClubID]money.Money{},
		}
		for _, row := range s.ClubRows {
			y.SquadMin, y.SquadMax = min(y.SquadMin, row.Players), max(y.SquadMax, row.Players)
			y.ClubAverage[row.ID] = row.AverageOverall
			f, _ := w.Finances(row.ID)
			y.Balance[row.ID] = f.Balance
		}
		for _, table := range w.Tables() {
			div := 2
			if upper[table.Competition] {
				div = 1
			}
			for _, r := range table.Rows {
				y.Division[r.Label.Club] = div
			}
		}
		if len(y.Division) != s.Clubs {
			t.Fatalf("seed %d year %d: %d clubs in the leagues, want %d", seed, year, len(y.Division), s.Clubs)
		}
		var snap []SquadPlayer
		now := map[ids.PlayerID]bool{}
		for _, id := range w.activePlayers() {
			sp := w.squadPlayer(id)
			now[id] = true
			snap = append(snap, sp)
			y.Ages = append(y.Ages, sp.Age)
			y.Overalls = append(y.Overalls, sp.Overall)
			if !active[id] && year > 0 {
				y.Youth++
			}
		}
		for id := range active {
			if !now[id] {
				y.Retired++
				age, err := w.age(id, w.Now())
				if err != nil {
					t.Fatal(err)
				}
				y.RetireAges = append(y.RetireAges, age)
			}
		}
		clear(active)
		for id := range now {
			active[id] = true
		}
		slices.Sort(y.Ages)
		slices.Sort(y.Overalls)
		slices.Sort(y.RetireAges)
		if slices.Contains(popCheckpoints, year) {
			run.Snapshots[year] = snap
		}
		for _, e := range w.finance.All() {
			if e.At <= from || e.At > w.Now() {
				continue
			}
			switch e.Kind {
			case finance.KindGate:
				y.Gate[e.Club] += e.Amount
			case finance.KindWages:
				y.Wages[e.Club] -= e.Amount
			case finance.KindTransfer:
				y.Net[e.Club] += e.Amount
			case finance.KindPayoff:
				y.Payoff[e.Club] -= e.Amount
			default:
				y.Other[e.Club] += e.Amount
			}
		}
		return y
	}
	active := map[ids.PlayerID]bool{}
	from := w.Now()
	run.Years = append(run.Years, measure(0, from, active))
	for year := 1; year <= years; year++ {
		from = w.Now()
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
		y := measure(year, from, active)
		prev := run.Years[year-1]
		for c, bal := range y.Balance {
			if moved := y.Gate[c] + y.Net[c] + y.Other[c] - y.Wages[c] - y.Payoff[c]; bal-prev.Balance[c] != moved {
				t.Fatalf("seed %d year %d club %d: balance moved %d, postings %d", seed, year, c, bal-prev.Balance[c], moved)
			}
		}
		run.Years = append(run.Years, y)
		if err := w.Validate(); err != nil {
			t.Fatalf("seed %d year %d: %v", seed, year, err)
		}
	}
	return run
}

// pct returns the p-th percentile of sorted xs (nearest rank, rounded down).
func pct(xs []int, p int) int {
	if len(xs) == 0 {
		return 0
	}
	return xs[(len(xs)-1)*p/100]
}

// TestBalancePopulation plays AI-only careers for 30 years and logs the
// population (sizes, ages, overall, retirements against youth intake,
// strength by division), every attribute's spread per position, and the
// clubs' money by division, for docs/balance.md.
//
//	ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1
func TestBalancePopulation(t *testing.T) {
	requireBalanceSweep(t)
	const years = 30
	runs := make([]popRun, len(balanceSeeds))
	t.Run("seeds", func(t *testing.T) {
		for i, s := range balanceSeeds {
			t.Run(fmt.Sprint(s), func(t *testing.T) {
				t.Parallel()
				runs[i] = sweepPopulation(t, s, years)
			})
		}
	})
	t.Log(reportPopulation(runs, years))
	t.Log(reportAttributes(runs))
	t.Log(reportMoney(runs, years))
}

func reportPopulation(runs []popRun, years int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\npopulation: %d seeds, means over seeds (ages and overall pooled)\n", len(runs))
	fmt.Fprintln(&b, "year  active free  retired youth ret-age  squad   age mean p10 p90 <=20% >=31%   ovr mean p10 p90   div1 div2 gap  div2>div1-median")
	for _, yr := range []int{0, 1, 2, 3, 5, 10, 15, 20, 25, 30} {
		if yr > years {
			continue
		}
		col := func(f func(popYear) int) int {
			var xs []int
			for _, r := range runs {
				xs = append(xs, f(r.Years[yr]))
			}
			return mean(xs)
		}
		var ages, ovrs, retire []int
		var div [3][]int
		above, lo, hi := 0, 1<<30, 0
		for _, r := range runs {
			y := r.Years[yr]
			ages, ovrs, retire = append(ages, y.Ages...), append(ovrs, y.Overalls...), append(retire, y.RetireAges...)
			lo, hi = min(lo, y.SquadMin), max(hi, y.SquadMax)
			var d1 []int
			for c, d := range y.Division {
				div[d] = append(div[d], y.ClubAverage[c])
				if d == 1 {
					d1 = append(d1, y.ClubAverage[c])
				}
			}
			slices.Sort(d1)
			for c, d := range y.Division {
				if d == 2 && y.ClubAverage[c] > pct(d1, 50) {
					above++
				}
			}
		}
		slices.Sort(ages)
		slices.Sort(ovrs)
		young, old := 0, 0
		for _, a := range ages {
			if a <= 20 {
				young++
			}
			if a >= 31 {
				old++
			}
		}
		d1, d2 := mean(div[1]), mean(div[2])
		fmt.Fprintf(&b, "%4d  %6d %4d  %7d %5d %7d  %2d-%2d   %8d %3d %3d %5d %5d   %8d %3d %3d   %4d %4d %3d  %.1f\n", yr,
			col(func(y popYear) int { return y.Active }), col(func(y popYear) int { return y.FreeAgents }),
			col(func(y popYear) int { return y.Retired }), col(func(y popYear) int { return y.Youth }), mean(retire),
			lo, hi, mean(ages), pct(ages, 10), pct(ages, 90), 100*young/len(ages), 100*old/len(ages),
			mean(ovrs), pct(ovrs, 10), pct(ovrs, 90), d1, d2, d1-d2, float64(above)/float64(len(runs)))
	}
	// Over the whole career, per seed: retirements against intake, and the
	// population's range.
	fmt.Fprintln(&b, "\nper seed over the career: retired youth  active(min-max)  age-mean(min-max)  ovr-mean(min-max)  div-gap(min..max)")
	for _, r := range runs {
		ret, youth := 0, 0
		var act, age, ovr, gap []int
		for i, y := range r.Years {
			ret, youth = ret+y.Retired, youth+y.Youth
			act, age, ovr = append(act, y.Active), append(age, mean(y.Ages)), append(ovr, mean(y.Overalls))
			if i > 0 {
				var div [3][]int
				for c, d := range y.Division {
					div[d] = append(div[d], y.ClubAverage[c])
				}
				gap = append(gap, mean(div[1])-mean(div[2]))
			}
		}
		fmt.Fprintf(&b, "%4d  %4d %4d  (%d-%d)  (%d-%d)  (%d-%d)  (%d..%d)\n", r.Seed, ret, youth,
			slices.Min(act), slices.Max(act), slices.Min(age), slices.Max(age), slices.Min(ovr), slices.Max(ovr), slices.Min(gap), slices.Max(gap))
	}
	return b.String()
}

// reportAttributes logs, per position, each attribute's mean and 10th-90th
// percentiles at the checkpoints, then the means by age band at the first
// and the last checkpoint. Pooled over seeds.
func reportAttributes(runs []popRun) string {
	var b strings.Builder
	short := []string{"gk", "def", "pas", "fin", "pac", "sta", "dri", "hea", "str", "acc", "pos"}
	head := func() {
		fmt.Fprint(&b, "      ")
		for _, s := range short {
			fmt.Fprintf(&b, " %10s", s)
		}
		fmt.Fprintf(&b, " %10s\n", "overall")
	}
	for _, cp := range popCheckpoints {
		if runs[0].Snapshots[cp] == nil {
			continue
		}
		fmt.Fprintf(&b, "\nattributes at year %d: mean (p10-p90), %d seeds pooled\n", cp, len(runs))
		head()
		for _, pos := range players.Positions() {
			vals := make([][]int, players.NumAttributes+1)
			for _, r := range runs {
				for _, p := range r.Snapshots[cp] {
					if p.Position != pos {
						continue
					}
					for a, v := range p.Attributes {
						vals[a] = append(vals[a], int(v))
					}
					vals[players.NumAttributes] = append(vals[players.NumAttributes], p.Overall)
				}
			}
			fmt.Fprintf(&b, "%-4s %3d", pos, len(vals[0])/len(runs))
			for _, xs := range vals {
				slices.Sort(xs)
				fmt.Fprintf(&b, " %10s", fmt.Sprintf("%d(%d-%d)", mean(xs), pct(xs, 10), pct(xs, 90)))
			}
			fmt.Fprintln(&b)
		}
	}
	bands := [][2]int{{16, 20}, {21, 25}, {26, 30}, {31, 36}}
	for _, cp := range []int{popCheckpoints[0], popCheckpoints[len(popCheckpoints)-1]} {
		if runs[0].Snapshots[cp] == nil {
			continue
		}
		fmt.Fprintf(&b, "\nattribute means by age band at year %d, %d seeds pooled (n per seed)\n", cp, len(runs))
		fmt.Fprint(&b, "            ")
		for _, s := range short {
			fmt.Fprintf(&b, " %4s", s)
		}
		fmt.Fprintln(&b, "  ovr")
		for _, pos := range players.Positions() {
			for _, band := range bands {
				vals := make([][]int, players.NumAttributes+1)
				for _, r := range runs {
					for _, p := range r.Snapshots[cp] {
						if p.Position != pos || p.Age < band[0] || p.Age > band[1] {
							continue
						}
						for a, v := range p.Attributes {
							vals[a] = append(vals[a], int(v))
						}
						vals[players.NumAttributes] = append(vals[players.NumAttributes], p.Overall)
					}
				}
				fmt.Fprintf(&b, "%-3s %2d-%2d %3d", pos, band[0], band[1], len(vals[0])/len(runs))
				for _, xs := range vals {
					fmt.Fprintf(&b, " %4d", mean(xs))
				}
				fmt.Fprintln(&b)
			}
		}
	}
	return b.String()
}

// reportMoney logs the clubs' money by division: per year, the mean balance
// and its range, gate receipts, wages, net transfer fees and payoffs, and
// how many clubs are below zero or below a quarter of the opening balance,
// how many balances fell in the year and how many gates (alone, then with
// the cup prizes) did not cover the wages.
func reportMoney(runs []popRun, years int) string {
	var b strings.Builder
	k := func(m money.Money) int { return int(int64(m) / money.MinorPerUnit / 1000) }
	fmt.Fprintf(&b, "\nmoney by division (the division played in the year), %d seeds pooled, thousands\n", len(runs))
	fmt.Fprintln(&b, "year div  bal-mean bal-min bal-max   gate wages  gate-wages  transfers payoffs other  <0 <500k  fell op-loss inc-loss")
	for _, yr := range []int{1, 2, 3, 5, 10, 15, 20, 25, 30} {
		if yr > years {
			continue
		}
		for div := 1; div <= 2; div++ {
			var bal, gate, wages, op, net, pay, other []int
			neg, poor, fell, opLoss, incLoss := 0, 0, 0, 0, 0
			for _, r := range runs {
				y, played := r.Years[yr], r.Years[yr-1].Division
				for c, d := range played {
					if d != div {
						continue
					}
					bal = append(bal, k(y.Balance[c]))
					gate, wages = append(gate, k(y.Gate[c])), append(wages, k(y.Wages[c]))
					op = append(op, k(y.Gate[c]-y.Wages[c]))
					net, pay, other = append(net, k(y.Net[c])), append(pay, k(y.Payoff[c])), append(other, k(y.Other[c]))
					if y.Balance[c] < 0 {
						neg++
					}
					if y.Balance[c] < money.Units(500_000) {
						poor++
					}
					if y.Balance[c] < r.Years[yr-1].Balance[c] {
						fell++
					}
					if y.Gate[c] < y.Wages[c] {
						opLoss++
					}
					if y.Gate[c]+y.Other[c] < y.Wages[c] {
						incLoss++
					}
				}
			}
			fmt.Fprintf(&b, "%4d %3d  %8d %7d %7d  %5d %5d  %10d  %9d %7d %5d  %2d %5d  %4d %7d %8d\n", yr, div,
				mean(bal), slices.Min(bal), slices.Max(bal), mean(gate), mean(wages), mean(op), mean(net), mean(pay), mean(other), neg, poor, fell, opLoss, incLoss)
		}
	}
	// A club trends towards insolvency when its balance falls in most years.
	fmt.Fprintln(&b, "\nper seed: clubs whose balance fell in at least 2 of 3 years over the career; poorest club at years 10 and 30; richest at 30")
	for _, r := range runs {
		falling := 0
		for c := range r.Years[0].Balance {
			down := 0
			for i := 1; i < len(r.Years); i++ {
				if r.Years[i].Balance[c] < r.Years[i-1].Balance[c] {
					down++
				}
			}
			if 3*down >= 2*(len(r.Years)-1) {
				falling++
			}
		}
		lowest := func(y popYear) money.Money {
			m := money.Money(1 << 62)
			for _, v := range y.Balance {
				m = min(m, v)
			}
			return m
		}
		highest := func(y popYear) money.Money {
			var m money.Money
			for _, v := range y.Balance {
				m = max(m, v)
			}
			return m
		}
		last := r.Years[len(r.Years)-1]
		fmt.Fprintf(&b, "%4d  falling %d  poorest %s / %s  richest %s\n", r.Seed, falling,
			thousands(lowest(r.Years[min(10, len(r.Years)-1)])), thousands(lowest(last)), thousands(highest(last)))
	}
	return b.String()
}

// ---- Rotation policy on matched worlds ----

// rotationArms are the manager's lineup policies, each played by the same
// club in the same seeded world:
//
//   - carry: submits the AI's selection for the first match and nothing after,
//     so the world carries that lineup into every later match and only refills
//     injured places (the manager who sets his eleven once). A manager who
//     never submits gets the AI's selection every match, the same as rotate:
//     the carry-over chain starts only from a stored lineup;
//   - rotate: submits the AI's selection each match (ai.SelectTeam ranks by
//     RoleScore × condition, so a tired player gives way);
//   - best: submits the selection with every condition read as 100 (the
//     strongest eleven by rating, whoever is tired).
var rotationArms = []string{"carry", "rotate", "best"}

// rotationRun is one managed club's football year under one policy.
type rotationRun struct {
	Points, Played int // league
	GoalsFor       int // league
	GoalsAgainst   int // league
	Injuries       int
	DaysLost       int
	Starts         int
	CondSum        int // the starters' condition before their matches
	Below90        int
	Appearances    map[ids.PlayerID]int
	// Cup matches the club played, and its starts by the days since its
	// previous match: under a week, a week up to three, three or more (the
	// first match counts as three or more), with those starters below 90.
	CupMatches  int
	RestStarts  [3]int
	RestBelow90 [3]int
}

// restBucket is the rest bucket of a match played gap game minutes after the
// club's previous one (zero: there was none).
func restBucket(gap sim.GameInstant) int {
	switch {
	case gap == 0 || gap >= 21*sim.GameInstant(sim.Day):
		return 2
	case gap >= 7*sim.GameInstant(sim.Day):
		return 1
	}
	return 0
}

// rotationPolicy is the lineup the manager submits for a pending fixture, or
// false to submit nothing.
func rotationPolicy(t *testing.T, w *World, arm string, fixture ids.FixtureID, first bool) (selection.Lineup, bool) {
	t.Helper()
	switch arm {
	case "carry":
		if !first {
			return selection.Lineup{}, false
		}
		fallthrough
	case "rotate", "balanced", "attacking", "defensive":
		l, err := w.SuggestLineup(fixture)
		if err != nil {
			t.Fatalf("seed %d: SuggestLineup: %v", w.seed, err)
		}
		switch arm {
		case "attacking":
			l.Tactics.Mentality = matches.Attacking
		case "defensive":
			l.Tactics.Mentality = matches.Defensive
		}
		return l, true
	}
	team, rules, err := w.pendingUserFixture(fixture)
	if err != nil {
		t.Fatalf("seed %d: %v", w.seed, err)
	}
	cands, err := w.knownCandidates(team, w.availableSquad(team))
	if err != nil {
		t.Fatalf("seed %d: %v", w.seed, err)
	}
	for i := range cands {
		cands[i].Condition = matches.MaxCondition
	}
	sel, err := ai.SelectTeam(team, cands, rules)
	if err != nil {
		t.Fatalf("seed %d: %v", w.seed, err)
	}
	return aiLineup(sel), true
}

// playManagedYear plays football years (as playSeason does) with the
// manager's club following arm in every one of them, and measures that club
// in the last. The manager signs and renews nobody, so contracts that end in
// an earlier year take players from every arm alike. Year 1 has no cup
// edition; the cup is played during year 2.
func playManagedYear(t *testing.T, w *World, arm string, years int) rotationRun {
	t.Helper()
	club, _ := w.UserClub()
	team, _ := w.userTeam()
	var run rotationRun
	var last sim.GameInstant // the club's previous kickoff
	submitted := false
	seen := w.lastEvent
	for year := 1; year <= years; year++ {
		run = rotationRun{Appearances: map[ids.PlayerID]int{}}
		playManagedFootballYear(t, w, arm, club, team, &run, &last, &submitted, &seen)
	}
	return run
}

func playManagedFootballYear(t *testing.T, w *World, arm string, club ids.ClubID, team ids.TeamID, run *rotationRun, last *sim.GameInstant, submitted *bool, seen *events.ID) {
	t.Helper()
	for _, target := range []func(*World) sim.GameInstant{seasonEnd, playoffEnd, cupsUnderwayEnd} {
		for {
			res := mustContinue(t, w, max(target(w), w.Now()))
			fold := asRounds(res)
			ready, ok := res.(FixtureRoundReady)
			if ok {
				for _, f := range ready.UserFixtures {
					first := !*submitted
					*submitted = true
					if l, submit := rotationPolicy(t, w, arm, f, first); submit {
						if _, err := w.SubmitLineup(SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: f, Lineup: l}); err != nil {
							t.Fatalf("seed %d %s: submit: %v", w.seed, arm, err)
						}
					}
				}
				cond := map[ids.PlayerID]int{}
				for _, id := range w.employment.Squad(team) {
					if c, ok := w.medical.Condition(id); ok {
						cond[id] = int(c)
					}
				}
				cmd := commandFor(ready, w.NextCommandID())
				cmd.ExpectedRevision = w.Revision() // submitted lineups advanced it
				resolved, err := w.ResolveRounds(cmd)
				if err != nil {
					t.Fatalf("seed %d %s: resolve: %v", w.seed, arm, err)
				}
				for _, m := range resolved.Matches {
					for side, label := range []TeamLabel{m.Home, m.Away} {
						if label.Club != club {
							continue
						}
						bucket := restBucket(0)
						if *last != 0 {
							bucket = restBucket(ready.At - *last)
						}
						*last = ready.At
						if matchPhase(w, m.Round.Season) == "cup" {
							run.CupMatches++
						}
						for _, slot := range m.Lineups[side].Starters {
							run.Starts++
							run.CondSum += cond[slot.Player]
							run.RestStarts[bucket]++
							if cond[slot.Player] < 90 {
								run.Below90++
								run.RestBelow90[bucket]++
							}
							run.Appearances[slot.Player]++
						}
					}
				}
				fold = append(fold, resolved)
			}
			for _, e := range w.Events() {
				if e.ID > *seen && e.Kind == events.KindPlayerInjured && e.PlayerInjured != nil && e.PlayerInjured.Club == club {
					run.Injuries++
					run.DaysLost += int(e.PlayerInjured.Days)
				}
			}
			*seen = w.lastEvent
			for _, r := range fold {
				for _, m := range r.Matches {
					if matchPhase(w, m.Round.Season) != "league" {
						continue
					}
					for side, label := range []TeamLabel{m.Home, m.Away} {
						if label.Club != club {
							continue
						}
						mine, theirs := m.Score[side], m.Score[1-side]
						run.Played++
						run.GoalsFor += int(mine)
						run.GoalsAgainst += int(theirs)
						switch {
						case mine > theirs:
							run.Points += 3
						case mine == theirs:
							run.Points++
						}
					}
				}
			}
			if !ok {
				break
			}
		}
	}
}

// rotationSample is every arm's football year for one (seed, club).
type rotationSample struct {
	Seed uint64
	Club ids.ClubID
	Arms [3]rotationRun // by rotationArms
}

// meanSE is the mean and standard error of xs.
func meanSE(xs []float64) (mean, se float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(ss/float64(len(xs)-1)) / math.Sqrt(float64(len(xs)))
}

// TestBalanceRotation plays one football year of the same seeded world once per
// lineup policy for each managed club, and compares the policies on matched
// pairs: points, injuries, days lost and the starters' condition, in football
// year 1 (no cup) and year 2 (a cup edition). The manager signs and renews
// nobody, so the arms differ only in his lineups.
//
//	ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceRotation -v -count=1 -timeout 2h
func TestBalanceRotation(t *testing.T) {
	requireBalanceSweep(t)
	type setup struct {
		engine string
		seeds  []uint64
		step   int // every step-th club by ID
	}
	setups := []setup{
		{"simple", []uint64{1, 2, 3, 4}, 1},
		{"tick", []uint64{7, 42, 2026}, 4},
	}
	for _, su := range setups {
		for _, years := range rotationYears {
			var samples []rotationSample
			for _, seed := range su.seeds {
				base := aiEngineWorld(t, seed, su.engine)
				var clubs []ids.ClubID
				for i, c := range base.registry.Clubs() {
					if i%su.step == 0 {
						clubs = append(clubs, c.ID)
					}
				}
				for _, club := range clubs {
					samples = append(samples, rotationSample{Seed: seed, Club: club})
				}
			}
			t.Run(fmt.Sprintf("%s-year%d", su.engine, years), func(t *testing.T) {
				for i := range samples {
					for armIdx, arm := range rotationArms {
						t.Run(fmt.Sprintf("%d-%d-%s", samples[i].Seed, samples[i].Club, arm), func(t *testing.T) {
							t.Parallel()
							cfg := DefaultConfig(random.Seed(samples[i].Seed))
							cfg.UserClub, cfg.Engine = samples[i].Club, su.engine
							w, err := NewWorld(cfg)
							if err != nil {
								t.Fatal(err)
							}
							samples[i].Arms[armIdx] = playManagedYear(t, w, arm, years)
						})
					}
				}
			})
			t.Log(reportRotation(fmt.Sprintf("%s, football year %d", su.engine, years), samples) + reportRest(samples))
		}
	}
}

// rotationYears are the football years the policies are compared in: year 1
// has no cup, year 2 plays one during the league season. The manager's
// policy applies in every year up to the measured one.
var rotationYears = []int{1, 2}

// reportRest formats the cup exposure and the rest buckets of every arm: the
// matches of the club's year, and by days since its previous match the share
// of its starts and of those starters who were below 90 condition.
func reportRest(samples []rotationSample) string {
	var b strings.Builder
	b.WriteString("\n| Policy | Cup matches | Starts under 7 days after the previous match % | of them below 90 % | Starts 7–20 days after % | of them below 90 % | Starts 21+ days after (or the first) % | of them below 90 % |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for ai, arm := range rotationArms {
		var cup, starts int
		var rs, rb [3]int
		for _, s := range samples {
			r := s.Arms[ai]
			cup += r.CupMatches
			starts += r.Starts
			for k := range rs {
				rs[k] += r.RestStarts[k]
				rb[k] += r.RestBelow90[k]
			}
		}
		pct := func(n, d int) float64 { return 100 * float64(n) / float64(max(d, 1)) }
		fmt.Fprintf(&b, "| %s | %.2f | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f |\n", arm, float64(cup)/float64(max(len(samples), 1)),
			pct(rs[0], starts), pct(rb[0], rs[0]), pct(rs[1], starts), pct(rb[1], rs[1]), pct(rs[2], starts), pct(rb[2], rs[2]))
	}
	return b.String()
}

// reportRotation formats the arms' means and the paired differences against
// "rotate" as markdown rows for docs/balance.md.
func reportRotation(engine string, samples []rotationSample) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s: %d matched (seed, club) football years per policy\n", engine, len(samples))
	b.WriteString("\n| Policy | League points | Injuries | Days lost | Starters' condition at kickoff | Starts below 90 % | Players used in the league year | Starts by the 14 most-used % |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	col := func(ai int, f func(r rotationRun) float64) []float64 {
		out := make([]float64, len(samples))
		for i, s := range samples {
			out[i] = f(s.Arms[ai])
		}
		return out
	}
	for ai, arm := range rotationArms {
		pts, _ := meanSE(col(ai, func(r rotationRun) float64 { return float64(r.Points) }))
		inj, _ := meanSE(col(ai, func(r rotationRun) float64 { return float64(r.Injuries) }))
		days, _ := meanSE(col(ai, func(r rotationRun) float64 { return float64(r.DaysLost) }))
		cond, _ := meanSE(col(ai, func(r rotationRun) float64 { return float64(r.CondSum) / float64(max(r.Starts, 1)) }))
		b90, _ := meanSE(col(ai, func(r rotationRun) float64 { return 100 * float64(r.Below90) / float64(max(r.Starts, 1)) }))
		used, _ := meanSE(col(ai, func(r rotationRun) float64 { return float64(len(r.Appearances)) }))
		top, _ := meanSE(col(ai, func(r rotationRun) float64 {
			var apps []int
			for _, n := range r.Appearances {
				apps = append(apps, n)
			}
			slices.SortFunc(apps, func(a, b int) int { return b - a })
			sum := 0
			for i, n := range apps {
				if i < 14 {
					sum += n
				}
			}
			return 100 * float64(sum) / float64(max(r.Starts, 1))
		}))
		fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.0f | %.1f | %.0f | %.1f | %.0f |\n", arm, pts, inj, days, cond, b90, used, top)
	}
	b.WriteString("\nPaired differences against rotate (mean ± standard error over the matched pairs):\n\n| Policy | League points | Injuries | Days lost |\n| --- | --- | --- | --- |\n")
	rot := slices.Index(rotationArms, "rotate")
	for ai, arm := range rotationArms {
		if ai == rot {
			continue
		}
		diff := func(f func(r rotationRun) float64) string {
			d := make([]float64, len(samples))
			for i, s := range samples {
				d[i] = f(s.Arms[ai]) - f(s.Arms[rot])
			}
			m, se := meanSE(d)
			return fmt.Sprintf("%+.2f ± %.2f", m, se)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", arm,
			diff(func(r rotationRun) float64 { return float64(r.Points) }),
			diff(func(r rotationRun) float64 { return float64(r.Injuries) }),
			diff(func(r rotationRun) float64 { return float64(r.DaysLost) }))
	}
	return b.String()
}

// mentalityArms are the mentalities a managed club submits every match with
// the AI's rotating selection.
var mentalityArms = []string{"balanced", "attacking", "defensive"}

// TestBalanceMentalityCareer plays one football year per mentality for each
// managed club of the same seeded career world (the manager signs and renews
// nobody) and compares the mentalities on matched pairs: points and goals
// for and against. Unlike the flat-input TestBalanceMentalityByGap, the
// squads, opponents and matchdays are a career's.
//
//	ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceMentalityCareer -v -count=1 -timeout 3h
func TestBalanceMentalityCareer(t *testing.T) {
	requireBalanceSweep(t)
	type setup struct {
		engine string
		seeds  []uint64
		step   int
	}
	for _, su := range []setup{{"simple", []uint64{1, 2, 3, 4}, 1}, {"tick", []uint64{7, 42, 2026}, 4}} {
		var samples []rotationSample
		for _, seed := range su.seeds {
			base := aiEngineWorld(t, seed, su.engine)
			for i, c := range base.registry.Clubs() {
				if i%su.step == 0 {
					samples = append(samples, rotationSample{Seed: seed, Club: c.ID})
				}
			}
		}
		t.Run(su.engine, func(t *testing.T) {
			for i := range samples {
				for armIdx, arm := range mentalityArms {
					t.Run(fmt.Sprintf("%d-%d-%s", samples[i].Seed, samples[i].Club, arm), func(t *testing.T) {
						t.Parallel()
						cfg := DefaultConfig(random.Seed(samples[i].Seed))
						cfg.UserClub, cfg.Engine = samples[i].Club, su.engine
						w, err := NewWorld(cfg)
						if err != nil {
							t.Fatal(err)
						}
						samples[i].Arms[armIdx] = playManagedYear(t, w, arm, 1)
					})
				}
			}
		})
		var b strings.Builder
		fmt.Fprintf(&b, "\n%s: %d matched (seed, club) football years per mentality\n\n| Mentality | League points | Goals for | Goals against | Goal difference |\n| --- | --- | --- | --- | --- |\n", su.engine, len(samples))
		bal := slices.Index(mentalityArms, "balanced")
		for ai, arm := range mentalityArms {
			col := func(f func(r rotationRun) float64) []float64 {
				out := make([]float64, len(samples))
				for i, s := range samples {
					out[i] = f(s.Arms[ai])
				}
				return out
			}
			diff := func(f func(r rotationRun) float64) string {
				d := make([]float64, len(samples))
				for i, s := range samples {
					d[i] = f(s.Arms[ai]) - f(s.Arms[bal])
				}
				m, se := meanSE(d)
				return fmt.Sprintf("%.2f (%+.2f ± %.2f)", mean64(col(f)), m, se)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", arm,
				diff(func(r rotationRun) float64 { return float64(r.Points) }),
				diff(func(r rotationRun) float64 { return float64(r.GoalsFor) }),
				diff(func(r rotationRun) float64 { return float64(r.GoalsAgainst) }),
				diff(func(r rotationRun) float64 { return float64(r.GoalsFor - r.GoalsAgainst) }))
		}
		t.Log(b.String())
	}
}

func mean64(xs []float64) float64 {
	m, _ := meanSE(xs)
	return m
}
