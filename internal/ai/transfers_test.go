package ai

import (
	"math"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/matches"
)

func TestValuationRisesWithOverallAndFallsWithAgeAndContract(t *testing.T) {
	if v := Valuation(60, 25, 3); v != money.Units(ValueAt60Units) {
		t.Fatalf("a 60 in his prime is valued at %s", v)
	}
	for o := 2; o <= 100; o++ {
		if Valuation(o, 25, 3) < Valuation(o-1, 25, 3) {
			t.Fatalf("overall %d is valued below %d", o, o-1)
		}
	}
	for age := 17; age < 40; age++ {
		if Valuation(75, age+1, 3) > Valuation(75, age, 3) {
			t.Fatalf("a %d-year-old is valued above a %d-year-old", age+1, age)
		}
	}
	for years := 1; years < 5; years++ {
		if Valuation(75, 25, years+1) < Valuation(75, 25, years) {
			t.Fatalf("%d years left are valued below %d", years+1, years)
		}
	}
	if v90, v60 := Valuation(90, 25, 3), Valuation(60, 25, 3); v90 < 3*v60 || v90 > 4*v60 {
		t.Fatalf("a 90 is valued at %s, a 60 at %s", v90, v60)
	}
	step := money.Units(ValueStepUnits)
	for _, v := range []money.Money{Valuation(1, 40, 1), Valuation(73, 30, 2), Valuation(100, 17, 4)} {
		if v < step || v%step != 0 {
			t.Fatalf("valuation %s is not a positive whole step", v)
		}
	}
}

func TestAcceptBidAtOrAboveThePrice(t *testing.T) {
	sale := func(fee money.Money) Sale { return Sale{Fee: fee, Price: 100, Spare: true, Replaceable: true} }
	if !AcceptBid(sale(100)) || !AcceptBid(sale(101)) || AcceptBid(sale(99)) {
		t.Fatal("bids are not judged against the price")
	}
}

// A club sells a player it needs only while it can replace him.
func TestAcceptBidKeepsANeededPlayerLate(t *testing.T) {
	s := Sale{Fee: 100, Price: 100}
	if AcceptBid(s) {
		t.Fatal("a needed player is sold with no time to replace him")
	}
	s.Spare = true
	if !AcceptBid(s) {
		t.Fatal("a spare player is kept")
	}
	s.Spare, s.Replaceable = false, true
	if !AcceptBid(s) {
		t.Fatal("a replaceable player is kept")
	}
}

// A player bought in the previous window is not sold on, unless listed.
func TestAcceptBidKeepsAPlayerSettlingIn(t *testing.T) {
	s := Sale{Fee: 100, Price: 100, Spare: true, Replaceable: true, Settling: true}
	if AcceptBid(s) {
		t.Fatal("a player bought last window is sold on")
	}
	if s.Listed = true; !AcceptBid(s) {
		t.Fatal("a listed player is kept")
	}
}

// A club asks more for its better players, never less than its valuation.
func TestSellingPriceRisesWithImportance(t *testing.T) {
	v := Valuation(70, 25, 3)
	if SellingPrice(v, 60, 62) != v || SellingPrice(v, 62, 62) != v {
		t.Fatal("a player at or below the squad average costs more than his valuation")
	}
	prev := v
	for overall := 63; overall <= 100; overall++ {
		p := SellingPrice(v, overall, 62)
		if p <= prev || p%money.Units(ValueStepUnits) != 0 {
			t.Fatalf("price %s at %d after %s", p, overall, prev)
		}
		prev = p
	}
	if SellingPrice(v, 82, 62) < 2*v {
		t.Fatal("a star 20 points above the average costs less than twice his valuation")
	}
}

func TestTransferBudgetKeepsAReserve(t *testing.T) {
	if b := TransferBudget(money.Units(2_000_000), money.Units(30_000)); b != money.Units(2_000_000-26*30_000) {
		t.Fatalf("budget %s", b)
	}
	if TransferBudget(money.Units(500_000), money.Units(30_000)) != 0 || TransferBudget(-5, 0) != 0 {
		t.Fatal("a club short of its reserve has a budget")
	}
	if TransferBudget(math.MaxInt64, math.MaxInt64/2) != 0 || TransferBudget(math.MinInt64, 1) != 0 {
		t.Fatal("extreme amounts overflow")
	}
}

