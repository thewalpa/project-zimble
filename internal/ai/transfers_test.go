package ai

import (
	"math"
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

func TestAcceptBidAtOrAboveValuation(t *testing.T) {
	if !AcceptBid(100, 100) || !AcceptBid(101, 100) || AcceptBid(99, 100) {
		t.Fatal("bids are not judged against the valuation")
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
