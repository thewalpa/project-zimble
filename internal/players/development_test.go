package players

import (
	"errors"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
)

func TestPlanDevelopsRetiresAndAdds(t *testing.T) {
	s, err := New([]Profile{validProfile(1), validProfile(2), validProfile(3)})
	if err != nil {
		t.Fatal(err)
	}
	grown := Attributes{30, 55, 80, 45, 65, 100}
	plan, err := s.Plan(Changes{
		Developments: []Development{{Player: 1, Attributes: grown}},
		Retirements:  []ids.PlayerID{2},
		Additions:    []Profile{validProfile(4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := s.Plan(Changes{Retirements: []ids.PlayerID{3}})
	if p, _ := s.Profile(1); p.Attributes == grown || len(s.PlayerIDs()) != 3 {
		t.Fatal("planning changed the store")
	}
	if err := s.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(stale); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale plan: %v", err)
	}
	if p, _ := s.Profile(1); p.Attributes != grown || p.Retired {
		t.Fatalf("player 1: %+v", p)
	}
	if p, _ := s.Profile(2); !p.Retired || p.Attributes != validProfile(2).Attributes {
		t.Fatalf("player 2 should be retired unchanged: %+v", p)
	}
	if p, ok := s.Profile(4); !ok || p.Retired {
		t.Fatalf("player 4 was not added: %+v", p)
	}
	restored, err := New(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := restored.Profile(2); !p.Retired {
		t.Fatal("retirement did not survive a snapshot")
	}
}

func TestPlanRejectsInvalidChanges(t *testing.T) {
	retired := validProfile(3)
	retired.Retired = true
	s, err := New([]Profile{validProfile(1), validProfile(2), retired})
	if err != nil {
		t.Fatal(err)
	}
	newRetired := validProfile(5)
	newRetired.Retired = true
	cases := map[string]Changes{
		"develop unknown":      {Developments: []Development{{Player: 9, Attributes: validProfile(1).Attributes}}},
		"develop retired":      {Developments: []Development{{Player: 3, Attributes: validProfile(1).Attributes}}},
		"develop out of range": {Developments: []Development{{Player: 1, Attributes: Attributes{0, 1, 1, 1, 1, 1}}}},
		"develop twice":        {Developments: []Development{{Player: 1, Attributes: validProfile(1).Attributes}, {Player: 1, Attributes: validProfile(1).Attributes}}},
		"retire retired":       {Retirements: []ids.PlayerID{3}},
		"retire unknown":       {Retirements: []ids.PlayerID{9}},
		"develop and retire":   {Developments: []Development{{Player: 1, Attributes: validProfile(1).Attributes}}, Retirements: []ids.PlayerID{1}},
		"add existing ID":      {Additions: []Profile{validProfile(2)}},
		"add twice":            {Additions: []Profile{validProfile(5), validProfile(5)}},
		"add retired":          {Additions: []Profile{newRetired}},
		"add invalid":          {Additions: []Profile{validProfile(0)}},
	}
	before := s.Snapshot()
	for name, c := range cases {
		if _, err := s.Plan(c); err == nil {
			t.Errorf("%s: plan accepted", name)
		}
	}
	if after := s.Snapshot(); len(after) != len(before) {
		t.Fatal("a rejected plan changed the store")
	}
}

// Development is a pure function of (seed, player, year, age, attributes):
// repeatable, independent of other players, and within the rating scale.
func TestDevelopIsDeterministicAndBounded(t *testing.T) {
	p := validProfile(7)
	a, b := Develop(42, p, 20, 2026), Develop(42, p, 20, 2026)
	if a != b {
		t.Fatal("same inputs developed differently")
	}
	differ := 0
	for year := 2026; year < 2036; year++ {
		if Develop(42, p, 20, year) != a {
			differ++
		}
	}
	if Develop(43, p, 20, 2026) != a {
		differ++
	}
	if differ < 9 {
		t.Fatalf("years and seeds barely change development (%d of 11 differ)", differ)
	}
	extremes := Profile{Player: 8, Position: Forward, Attributes: Attributes{1, 1, 100, 100, 1, 100}}
	for year := 2026; year < 2126; year++ {
		for _, age := range []int{16, 40} {
			for _, r := range Develop(1, extremes, age, year) {
				if !r.Valid() {
					t.Fatalf("age %d year %d: rating %d outside the scale", age, year, r)
				}
			}
		}
	}
}

// On average the young improve, players in their prime hold level and the
// old decline, with physical attributes declining first.
func TestDevelopmentFollowsAge(t *testing.T) {
	mean := func(age int, a Attribute) float64 {
		total := 0
		const n = 2000
		for id := range n {
			p := Profile{Player: ids.PlayerID(id + 1), Position: Midfielder, Attributes: Attributes{50, 50, 50, 50, 50, 50}}
			total += int(Develop(random.Seed(9), p, age, 2030)[a]) - 50
		}
		return float64(total) / n
	}
	for _, c := range []struct {
		age    int
		lo, hi float64
	}{{17, 3.5, 4.5}, {21, 1.5, 2.5}, {26, -0.5, 0.5}, {34, -3.5, -2.5}} {
		if m := mean(c.age, Passing); m < c.lo || m > c.hi {
			t.Errorf("age %d: mean passing change %.2f, want %.1f..%.1f", c.age, m, c.lo, c.hi)
		}
	}
	if p, s := mean(30, Passing), mean(30, Pace); s >= p-0.5 {
		t.Errorf("at 30 pace changes by %.2f, passing by %.2f; pace should decline faster", s, p)
	}
}

func TestRetirement(t *testing.T) {
	for age := 16; age < RetirementAge; age++ {
		for id := ids.PlayerID(1); id <= 50; id++ {
			if RetirementPermille(age) == 0 && Retires(42, id, age, 2030, true) {
				t.Fatalf("player %d retired at %d with a club", id, age)
			}
		}
	}
	for id := ids.PlayerID(1); id <= 50; id++ {
		if !Retires(42, id, RetirementAge, 2030, true) || !Retires(42, id, FreeAgentRetirementAge, 2030, false) {
			t.Fatalf("player %d did not retire at the retirement ages", id)
		}
		if Retires(42, id, FreeAgentRetirementAge-1, 2030, false) {
			t.Fatalf("player %d retired as a young free agent", id)
		}
		if Retires(42, id, 35, 2030, true) != Retires(42, id, 35, 2030, true) {
			t.Fatal("retirement is not repeatable")
		}
	}
	for age := 33; age < RetirementAge; age++ {
		n := 0
		const players = 4000
		for id := ids.PlayerID(1); id <= players; id++ {
			if Retires(7, id, age, 2031, true) {
				n++
			}
		}
		want := RetirementPermille(age) * players / 1000
		if n < want-want/5-20 || n > want+want/5+20 {
			t.Errorf("age %d: %d of %d retired, want about %d", age, n, players, want)
		}
	}
}
