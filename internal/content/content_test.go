package content

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/players"
)

func TestDefaultIsValid(t *testing.T) {
	d := Default()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.ClubCount != 8 || d.SquadSize() != 20 {
		t.Fatalf("clubs=%d squad=%d, want 8 and 20", d.ClubCount, d.SquadSize())
	}
}

func TestDefaultReturnsIndependentCopies(t *testing.T) {
	a := Default()
	a.Towns[0].Name = "Changed"
	if Default().Towns[0].Name == "Changed" {
		t.Fatal("Default shares slices between calls")
	}
}

func TestValidateRejectsBrokenDefinitions(t *testing.T) {
	cases := map[string]func(*Definitions){
		"too few towns":   func(d *Definitions) { d.Towns = d.Towns[:3] },
		"duplicate short": func(d *Definitions) { d.Towns[1].Short = d.Towns[0].Short },
		"empty names":     func(d *Definitions) { d.FirstNames = nil },
		"bad quota":       func(d *Definitions) { d.Roster[0].Count = 0 },
		"missing profile": func(d *Definitions) { d.Profiles = d.Profiles[1:] },
		"range above max": func(d *Definitions) { d.Profiles[0].Ranges[players.Pace].Max = 101 },
		"inverted range":  func(d *Definitions) { d.Profiles[0].Ranges[players.Pace] = Range{9, 3} },
		"no wage":         func(d *Definitions) { d.Economy.WageReference = 0 },
		"negative gate":   func(d *Definitions) { d.Economy.GatePerHomeMatch = -1 },
		"zero years":      func(d *Definitions) { d.Economy.ContractYears = [2]int{0, 2} },
		"inverted years":  func(d *Definitions) { d.Economy.ContractYears = [2]int{3, 2} },
		"variation 100%":  func(d *Definitions) { d.Economy.WageVariationPct = 100 },
	}
	for name, mutate := range cases {
		d := Default()
		mutate(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("%s: Validate succeeded, want error", name)
		}
	}
}

func TestLeagueValidation(t *testing.T) {
	if err := DefaultLeague().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*League){
		"zero ID":                func(l *League) { l.ID = 0 },
		"one entrant":            func(l *League) { l.Entrants = 1 },
		"no round interval":      func(l *League) { l.RoundInterval = 0 },
		"too many substitutions": func(l *League) { l.MaxSubstitutions = l.MaxBench + 1 },
		"no season interval":     func(l *League) { l.SeasonInterval = 0 },
		"seasons overlap":        func(l *League) { l.SeasonInterval = 13 * l.RoundInterval },
	} {
		l := DefaultLeague()
		mutate(&l)
		if err := l.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	l := DefaultLeague()
	l.SeasonInterval = 13*l.RoundInterval + 1
	if err := l.Validate(); err != nil {
		t.Fatalf("season starting a minute after the last kickoff interval rejected: %v", err)
	}
}

func TestWageFormula(t *testing.T) {
	e := Default().Economy
	for _, c := range []struct {
		overall, variation int
		want               money.Money
	}{
		{60, 0, money.Units(1_600)},
		{60, 20, money.Units(1_920)},
		{60, -20, money.Units(1_280)},
		{30, 0, money.Units(400)},
		{90, 0, money.Units(3_600)},
		{1, -20, money.Units(10)},   // floor
		{59, 0, money.Units(1_550)}, // 1547.1 rounded to 10s
	} {
		if got := e.Wage(c.overall, c.variation); got != c.want {
			t.Errorf("Wage(%d, %d) = %s, want %s", c.overall, c.variation, got, c.want)
		}
	}
}