func TestLargestNeed(t *testing.T) {
	if r, ok := LargestNeed([]RoleCount{{matches.Forward, 2}, {matches.Defender, 2}, {matches.Midfielder, 1}}); !ok || r != matches.Defender {
		t.Fatalf("largest need %v %v", r, ok)
	}
	if _, ok := LargestNeed([]RoleCount{{matches.Forward, 0}}); ok {
		t.Fatal("no need reported as a need")
	}
}

func TestChooseTarget(t *testing.T) {
	budget := money.Units(1_000_000)
	cands := []TransferCandidate{
		{Player: 5, Club: 2, Overall: 80, Value: money.Units(1_200_000)}, // too dear
		{Player: 6, Club: 1, Overall: 79, Value: money.Units(100_000)},   // own player
		{Player: 9, Club: 3, Overall: 74, Value: money.Units(700_000)},
		{Player: 7, Club: 2, Overall: 74, Value: money.Units(600_000)}, // cheapest of the best
		{Player: 8, Club: 4, Overall: 74, Value: money.Units(600_000)},
		{Player: 4, Club: 3, Overall: 60, Value: money.Units(300_000)},
	}
	if c, ok := ChooseTarget(1, cands, 60, budget); !ok || c.Player != 7 {
		t.Fatalf("chose %+v %v", c, ok)
	}
	// Order does not matter.
	rev := []TransferCandidate{cands[5], cands[4], cands[3], cands[2], cands[1], cands[0]}
	if c, _ := ChooseTarget(1, rev, 60, budget); c.Player != 7 {
		t.Fatalf("chose %+v from reversed input", c)
	}
	// Not better than a free agent by the margin: sign the free agent.
	if _, ok := ChooseTarget(1, cands, 74-TransferMargin+1, budget); ok {
		t.Fatal("paid a fee for a player barely better than a free agent")
	}
	if c, ok := ChooseTarget(1, cands, 74-TransferMargin, budget); !ok || c.Player != 7 {
		t.Fatal("the margin is not inclusive")
	}
	if _, ok := ChooseTarget(1, cands, 0, 0); ok {
		t.Fatal("chose a target without a budget")
	}
	if _, ok := ChooseTarget(ids.ClubID(1), nil, 0, budget); ok {
		t.Fatal("chose from no candidates")
	}
}

func TestListingPriceIsADiscountedWholeStep(t *testing.T) {
	step := money.Units(ValueStepUnits)
	if p := ListingPrice(money.Units(600_000)); p != money.Units(480_000) {
		t.Fatalf("a player valued at 600,000 is listed at %s", p)
	}
	for _, v := range []money.Money{step, Valuation(55, 30, 1), Valuation(73, 24, 2), Valuation(100, 17, 4)} {
		p := ListingPrice(v)
		if p < step || p%step != 0 || p > v || (v > step && p >= v) {
			t.Fatalf("valuation %s is listed at %s", v, p)
		}
	}
	if ListingPrice(0) != step || ListingPrice(-5) != step {
		t.Fatal("a worthless player is listed below one step")
	}
}

func TestSurplusIsEveryoneButTheBest(t *testing.T) {
	members := []Member{{Player: 4, Overall: 60}, {Player: 2, Overall: 71}, {Player: 9, Overall: 55}, {Player: 3, Overall: 60}, {Player: 7, Overall: 64}}
	// The best three are 2, 7 and 3 (the tie at 60 goes to the lower ID).
	if got := Surplus(members, 3); !slices.Equal(got, []ids.PlayerID{4, 9}) {
		t.Fatalf("surplus %v", got)
	}
	rev := slices.Clone(members)
	slices.Reverse(rev)
	if got := Surplus(rev, 3); !slices.Equal(got, []ids.PlayerID{4, 9}) {
		t.Fatalf("surplus %v from reversed input", got)
	}
	if members[0].Player != 4 || rev[0].Player != 7 {
		t.Fatal("the input was modified")
	}
	if Surplus(members, 5) != nil || Surplus(members, 9) != nil || Surplus(nil, 0) != nil {
		t.Fatal("a club without surplus lists players")
	}
}

