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
// the answer to a bid, the choice of a bid's target, and which players a
// club lists and at what price. Bump it whenever the same squads, balances
// and offers would produce different decisions.
const TransfersVersion = 6

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
	// UpgradeMargin is how much better than its weakest player at a
	// position a player must be for an AI club without a vacancy to buy him.
	UpgradeMargin = 8
	// ListingPermille is an AI club's asking price for a player it has
	// listed, in permille of its valuation: it sells a player it does not
	// need at a discount.
	ListingPermille = 800
	// KeyPermillePerPoint is how much more an AI club asks for a player it
	// has not listed, in permille of its valuation, for each point he rates
	// above its squad average.
	KeyPermillePerPoint = 60
	// StarMargin is how far above his club's squad average a player rates
	// to be one of its stars, who joins only a club at least as strong.
	StarMargin = 10
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
// nearest ValueStepUnits and at least one step. An AI club's price for a
// player it has not listed starts from it (SellingPrice). Integer
// arithmetic only.
func Valuation(overall, age, contractYears int) money.Money {
	o := int64(max(overall, 1))
	v := int64(money.Units(ValueAt60Units)) * o * o * o / (60 * 60 * 60)
	v = v * agePermille(age) / 1000
	v = v * contractPermille(contractYears) / 1000
	step := int64(money.Units(ValueStepUnits))
	return money.Money(max((v+step/2)/step*step, step))
}

// ListingPrice is the asking price at which an AI club lists a player it
// values at valuation: ListingPermille of it, rounded to the nearest
// ValueStepUnits and at least one step.
func ListingPrice(valuation money.Money) money.Money {
	step := int64(money.Units(ValueStepUnits))
	v := int64(max(valuation, 0)) / 1000 * ListingPermille
	return money.Money(max((v+step/2)/step*step, step))
}

// Sale is a bid an AI club answers, with what it knows about the player.
// Whether the player agrees to join the buyer (Joins) is not the club's
// decision: it is checked when the transfer completes, whoever the seller.
type Sale struct {
	Fee   money.Money // the bid
	Price money.Money // its price for the player (SellingPrice, or his asking price if listed)
	// Spare: the sale leaves the club at or above its roster count at his
	// position. Replaceable: it still has time to replace him in the window.
	Spare, Replaceable bool
	// Listed: the club has listed him. Settling: he joined it by transfer in
	// the previous window.
	Listed, Settling bool
}

// AcceptBid reports whether an AI club accepts a bid: a fee at or above its
// price, for a player it can spare or still has time to replace, who, unless
// it listed him, is not still settling in (a club does not sell on a player
// it bought in the previous window).
func AcceptBid(s Sale) bool {
	return s.Fee >= s.Price && (s.Spare || s.Replaceable) && (s.Listed || !s.Settling)
}

// Joins reports whether a player agrees to move between clubs with these
// squad averages: a star (rated at least StarMargin above his club's
// average) joins only a club at least as strong. It is the player's
// consent, the same for every seller and buyer.
func Joins(overall, sellerAverage, buyerAverage int) bool {
	return overall < sellerAverage+StarMargin || buyerAverage >= sellerAverage
}

// SellingPrice is what an AI club asks for a player it has not listed, whom
// it values at valuation: KeyPermillePerPoint more for each point his
// overall is above its squad average (a club's best players cost it the
// most to lose), rounded to the nearest ValueStepUnits and at least one step.
func SellingPrice(valuation money.Money, overall, squadAverage int) money.Money {
	permille := int64(1000 + KeyPermillePerPoint*max(overall-squadAverage, 0))
	step := int64(money.Units(ValueStepUnits))
	v := int64(max(valuation, 0)) / 1000 * permille
	return money.Money(max((v+step/2)/step*step, step))
}

// Member is one of a club's players at a position.
type Member struct {
	Player  ids.PlayerID
	Overall int
}

// Surplus returns the players a club does not need at a position where it
// holds more than keep: everyone but its best keep (by overall, then the
// lower ID), in ID order. The club lists them.
func Surplus(members []Member, keep int) []ids.PlayerID {
	if len(members) <= keep {
		return nil
	}
	ranked := slices.Clone(members)
	slices.SortFunc(ranked, func(a, b Member) int {
		return cmp.Or(cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Player, b.Player))
	})
	var out []ids.PlayerID
	for _, m := range ranked[max(keep, 0):] {
		out = append(out, m.Player)
	}
	slices.Sort(out)
	return out
}

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
// the fee it would bid: his club's price (the asking price if he is listed,
// else its valuation).
type TransferCandidate struct {
	Player  ids.PlayerID
	Club    ids.ClubID // the player's club
	Role    matches.Role
	Overall int
	Value   money.Money
	Listed  bool // on the transfer list
}

// listedFirst narrows eligible candidates to the listed ones, if any: an AI
// club looks at the transfer list first.
func listedFirst(eligible []TransferCandidate) []TransferCandidate {
	if listed := slices.DeleteFunc(slices.Clone(eligible), func(c TransferCandidate) bool { return !c.Listed }); len(listed) > 0 {
		return listed
	}
	return eligible
}

// ChooseTarget picks the player an AI club bids for to fill a need: among
// candidates of other clubs whose value fits the budget and whose overall is
// at least TransferMargin above the best free agent the club could sign
// instead (0 if none), listed ones first, the best overall, then the
// cheapest, then the lowest player ID. The caller passes only players the
// rules allow it to buy. It returns false when no one qualifies: the club
// signs a free agent instead.
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
	return slices.MinFunc(listedFirst(eligible), func(a, b TransferCandidate) int {
		return cmp.Or(cmp.Compare(b.Overall, a.Overall), cmp.Compare(a.Value, b.Value), cmp.Compare(a.Player, b.Player))
	}), true
}

// ChooseUpgrade picks the player an AI club without a vacancy bids for to
// strengthen its squad: among candidates of other clubs whose value fits the
// budget and whose overall is at least UpgradeMargin above the club's weakest
// player in their role (weakest; roles the club has no player in are
// skipped), listed ones first, the largest improvement, then the cheapest,
// then the lowest player ID. The caller passes only players the rules allow it to buy. The
// club then lists the player it replaces. It returns false when no one
// qualifies.
func ChooseUpgrade(club ids.ClubID, candidates []TransferCandidate, weakest map[matches.Role]int, budget money.Money) (TransferCandidate, bool) {
	gain := func(c TransferCandidate) int { return c.Overall - weakest[c.Role] }
	var eligible []TransferCandidate
	for _, c := range candidates {
		if _, ok := weakest[c.Role]; ok && c.Club != club && c.Value <= budget && gain(c) >= UpgradeMargin {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) == 0 {
		return TransferCandidate{}, false
	}
	return slices.MinFunc(listedFirst(eligible), func(a, b TransferCandidate) int {
		return cmp.Or(cmp.Compare(gain(b), gain(a)), cmp.Compare(a.Value, b.Value), cmp.Compare(a.Player, b.Player))
	}), true
}
