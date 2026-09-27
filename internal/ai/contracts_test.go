package ai

import (
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

func TestRenewKeepsPlayersNearTheSquadAverage(t *testing.T) {
	if !Renew(60, 60, 28) || !Renew(57, 60, 28) || Renew(56, 60, 28) || !Renew(80, 60, 35) || Renew(56, 60, PromiseAge) {
		t.Fatal("renewal threshold is not average minus the margin")
	}
	// Each year short of PromiseAge buys PromisePerYear points.
	if !Renew(55, 60, PromiseAge-1) || Renew(54, 60, PromiseAge-1) || !Renew(43, 60, 17) || Renew(42, 60, 17) {
		t.Fatal("young players' margin is not widened by their promise")
	}
}

func TestContractLengthIsDeterministicAndInRange(t *testing.T) {
	seen := map[int]bool{}
	for p := ids.PlayerID(1); p <= 200; p++ {
		n := ContractLength(42, p, 2026, 1, 4)
		if n < 1 || n > 4 || n != ContractLength(42, p, 2026, 1, 4) {
			t.Fatalf("player %d: length %d", p, n)
		}
		seen[n] = true
	}
	if len(seen) != 4 {
		t.Fatalf("lengths drawn: %v", seen)
	}
	differs := false
	for p := ids.PlayerID(1); p <= 20; p++ {
		differs = differs || ContractLength(42, p, 2026, 1, 4) != ContractLength(42, p, 2027, 1, 4)
	}
	if !differs {
		t.Fatal("the start year does not change the draw")
	}
}

func TestSigningsFillNeedsFairly(t *testing.T) {
	clubs := []ClubNeeds{
		{Club: 2, Average: 60, Needs: []RoleCount{{matches.Forward, 2}}},
		{Club: 1, Average: 55, Needs: []RoleCount{{matches.Forward, 1}, {matches.Goalkeeper, 1}}},
		{Club: 3, Average: 55, Needs: nil},
	}
	pool := []FreeAgent{
		{Player: 10, Role: matches.Forward, Overall: 70},
		{Player: 11, Role: matches.Forward, Overall: 75},
		{Player: 12, Role: matches.Forward, Overall: 70},
		{Player: 13, Role: matches.Defender, Overall: 90},
		{Player: 14, Role: matches.Goalkeeper, Overall: 50},
	}
	got, err := Signings(clubs, pool)
	if err != nil {
		t.Fatal(err)
	}
	// Club 1 (weaker) picks first: its needs tie at 1, so role order gives
	// the goalkeeper; club 2 takes the best forward. Round 2: club 1's
	// forward (70, lower ID), club 2's last forward.
	want := []Signing{{1, 14}, {2, 11}, {1, 10}, {2, 12}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("signings %v, want %v", got, want)
	}
	// Input order is irrelevant and the inputs are not modified.
	before := slices.Clone(pool)
	slices.Reverse(clubs)
	slices.Reverse(pool)
	again, _ := Signings(clubs, pool)
	slices.Reverse(pool)
	if !reflect.DeepEqual(again, want) || !reflect.DeepEqual(pool, before) {
		t.Fatal("signings depend on input order or modified the pool")
	}
}

// A club takes back a player it released only when no one else fits.
func TestSigningsPreferOtherClubsReleases(t *testing.T) {
	pool := []FreeAgent{
		{Player: 1, Role: matches.Defender, Overall: 80, ReleasedBy: 1},
		{Player: 2, Role: matches.Defender, Overall: 60, ReleasedBy: 2},
		{Player: 3, Role: matches.Goalkeeper, Overall: 50, ReleasedBy: 3},
	}
	got, err := Signings([]ClubNeeds{
		{Club: 1, Average: 0, Needs: []RoleCount{{matches.Defender, 1}}},
		{Club: 2, Average: 1, Needs: []RoleCount{{matches.Defender, 1}}},
		{Club: 3, Average: 2, Needs: []RoleCount{{matches.Goalkeeper, 1}}},
	}, pool)
	// Club 1 picks first but passes over its own 80 for club 2's 60; club 2
	// takes club 1's 80; club 3's only goalkeeper is its own, so it takes
	// them back rather than leave the need open.
	if want := []Signing{{1, 2}, {2, 1}, {3, 3}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("signings %v, %v; want %v", got, err, want)
	}
}

func TestSigningsStopWhenThePoolRunsOut(t *testing.T) {
	got, err := Signings(
		[]ClubNeeds{{Club: 1, Needs: []RoleCount{{matches.Goalkeeper, 2}, {matches.Defender, 1}}}},
		[]FreeAgent{{Player: 5, Role: matches.Goalkeeper, Overall: 40}},
	)
	if err != nil || !reflect.DeepEqual(got, []Signing{{1, 5}}) {
		t.Fatalf("signings %v, %v", got, err)
	}
}

func TestSigningsRejectInvalidInput(t *testing.T) {
	agent := []FreeAgent{{Player: 5, Role: matches.Goalkeeper, Overall: 40}}
	for name, c := range map[string]struct {
		clubs []ClubNeeds
		pool  []FreeAgent
	}{
		"zero club":       {[]ClubNeeds{{Club: 0}}, agent},
		"club twice":      {[]ClubNeeds{{Club: 1}, {Club: 1}}, agent},
		"negative need":   {[]ClubNeeds{{Club: 1, Needs: []RoleCount{{matches.Forward, -1}}}}, agent},
		"invalid role":    {[]ClubNeeds{{Club: 1, Needs: []RoleCount{{0, 1}}}}, agent},
		"agent twice":     {[]ClubNeeds{{Club: 1}}, append(slices.Clone(agent), agent...)},
		"agent zero role": {[]ClubNeeds{{Club: 1}}, []FreeAgent{{Player: 5}}},
	} {
		if _, err := Signings(c.clubs, c.pool); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