func TestChooseUpgrade(t *testing.T) {
	budget := money.Units(1_000_000)
	weakest := map[matches.Role]int{matches.Defender: 60, matches.Forward: 70, matches.Midfielder: 50}
	cands := []TransferCandidate{
		{Player: 5, Club: 2, Role: matches.Midfielder, Overall: 80, Value: money.Units(1_200_000)}, // too dear
		{Player: 6, Club: 1, Role: matches.Midfielder, Overall: 79, Value: money.Units(100_000)},   // own player
		{Player: 3, Club: 3, Role: matches.Goalkeeper, Overall: 90, Value: money.Units(100_000)},   // no keeper to replace
		{Player: 8, Club: 3, Role: matches.Forward, Overall: 77, Value: money.Units(500_000)},      // +7: not enough
		{Player: 9, Club: 3, Role: matches.Defender, Overall: 70, Value: money.Units(700_000)},     // +10
		{Player: 7, Club: 2, Role: matches.Midfielder, Overall: 60, Value: money.Units(600_000)},   // +10, cheapest
		{Player: 4, Club: 3, Role: matches.Midfielder, Overall: 60, Value: money.Units(650_000)},
	}
	if c, ok := ChooseUpgrade(1, cands, weakest, budget); !ok || c.Player != 7 {
		t.Fatalf("chose %+v %v", c, ok)
	}
	rev := slices.Clone(cands)
	slices.Reverse(rev)
	if c, _ := ChooseUpgrade(1, rev, weakest, budget); c.Player != 7 {
		t.Fatalf("chose %+v from reversed input", c)
	}
	// The margin is inclusive.
	if c, ok := ChooseUpgrade(1, cands[3:4], map[matches.Role]int{matches.Forward: 77 - UpgradeMargin}, budget); !ok || c.Player != 8 {
		t.Fatalf("chose %+v %v, want the forward 8 at exactly the margin", c, ok)
	}
	if _, ok := ChooseUpgrade(1, cands, weakest, 0); ok {
		t.Fatal("chose an upgrade without a budget")
	}
	if _, ok := ChooseUpgrade(1, cands, map[matches.Role]int{matches.Midfielder: 75}, budget); ok {
		t.Fatal("chose a player who is no clear upgrade")
	}
}

// AI clubs look at the transfer list first: a listed player who qualifies
// is chosen over better unlisted ones.
func TestListedPlayersComeFirst(t *testing.T) {
	budget := money.Units(1_000_000)
	cands := []TransferCandidate{
		{Player: 5, Club: 2, Role: matches.Defender, Overall: 80, Value: money.Units(500_000)},
		{Player: 7, Club: 3, Role: matches.Defender, Overall: 70, Value: money.Units(400_000), Listed: true},
		{Player: 9, Club: 3, Role: matches.Defender, Overall: 61, Value: money.Units(100_000), Listed: true}, // no clear improvement
	}
	if c, ok := ChooseTarget(1, cands, 60, budget); !ok || c.Player != 7 {
		t.Fatalf("vacancy chose %+v %v, want the listed 7", c, ok)
	}
	if c, ok := ChooseUpgrade(1, cands, map[matches.Role]int{matches.Defender: 60}, budget); !ok || c.Player != 7 {
		t.Fatalf("upgrade chose %+v %v, want the listed 7", c, ok)
	}
	// With no listed player qualifying, the best unlisted one is chosen.
	if c, ok := ChooseUpgrade(1, cands, map[matches.Role]int{matches.Defender: 65}, budget); !ok || c.Player != 5 {
		t.Fatalf("upgrade chose %+v %v, want 5", c, ok)
	}
	if c, _ := ChooseTarget(1, cands, 66, budget); c.Player != 5 {
		t.Fatalf("vacancy chose %+v, want 5", c)
	}
}

// A star joins only a club at least as strong as his own; other players go
// anywhere.
func TestJoins(t *testing.T) {
	if !Joins(60+StarMargin-1, 60, 50) {
		t.Fatal("a player below the star margin refuses a weaker club")
	}
	if Joins(60+StarMargin, 60, 59) {
		t.Fatal("a star joins a weaker club")
	}
	if !Joins(60+StarMargin, 60, 60) || !Joins(90, 60, 65) {
		t.Fatal("a star refuses a club as strong as his own")
	}
	if AcceptBid(Sale{Fee: 100, Price: 100, Spare: true, Overall: 80, SellerAverage: 60, BuyerAverage: 55}) {
		t.Fatal("a club sells a star to a weaker club")
	}
}
