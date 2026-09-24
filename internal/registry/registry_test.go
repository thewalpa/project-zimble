package registry

import "testing"

func validInit() Init {
	return Init{
		Clubs:   []Club{{ID: 1, Name: "Alpha FC", ShortName: "ALP"}, {ID: 2, Name: "Beta FC", ShortName: "BET"}},
		Teams:   []Team{{ID: 1, Club: 1, Kind: TeamSenior}, {ID: 2, Club: 2, Kind: TeamSenior}},
		Players: []Player{{ID: 1, FirstName: "Ann", LastName: "Lee"}},
	}
}

func TestNewAcceptsValidInit(t *testing.T) {
	r, err := New(validInit())
	if err != nil {
		t.Fatal(err)
	}
	if team, ok := r.SeniorTeam(2); !ok || team != 2 {
		t.Fatalf("SeniorTeam(2) = %d, %v", team, ok)
	}
}

func TestNewRejectsInvalidInit(t *testing.T) {
	cases := map[string]func(*Init){
		"zero club ID":         func(in *Init) { in.Clubs[0].ID = 0 },
		"duplicate club ID":    func(in *Init) { in.Clubs[1].ID = 1 },
		"duplicate club name":  func(in *Init) { in.Clubs[1].Name = "Alpha FC" },
		"duplicate short name": func(in *Init) { in.Clubs[1].ShortName = "ALP" },
		"empty club name":      func(in *Init) { in.Clubs[0].Name = "" },
		"zero team ID":         func(in *Init) { in.Teams[0].ID = 0 },
		"duplicate team ID":    func(in *Init) { in.Teams[1].ID = 1 },
		"team of unknown club": func(in *Init) { in.Teams[1].Club = 9 },
		"invalid team kind":    func(in *Init) { in.Teams[1].Kind = 0 },
		"no senior team":       func(in *Init) { in.Teams = in.Teams[:1] },
		"two senior teams":     func(in *Init) { in.Teams = append(in.Teams, Team{ID: 3, Club: 1, Kind: TeamSenior}) },
		"zero player ID":       func(in *Init) { in.Players[0].ID = 0 },
		"duplicate player ID":  func(in *Init) { in.Players = append(in.Players, in.Players[0]) },
		"empty player name":    func(in *Init) { in.Players[0].LastName = "" },
	}
	for name, mutate := range cases {
		in := validInit()
		mutate(&in)
		if _, err := New(in); err == nil {
			t.Errorf("%s: New succeeded, want error", name)
		}
	}
}

func TestQueriesReturnCopies(t *testing.T) {
	in := validInit()
	r, err := New(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Clubs[0].Name = "Changed"
	clubs := r.Clubs()
	clubs[0].Name = "Changed"
	if c, _ := r.Club(1); c.Name != "Alpha FC" {
		t.Fatalf("registry mutated through caller slice: %q", c.Name)
	}
}
