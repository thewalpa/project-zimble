package ai

import (
	"cmp"
	"math"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// TransfersVersion identifies the transfer heuristics: valuations, budgets,
// the answer to a bid and the choice of a bid's target. Bump it whenever the
// same squads, balances and offers would produce different decisions.
const TransfersVersion = 1

const (
	// ValueAt60Units is a 60-overall player's value, in currency units, in
	// his prime with at least three contract years to run. Value grows
	// with the cube of overall: a 75 is worth about twice as much, a 90
	// about three and a half times.
	ValueAt60Units = 600_000
	// ValueStepUnits rounds valuations to a whole number of these units;
	// no player is valued below one step.
	ValueStepUnits = 10_000
	// ReserveWeeks is how many weeks of its wage bill an AI club keeps in
	// hand; the rest of its balance is its transfer budget.
	ReserveWeeks = 26
	// TransferMargin is how much better than the best free agent at the
	// position a player must be for an AI club to pay a fee for him.
	TransferMargin = 5
)

// agePermille is a player's value by age, in permille: young players carry a
// premium for their improvement, and value falls from 28 as they decline.
func agePermille(age int) int64 {
	switch {
	case age <= 21:
		return 1200
	case age <= 27:
		return 1000
	case age <= 29:
		return 800
	case age <= 31:
		return 550
	case age <= 33:
		return 350
	}
	return 200
}

// contractPermille is a player's value by the contract years he has left
// (the current one included): a player about to leave for nothing is worth
// less to his club.
func contractPermille(years int) int64 {
	switch {
	case years <= 1:
		return 600
	case years == 2:
		return 850
	}
	return 1000
}

// Valuation is what a club values a player at: ValueAt60Units scaled by
// (overall / 60)³, by age and by the contract years left, rounded to the
// nearest ValueStepUnits and at least one step. It is also the fee at which
// an AI club sells and the fee it bids. Integer arithmetic only.
func Valuation(overall, age, contractYears int) money.Money {
	o := int64(max(overall, 1))
	v := int64(money.Units(ValueAt60Units)) * o * o * o / (60 * 60 * 60)
	v = v * agePermille(age) / 1000
	v = v * contractPermille(contractYears) / 1000
	step := int64(money.Units(ValueStepUnits))
	return money.Money(max((v+step/2)/step*step, step))
}

// AcceptBid reports whether an AI club accepts a bid: a fixed fee at or
// above its valuation of the player.
func AcceptBid(fee, valuation money.Money) bool { return fee >= valuation }

// TransferBudget is what an AI club may spend on one fee: its balance minus
// ReserveWeeks of its weekly wage bill, never below zero.
func TransferBudget(balance, weeklyWages money.Money) money.Money {
	if weeklyWages < 0 || weeklyWages > math.MaxInt64/ReserveWeeks {
		return 0
	}
	left, err := balance.Add(-weeklyWages * ReserveWeeks)
	if err != nil || left < 0 {
		return 0
	}
	return left
}

// LargestNeed returns the role a club needs most (ties: role order), or
// false when it needs nothing.
func LargestNeed(needs []RoleCount) (matches.Role, bool) {
	best, count := matches.Role(0), 0
	for _, role := range []matches.Role{matches.Goalkeeper, matches.Defender, matches.Midfielder, matches.Forward} {
		n := 0
		for _, rc := range needs {
			if rc.Role == role {
				n += rc.Count
			}
		}
		if n > count {
			best, count = role, n
		}
	}
	return best, count > 0
}

// TransferCandidate is another club's player an AI club could bid for, with
// the fee it would bid (its valuation).
type TransferCandidate struct {
	Player  ids.PlayerID
	Club    ids.ClubID // the player's club
	Overall int
	Value   money.Money
}

// ChooseTarget picks the player an AI club bids for to fill a need: among
// candidates of other clubs whose value fits the budget and whose overall is
// at least TransferMargin above the best free agent the club could sign
// instead (0 if none), the best overall, then the cheapest, then the lowest
// player ID. The caller passes only players the rules allow it to buy. It
// returns false when no one qualifies: the club signs a free agent instead.
func ChooseTarget(club ids.ClubID, candidates []TransferCandidate, bestFreeAgent int, budget money.Money) (TransferCandidate, bool) {
	var eligible []TransferCandidate
	for _, c := range candidates {
		if c.Club != club && c.Value <= budget && c.Overall >= bestFreeAgent+TransferMargin {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) == 0 {
		return TransferCandidate{}, false
	}
	return slices.MinFunc(eligible, func(a, b TransferCandidate) int {
		return cmp.Or(cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Value, b.Value), cmp.Compare(a.Player, b.Player))
	}), true
}
