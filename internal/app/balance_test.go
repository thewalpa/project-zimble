package app

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
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
func sweepMarket(t *testing.T, seed uint64, club ids.ClubID, years int) marketRun {
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
		mustContinue(t, w, w.TransferWindow().Closes)

		listed := map[ids.PlayerID]ids.ClubID{}
		if len(w.journal) > 0 && w.journal[0].ID > seen+1 {
			t.Fatalf("seed %d year %d: the journal dropped events %d..%d before they were read", seed, year, seen+1, w.journal[0].ID-1)
		}
		for _, e := range w.journal {
			if e.ID > seen && e.Kind == events.KindPlayerListed && e.PlayerListed.Club != w.userClub {
				listed[e.PlayerListed.Player] = e.PlayerListed.Club
			}
		}
		seen = w.lastEvent
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
		mustContinue(t, w, w.ContractYearEnd())

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

func sweepSeeds(t *testing.T, seeds []uint64, club ids.ClubID, years int) []marketRun {
	runs := make([]marketRun, len(seeds))
	t.Run("seeds", func(t *testing.T) {
		for i, s := range seeds {
			t.Run(fmt.Sprint(s), func(t *testing.T) {
				t.Parallel()
				runs[i] = sweepMarket(t, s, club, years)
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
	fmt.Fprintln(&b, "year  bids done rej exp col maxclub weaker stars16 listed unsold  fa-open fa-close best-fa  avg-top avg-bot avg-mean  bal-min bal-med bal-max neg")
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
		fmt.Fprintf(&b, "%4s  %4d %4d %3d %3d %3d %7d %6d %7d %6d %6d  %7d %8d %7d  %7d %7d %8d  %7s %7s %7s %3d\n", label,
			col(func(y marketYear) int { return y.Bids }), col(func(y marketYear) int { return y.Completed }),
			col(func(y marketYear) int { return y.Rejected }), col(func(y marketYear) int { return y.Expired }),
			col(func(y marketYear) int { return y.Collapsed }), col(func(y marketYear) int { return y.MaxClubMoves }),
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
		fmt.Fprintln(&b, "\nmanager's club (passive): year  bids-for-his-players expired  squad-avg  balance")
		for _, yr := range []int{1, 2, 3, 5, 10, 20, 30} {
			if yr > years {
				continue
			}
			var bids, expired, avg, bal []int
			for _, r := range runs {
				y := r.Years[yr-1]
				bids, expired, avg = append(bids, y.UserBids), append(expired, y.UserBidsExpired), append(avg, y.UserAverage)
				bal = append(bal, int(int64(y.UserBalance)/money.MinorPerUnit/1000))
			}
			fmt.Fprintf(&b, "  %4d  %4d %4d  %4d  %dk\n", yr, mean(bids), mean(expired), mean(avg), mean(bal))
		}
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
	reportMarket(t, sweepSeeds(t, balanceSeeds, 0, 30), false)
}

func TestBalanceAIMarketWithPassiveManager(t *testing.T) {
	requireBalanceSweep(t)
	reportMarket(t, sweepSeeds(t, []uint64{7, 42, 99, 2026}, userClub3, 30), true)
}
