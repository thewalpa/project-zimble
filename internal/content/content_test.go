package content

import (
	"testing"

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
		"range above max": func(d *Definitions) { d.Profiles[0].Ranges[players.Pace].Max = 21 },
		"inverted range":  func(d *Definitions) { d.Profiles[0].Ranges[players.Pace] = Range{9, 3} },
	}
	for name, mutate := range cases {
		d := Default()
		mutate(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("%s: Validate succeeded, want error", name)
		}
	}
}
