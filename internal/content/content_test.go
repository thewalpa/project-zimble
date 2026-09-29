package content

import (
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/players"
)

func TestDefaultIsValid(t *testing.T) {
	d := Default()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.ClubCount() != 32 || len(d.Nations) != 2 || d.SquadSize() != 20 {
		t.Fatalf("clubs=%d in %d nations, squad=%d; want 32, 2 and 20", d.ClubCount(), len(d.Nations), d.SquadSize())
	}
}

func TestDefaultReturnsIndependentCopies(t *testing.T) {
	a := Default()
	a.Nations[1].Divisions[1].Towns[0].Name = "Changed"
	if Default().Nations[1].Divisions[1].Towns[0].Name == "Changed" {
		t.Fatal("Default shares slices between calls")
	}
}

func TestValidateRejectsBrokenDefinitions(t *testing.T) {
	cases := map[string]func(*Definitions){
		"too few towns": func(d *Definitions) { d.Nations[0].Divisions[0].Towns = d.Nations[0].Divisions[0].Towns[:3] },
		"duplicate short": func(d *Definitions) {
			d.Nations[0].Divisions[0].Towns[1].Short = d.Nations[0].Divisions[0].Towns[0].Short
		},
		"short across": func(d *Definitions) {
			d.Nations[1].Divisions[0].Towns[0].Short = d.Nations[0].Divisions[0].Towns[0].Short
		},
		"short across tiers": func(d *Definitions) {
			d.Nations[0].Divisions[1].Towns[0].Short = d.Nations[0].Divisions[0].Towns[0].Short
		},
		"town across": func(d *Definitions) {
			d.Nations[1].Divisions[0].Towns[0].Name = d.Nations[0].Divisions[0].Towns[0].Name
		},
		"no divisions":     func(d *Definitions) { d.Nations[1].Divisions = nil },
		"uneven divisions": func(d *Definitions) { d.Nations[1].Divisions = d.Nations[1].Divisions[:1] },
		"no nations":       func(d *Definitions) { d.Nations = nil },
		"no clubs":         func(d *Definitions) { d.Nations[1].Divisions[1].Clubs = 0 },
		"same nation":      func(d *Definitions) { d.Nations[1].Name = d.Nations[0].Name },
		"empty names":      func(d *Definitions) { d.FirstNames = nil },
		"bad quota":        func(d *Definitions) { d.Roster[0].Count = 0 },
		"min above count":  func(d *Definitions) { d.Roster[0].Min = d.Roster[0].Count + 1 },
		"zero min":         func(d *Definitions) { d.Roster[1].Min = 0 },
		"no squad limit":   func(d *Definitions) { d.SquadLimit = 0 },
		"limit below size": func(d *Definitions) { d.SquadLimit = d.SquadSize() - 1 },
		"offer ceiling":    func(d *Definitions) { d.Economy.OfferCeilingPct = 99 },
		"missing profile":  func(d *Definitions) { d.Profiles = d.Profiles[1:] },
		"range above max":  func(d *Definitions) { d.Profiles[0].Ranges[players.Pace].Max = 101 },
		"inverted range":   func(d *Definitions) { d.Profiles[0].Ranges[players.Pace] = Range{9, 3} },
		"no wage":          func(d *Definitions) { d.Economy.WageReference = 0 },
		"negative gate":    func(d *Definitions) { d.Economy.GatePerHomeMatch = -1 },
		"zero years":       func(d *Definitions) { d.Economy.ContractYears = [2]int{0, 2} },
		"inverted years":   func(d *Definitions) { d.Economy.ContractYears = [2]int{3, 2} },
		"variation 100%":   func(d *Definitions) { d.Economy.WageVariationPct = 100 },
		"too young":        func(d *Definitions) { d.Ages[0] = 14 },
		"inverted ages":    func(d *Definitions) { d.Ages = [2]int{30, 20} },
		"retired at start": func(d *Definitions) { d.Ages[1] = players.RetirementAge },
		"youth too old":    func(d *Definitions) { d.Youth.Ages[1] = players.FreeAgentRetirementAge },
		"youth inverted":   func(d *Definitions) { d.Youth.Ages = [2]int{18, 16} },
		"negative gap":     func(d *Definitions) { d.Youth.RatingGap = -1 },
		"youth contract":   func(d *Definitions) { d.Youth.ContractYears = d.Economy.ContractYears[1] + 1 },
		"one-day window":   func(d *Definitions) { d.Transfers.WindowDays = 1 },
		"half-year window": func(d *Definitions) { d.Transfers.WindowDays = 181 },
		"no response time": func(d *Definitions) { d.Transfers.ResponseDays = 0 },
		"response too long": func(d *Definitions) {
			d.Transfers.ResponseDays = d.Transfers.WindowDays + 1
		},
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
		"season too long":        func(l *League) { l.RoundInterval = MaxSeasonSpan/13 + 1 },
		"round beyond a season":  func(l *League) { l.Entrants, l.RoundInterval = 2, MaxSeasonSpan+1 },
		"overflowing interval":   func(l *League) { l.RoundInterval = sim.Duration(1) << 62 },
	} {
		l := DefaultLeague()
		mutate(&l)
		if err := l.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	l := DefaultLeague()
	l.RoundInterval = MaxSeasonSpan / 13
	if err := l.Validate(); err != nil {
		t.Fatalf("season of the longest span rejected: %v", err)
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

func TestYouthRange(t *testing.T) {
	y := Youth{RatingGap: 20}
	for _, c := range []struct{ in, want Range }{
		{Range{43, 90}, Range{23, 70}},
		{Range{11, 37}, Range{1, 17}},
		{Range{1, 11}, Range{1, 1}},
	} {
		if got := y.Range(c.in); got != c.want {
			t.Errorf("Range(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCupValidation(t *testing.T) {
	for _, c := range DefaultCups() {
		if err := c.Validate(); err != nil || c.Entrants() != 8 {
			t.Fatalf("default cup %+v: %v", c, err)
		}
	}
	cases := map[string]func(*Cup){
		"six entrants":    func(c *Cup) { c.Qualifiers[1].Places = 2 },
		"one entrant":     func(c *Cup) { c.Qualifiers = []Qualifier{{League: 1, Places: 1}} },
		"same league":     func(c *Cup) { c.Qualifiers[1].League = 1 },
		"zero league":     func(c *Cup) { c.Qualifiers[0].League = 0 },
		"no places":       func(c *Cup) { c.Qualifiers = append(c.Qualifiers, Qualifier{League: 5}) },
		"no delay":        func(c *Cup) { c.FirstRoundDelay = 0 },
		"no interval":     func(c *Cup) { c.RoundInterval = 0 },
		"no name":         func(c *Cup) { c.Name = "" },
		"subs over bench": func(c *Cup) { c.MaxSubstitutions = c.MaxBench + 1 },
	}
	for name, mutate := range cases {
		c := DefaultCups()[0]
		c.Qualifiers = slices.Clone(c.Qualifiers)
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Seeding interleaves the leagues by position and uses the standard
// bracket: the two champions can meet only in the final.
func TestCupSeeding(t *testing.T) {
	got := DefaultCups()[0].Seeding()
	want := [][2]int{{0, 1}, {1, 4}, {1, 2}, {0, 3}, {1, 1}, {0, 4}, {0, 2}, {1, 3}} // A1 B4, B2 A3, B1 A4, A2 B3
	if !slices.Equal(got, want) {
		t.Fatalf("seeding %v, want %v", got, want)
	}
	four := Cup{Qualifiers: []Qualifier{{League: 1, Places: 2}, {League: 2, Places: 1}, {League: 3, Places: 1}}}
	if got := four.Seeding(); !slices.Equal(got, [][2]int{{0, 1}, {0, 2}, {1, 1}, {2, 1}}) {
		t.Fatalf("four-team seeding %v", got)
	}
}

func TestDefaultLeaguesFitTheDefaultWorld(t *testing.T) {
	leagues := DefaultLeagues()
	entrants := 0
	for _, l := range leagues {
		if err := l.Validate(); err != nil {
			t.Fatal(err)
		}
		entrants += l.Entrants
	}
	if entrants != Default().ClubCount() {
		t.Fatalf("leagues take %d clubs, the world has %d", entrants, Default().ClubCount())
	}
	if err := ValidatePromotions(leagues, DefaultPromotions()); err != nil {
		t.Fatal(err)
	}
	for _, c := range DefaultCups() {
		for _, q := range c.Qualifiers {
			if q.League != 1 && q.League != 2 {
				t.Errorf("cup %d takes places from league %d, want the first divisions only", c.ID, q.League)
			}
		}
	}
}

func TestPromotionValidation(t *testing.T) {
	leagues := DefaultLeagues()
	small := DefaultLeague()
	small.ID, small.Entrants = 6, 6
	leagues = append(leagues, small)
	cases := map[string][]Promotion{
		"missing lower":  {{Upper: 1, Lower: 9, Places: 2}},
		"missing upper":  {{Upper: 9, Lower: 1, Places: 2}},
		"same league":    {{Upper: 1, Lower: 1, Places: 2}},
		"unequal sizes":  {{Upper: 1, Lower: 6, Places: 2}},
		"no places":      {{Upper: 1, Lower: 4}},
		"over half":      {{Upper: 1, Lower: 4, Places: 5}},
		"two lowers":     {{Upper: 1, Lower: 4, Places: 1}, {Upper: 1, Lower: 5, Places: 1}},
		"two uppers":     {{Upper: 1, Lower: 4, Places: 1}, {Upper: 2, Lower: 4, Places: 1}},
		"swapped ends":   {{Upper: 1, Lower: 4, Places: 1}, {Upper: 4, Lower: 1, Places: 1}},
		"three-way loop": {{Upper: 1, Lower: 2, Places: 1}, {Upper: 2, Lower: 4, Places: 1}, {Upper: 4, Lower: 1, Places: 1}},
	}
	for name, links := range cases {
		if err := ValidatePromotions(leagues, links); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	chain := []Promotion{{Upper: 1, Lower: 2, Places: 2}, {Upper: 2, Lower: 4, Places: 2}}
	if err := ValidatePromotions(leagues, chain); err != nil {
		t.Errorf("a chain of divisions rejected: %v", err)
	}
	if err := ValidatePromotions(leagues, nil); err != nil {
		t.Errorf("no links rejected: %v", err)
	}
}
