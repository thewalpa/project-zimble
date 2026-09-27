package registry

import (
	"errors"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

func validInit() Init {
	return Init{
		Clubs:      []Club{{ID: 1, Name: "Alpha FC", ShortName: "ALP"}, {ID: 2, Name: "Beta FC", ShortName: "BET"}},
		Teams:      []Team{{ID: 1, Club: 1, Kind: TeamSenior}, {ID: 2, Club: 2, Kind: TeamSenior}},
		Players:    []Player{{ID: 1, FirstName: "Ann", LastName: "Lee", Born: -9000 * sim.GameInstant(sim.Day)}},
		LastPlayer: 1,
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
		"zero club ID":           func(in *Init) { in.Clubs[0].ID = 0 },
		"duplicate club ID":      func(in *Init) { in.Clubs[1].ID = 1 },
		"duplicate club name":    func(in *Init) { in.Clubs[1].Name = "Alpha FC" },
		"duplicate short name":   func(in *Init) { in.Clubs[1].ShortName = "ALP" },
		"empty club name":        func(in *Init) { in.Clubs[0].Name = "" },
		"zero team ID":           func(in *Init) { in.Teams[0].ID = 0 },
		"duplicate team ID":      func(in *Init) { in.Teams[1].ID = 1 },
		"team of unknown club":   func(in *Init) { in.Teams[1].Club = 9 },
		"invalid team kind":      func(in *Init) { in.Teams[1].Kind = 0 },
		"no senior team":         func(in *Init) { in.Teams = in.Teams[:1] },
		"two senior teams":       func(in *Init) { in.Teams = append(in.Teams, Team{ID: 3, Club: 1, Kind: TeamSenior}) },
		"zero player ID":         func(in *Init) { in.Players[0].ID = 0 },
		"duplicate player ID":    func(in *Init) { in.Players = append(in.Players, in.Players[0]) },
		"empty player name":      func(in *Init) { in.Players[0].LastName = "" },
		"player above allocator": func(in *Init) { in.LastPlayer = 0 },
		"birth out of range":     func(in *Init) { in.Players[0].Born = -sim.MaxInstant - 1 },
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

// New players continue the allocator in order; a snapshot keeps the
// allocator, so IDs are never issued twice.
func TestPlanPlayersContinuesTheAllocator(t *testing.T) {
	in := validInit()
	in.LastPlayer = 5 // IDs 2..5 were issued before
	r, err := New(in)
	if err != nil {
		t.Fatal(err)
	}
	youth := func(id ids.PlayerID) Player { return Player{ID: id, FirstName: "Kit", LastName: "Moss"} }
	for name, add := range map[string][]Player{
		"reused ID":          {youth(1)},
		"skipped ID":         {youth(7)},
		"out of order":       {youth(7), youth(6)},
		"repeated ID":        {youth(6), youth(6)},
		"empty name":         {{ID: 6, FirstName: "Kit"}},
		"birth out of range": {{ID: 6, FirstName: "Kit", LastName: "Moss", Born: sim.MaxInstant + 1}},
	} {
		if _, err := r.PlanPlayers(add); err == nil {
			t.Errorf("%s: plan accepted", name)
		}
	}
	plan, err := r.PlanPlayers([]Player{youth(6), youth(7)})
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := r.PlanPlayers([]Player{youth(6)})
	if len(r.Players()) != 1 || r.LastPlayer() != 5 {
		t.Fatal("planning changed the registry")
	}
	if err := r.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(stale); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale plan: %v", err)
	}
	if p, ok := r.Player(7); !ok || p.LastName != "Moss" || r.LastPlayer() != 7 || len(r.Players()) != 3 {
		t.Fatalf("after apply: %+v %v, last %d", p, ok, r.LastPlayer())
	}
	restored, err := New(r.Snapshot())
	if err != nil || restored.LastPlayer() != 7 {
		t.Fatalf("restore: %v", err)
	}
	if _, err := restored.PlanPlayers([]Player{youth(7)}); err == nil {
		t.Fatal("a restored registry reissued ID 7")
	}
}
