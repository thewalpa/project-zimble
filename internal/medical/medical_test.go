package medical

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

func store(t *testing.T, conditions ...uint8) *Store {
	t.Helper()
	var recs []Record
	for i, c := range conditions {
		recs = append(recs, Record{Player: ids.PlayerID(i + 1), Condition: c})
	}
	s, err := New(DefaultParams(), recs)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRejectsInvalidState(t *testing.T) {
	for name, recs := range map[string][]Record{
		"zero player":        {{Player: 0, Condition: 1}},
		"duplicate player":   {{Player: 1, Condition: 1}, {Player: 1, Condition: 2}},
		"condition too high": {{Player: 1, Condition: MaxCondition + 1}},
		"below the floor":    {{Player: 1, Condition: DefaultParams().MinCondition - 1}},
	} {
		if _, err := New(DefaultParams(), recs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bad := DefaultParams()
	bad.MinCondition = 0
	if _, err := New(bad, nil); err == nil {
		t.Error("zero MinCondition accepted")
	}
	bad = DefaultParams()
	bad.RecoveryBase = 100*int(MaxCondition) + 1
	if _, err := New(bad, nil); err == nil {
		t.Error("recovery above MaxCondition per day accepted")
	}
}

// Exposure costs more for more minutes and for less stamina, never goes
// below MinCondition, and leaves unexposed players alone.
func TestExposureRules(t *testing.T) {
	p := DefaultParams()
	s := store(t, MaxCondition, MaxCondition, MaxCondition, 25, MaxCondition)
	plan, err := s.PlanExposure([]Exposure{
		{Player: 3, Minutes: 90, Stamina: 90},
		{Player: 1, Minutes: 90, Stamina: 30},
		{Player: 2, Minutes: 30, Stamina: 30},
		{Player: 4, Minutes: 90, Stamina: 30},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	c := func(id ids.PlayerID) int { v, _ := s.Condition(id); return int(v) }
	full := int(MaxCondition)
	switch {
	case c(1) != full-p.Drain(90, 30) || c(3) != full-p.Drain(90, 90):
		t.Fatalf("drain: %d, %d", c(1), c(3))
	case !(c(1) < c(2) && c(2) < full) || !(c(1) < c(3)):
		t.Fatal("more minutes or less stamina must cost more")
	case c(4) != int(p.MinCondition):
		t.Fatalf("floor: %d, want %d", c(4), p.MinCondition)
	case c(5) != full:
		t.Fatal("unexposed player changed")
	}
}

func TestRecoveryRules(t *testing.T) {
	p := DefaultParams()
	s := store(t, 50, 50, MaxCondition-1)
	rest := []Rest{{Player: 3, Stamina: 50}, {Player: 1, Stamina: 30}, {Player: 2, Stamina: 90}}
	plan, err := s.PlanRecovery(rest)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	c := func(id ids.PlayerID) int { v, _ := s.Condition(id); return int(v) }
	if c(1) != 50+p.Recovery(30) || c(2) != 50+p.Recovery(90) || c(1) > c(2) || c(1) == 50 {
		t.Fatalf("recovery %d, %d", c(1), c(2))
	}
	if c(3) != int(MaxCondition) {
		t.Fatalf("recovery not capped: %d", c(3))
	}
}

// A week of rest makes up a weekly full match for the fittest only: with
// DefaultParams a starter of stamina 69 or more holds full condition, and
// the less fit lose more each week the lower their stamina.
func TestWeeklyMatchesTireTheLessFit(t *testing.T) {
	stamina := []uint8{30, 50, 68, 69, 90}
	s := store(t, 100, 100, 100, 100, 100)
	for week := 0; week < 4; week++ {
		var ex []Exposure
		var rest []Rest
		for i, st := range stamina {
			ex = append(ex, Exposure{Player: ids.PlayerID(i + 1), Minutes: 90, Stamina: st})
			rest = append(rest, Rest{Player: ids.PlayerID(i + 1), Stamina: st})
		}
		plan, err := s.PlanExposure(ex, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(plan); err != nil {
			t.Fatal(err)
		}
		for day := 0; day < 7; day++ {
			plan, err := s.PlanRecovery(rest)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(plan); err != nil {
				t.Fatal(err)
			}
		}
	}
	var got []uint8
	for i := range stamina {
		c, _ := s.Condition(ids.PlayerID(i + 1))
		got = append(got, c)
	}
	if got[3] != MaxCondition || got[4] != MaxCondition {
		t.Fatalf("after four weeks of 90 minutes, stamina 69 and 90 are at %d and %d, want full", got[3], got[4])
	}
	if !(got[0] < got[1] && got[1] < got[2] && got[2] < MaxCondition) {
		t.Fatalf("after four weeks of 90 minutes, stamina %v are at %v: want the less fit lower, all below full", stamina[:3], got[:3])
	}
}

func TestPlansValidateAndChangeNothingUntilApplied(t *testing.T) {
	s := store(t, 80, 80)
	want := s.Snapshot()
	for name, ex := range map[string][]Exposure{
		"unknown player": {{Player: 9, Minutes: 90, Stamina: 10}},
		"twice":          {{Player: 1, Minutes: 45, Stamina: 10}, {Player: 1, Minutes: 45, Stamina: 10}},
		"too long":       {{Player: 1, Minutes: MaxMinutes + 1, Stamina: 10}},
		"no stamina":     {{Player: 1, Minutes: 90, Stamina: 0}},
		"stamina 101":    {{Player: 1, Minutes: 90, Stamina: 101}},
	} {
		if _, err := s.PlanExposure(ex, nil); err == nil {
			t.Errorf("exposure %s: accepted", name)
		}
	}
	for name, rest := range map[string][]Rest{
		"missing player": {{Player: 1, Stamina: 10}},
		"extra player":   {{Player: 1, Stamina: 10}, {Player: 2, Stamina: 10}, {Player: 3, Stamina: 10}},
		"wrong player":   {{Player: 1, Stamina: 10}, {Player: 3, Stamina: 10}},
		"bad stamina":    {{Player: 1, Stamina: 10}, {Player: 2, Stamina: 0}},
	} {
		if _, err := s.PlanRecovery(rest); err == nil {
			t.Errorf("recovery %s: accepted", name)
		}
	}
	if _, err := s.PlanExposure([]Exposure{{Player: 1, Minutes: 90, Stamina: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("planning changed the store")
	}
}

func TestStalePlanIsRejected(t *testing.T) {
	s := store(t, 80, 80)
	a, _ := s.PlanExposure([]Exposure{{Player: 1, Minutes: 90, Stamina: 10}}, nil)
	b, _ := s.PlanExposure([]Exposure{{Player: 2, Minutes: 90, Stamina: 10}}, nil)
	if err := s.Apply(a); err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	if err := s.Apply(b); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale plan: err = %v", err)
	}
	if err := s.Apply(a); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("plan applied twice: err = %v", err)
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("rejected plans changed the store")
	}
}

func TestStoreSharesNoMemory(t *testing.T) {
	in := []Record{{Player: 1, Condition: 70}}
	s, _ := New(DefaultParams(), in)
	in[0].Condition = 1
	s.Records()[0].Condition = 2
	plan, _ := s.PlanExposure([]Exposure{{Player: 1, Minutes: 10, Stamina: 50}}, nil)
	plan.Changes()[0].Condition = 3
	if c, _ := s.Condition(1); c != 70 {
		t.Fatalf("condition %d; store shares memory", c)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Condition(1); c != 70-uint8(DefaultParams().Drain(10, 50)) {
		t.Fatalf("condition %d after apply", c)
	}
}

// Rates are finer than a point; each result is rounded half up once.
func TestRoundingOfWholePoints(t *testing.T) {
	p := DefaultParams()
	for _, c := range []struct {
		minutes uint16
		stamina uint8
		want    int
	}{
		{90, 30, 31}, // 90 * 0.346 = 31.14
		{90, 50, 26}, // 90 * 0.29 = 26.1
		{90, 68, 22}, // 90 * 0.2396 = 21.564
		{90, 69, 21}, // 90 * 0.2368 = 21.312
		{50, 100, 8}, // 50 * 0.15 = 7.5, half up
		{1, 100, 0},  // 0.15
		{0, 1, 0},
	} {
		if got := p.Drain(c.minutes, c.stamina); got != c.want {
			t.Errorf("Drain(%d, %d) = %d, want %d", c.minutes, c.stamina, got, c.want)
		}
	}
	for stamina, want := range map[uint8]int{1: 3, 30: 3, 75: 3, 100: 3} {
		if got := p.Recovery(stamina); got != want {
			t.Errorf("Recovery(%d) = %d, want %d", stamina, got, want)
		}
	}
}

// New players join fully fit; departing players' records go; the rest keep
// their condition.
func TestRosterAdmitsAndDischarges(t *testing.T) {
	s := store(t, 60, 70, 80)
	plan, err := s.PlanRoster([]ids.PlayerID{5, 4}, []ids.PlayerID{2})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records()) != 3 {
		t.Fatal("planning changed the store")
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	want := []Record{{Player: 1, Condition: 60}, {Player: 3, Condition: 80}, {Player: 4, Condition: MaxCondition}, {Player: 5, Condition: MaxCondition}}
	if got := s.Records(); !reflect.DeepEqual(got, want) {
		t.Fatalf("records %v, want %v", got, want)
	}
	for name, c := range map[string][2][]ids.PlayerID{
		"admit known":       {{1}, nil},
		"admit zero":        {{0}, nil},
		"admit twice":       {{6, 6}, nil},
		"discharge unknown": {nil, {2}},
		"discharge twice":   {nil, {1, 1}},
	} {
		if _, err := s.PlanRoster(c[0], c[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	stale, _ := s.PlanRoster(nil, []ids.PlayerID{1})
	next, _ := s.PlanRoster([]ids.PlayerID{6}, nil)
	if err := s.Apply(next); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(stale); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale roster plan: %v", err)
	}
}

func TestInjuryRollIsDeterministicAndBounded(t *testing.T) {
	p := DefaultParams()
	var recs []Record
	var ex []Exposure
	for i := 1; i <= 200; i++ {
		recs = append(recs, Record{Player: ids.PlayerID(i), Condition: 60 + uint8(i%40)})
		ex = append(ex, Exposure{Player: ids.PlayerID(i), Minutes: 90, Stamina: 50})
	}
	s, err := New(p, recs)
	if err != nil {
		t.Fatal(err)
	}
	roll := func(seed uint64) []Injury {
		in, err := s.Roll(ex, random.NewStream(seed))
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	if a, b := roll(7), roll(7); !reflect.DeepEqual(a, b) {
		t.Fatal("the same stream rolled different injuries")
	}
	total, hurt := 0, 0
	for seed := uint64(1); seed <= 50; seed++ {
		for _, in := range roll(seed) {
			hurt++
			if in.Days < 1 || int(in.Days) > p.SeriousDays[1] || in.Player == 0 {
				t.Fatalf("injury %+v out of range", in)
			}
		}
		total += len(ex)
	}
	if rate := hurt * 1000 / total; rate < 60 || rate > 180 { // about 5.9% (fit) to 16.7% (condition 60) a match
		t.Fatalf("%d injuries per 1000 matches", rate)
	}
	// The tired are hurt more often than the fit.
	fit, _ := New(p, []Record{{Player: 1, Condition: MaxCondition}})
	tired, _ := New(p, []Record{{Player: 1, Condition: p.MinCondition}})
	count := func(s *Store) int {
		n := 0
		for seed := uint64(1); seed <= 4000; seed++ {
			in, _ := s.Roll([]Exposure{{Player: 1, Minutes: 90, Stamina: 50}}, random.NewStream(seed))
			n += len(in)
		}
		return n
	}
	if f, tr := count(fit), count(tired); tr <= f {
		t.Fatalf("tired player hurt %d times, fit player %d", tr, f)
	}
}

func TestInjuriesKeepPlayersOutUntilTheLastRecoveryDay(t *testing.T) {
	s := store(t, 80, 80, 80)
	ex := []Exposure{{Player: 1, Minutes: 90, Stamina: 50}, {Player: 2, Minutes: 90, Stamina: 50}}
	plan, err := s.PlanExposure(ex, []Injury{{Player: 2, Days: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Injured(); !reflect.DeepEqual(got, []Injury{{Player: 2, Days: 2}}) {
		t.Fatalf("plan injuries %v", got)
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	rest := []Rest{{Player: 1, Stamina: 50}, {Player: 2, Stamina: 50}, {Player: 3, Stamina: 50}}
	for day, wantDays := range []uint16{1, 0} {
		plan, err := s.PlanRecovery(rest)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(plan); err != nil {
			t.Fatal(err)
		}
		d, injured := s.DaysOut(2)
		if d != wantDays || injured != (wantDays > 0) {
			t.Fatalf("day %d: %d days out (%t), want %d", day+1, d, injured, wantDays)
		}
		if got := plan.Recovered(); (len(got) == 1) != (wantDays == 0) {
			t.Fatalf("day %d: recovered %v", day+1, got)
		}
	}
	if _, injured := s.DaysOut(1); injured {
		t.Fatal("an uninjured player is out")
	}
}

func TestInjuryPlansValidate(t *testing.T) {
	s := store(t, 80, 80)
	ex := []Exposure{{Player: 1, Minutes: 90, Stamina: 50}}
	for name, inj := range map[string][]Injury{
		"unknown":      {{Player: 9, Days: 3}},
		"twice":        {{Player: 1, Days: 3}, {Player: 1, Days: 4}},
		"no days":      {{Player: 1}},
		"too long":     {{Player: 1, Days: MaxInjuryDays + 1}},
		"did not play": {{Player: 2, Days: 3}},
	} {
		if _, err := s.PlanExposure(ex, inj); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	plan, _ := s.PlanExposure(ex, []Injury{{Player: 1, Days: 3}})
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanExposure(ex, []Injury{{Player: 1, Days: 3}}); err == nil {
		t.Error("an injured player was injured again")
	}
	if _, err := New(DefaultParams(), []Record{{Player: 1, Condition: 50, DaysOut: MaxInjuryDays + 1}}); err == nil {
		t.Error("a restored layoff beyond the maximum was accepted")
	}
	bad := DefaultParams()
	bad.MinorPermille, bad.ModeratePermille = 700, 400
	if _, err := New(bad, nil); err == nil {
		t.Error("injury classes above 1000 permille accepted")
	}
}
