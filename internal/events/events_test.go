package events

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func valid() []Event {
	env := func(id ID, k Kind) Event {
		return Event{ID: id, Revision: 3, Sequence: 1, Cause: Cause{Kind: CauseTask, ID: 9}, Kind: k, SchemaVersion: SchemaVersion}
	}
	a := env(1, KindRoundStarted)
	a.RoundStarted = &RoundStarted{Competition: 1, Season: 1, Round: 1, Fixtures: []Pairing{{Fixture: 1, Home: 1, Away: 2}, {Fixture: 2, Home: 3, Away: 4}}}
	b := env(2, KindMatchCompleted)
	b.MatchCompleted = &MatchCompleted{Fixture: 1, Competition: 1, Season: 1, Round: 1, Home: 1, Away: 2, HomeGoals: 2}
	c := env(3, KindLineupSubmitted)
	c.Cause.Kind = CauseCommand
	c.LineupSubmitted = &LineupSubmitted{Fixture: 1, Team: 1}
	d := env(4, KindSeasonEnded)
	d.SeasonEnded = &SeasonEnded{Competition: 1, Season: 1, Ranking: []ids.TeamID{2, 1}}
	e := env(5, KindSeasonStarted)
	e.SeasonStarted = &SeasonStarted{Competition: 1, Season: 2, FirstKickoff: 100, Entrants: []ids.TeamID{1, 2}}
	f := env(6, KindLedgerPosted)
	f.LedgerPosted = &LedgerPosted{Entries: []LedgerEntry{{Entry: 9, Club: 1, Kind: 2, Amount: -500, Balance: 100}, {Entry: 10, Club: 2, Kind: 2, Amount: -700, Balance: -50}}}
	return []Event{a, b, c, d, e, f}
}

func TestValidate(t *testing.T) {
	for _, e := range valid() {
		if err := e.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]func([]Event) Event{
		"zero ID":           func(v []Event) Event { v[0].ID = 0; return v[0] },
		"zero revision":     func(v []Event) Event { v[0].Revision = 0; return v[0] },
		"zero sequence":     func(v []Event) Event { v[0].Sequence = 0; return v[0] },
		"other schema":      func(v []Event) Event { v[0].SchemaVersion = 2; return v[0] },
		"no cause":          func(v []Event) Event { v[0].Cause = Cause{}; return v[0] },
		"cause kind 3":      func(v []Event) Event { v[0].Cause.Kind = 3; return v[0] },
		"kind mismatch":     func(v []Event) Event { v[0].Kind = KindMatchCompleted; return v[0] },
		"no payload":        func(v []Event) Event { v[1].MatchCompleted = nil; return v[1] },
		"two payloads":      func(v []Event) Event { v[1].LineupSubmitted = v[2].LineupSubmitted; return v[1] },
		"unknown kind":      func(v []Event) Event { v[2].Kind = 99; return v[2] },
		"no fixtures":       func(v []Event) Event { v[0].RoundStarted.Fixtures = nil; return v[0] },
		"unordered fixture": func(v []Event) Event { v[0].RoundStarted.Fixtures[1].Fixture = 1; return v[0] },
		"self match":        func(v []Event) Event { v[1].MatchCompleted.Away = 1; return v[1] },
		"zero team":         func(v []Event) Event { v[2].LineupSubmitted.Team = 0; return v[2] },
		"empty ranking":     func(v []Event) Event { v[3].SeasonEnded.Ranking = nil; return v[3] },
		"zero entrant":      func(v []Event) Event { v[4].SeasonStarted.Entrants[0] = 0; return v[4] },
		"no ledger entries": func(v []Event) Event { v[5].LedgerPosted.Entries = nil; return v[5] },
		"entries unordered": func(v []Event) Event { v[5].LedgerPosted.Entries[1].Entry = 9; return v[5] },
		"entry kind zero":   func(v []Event) Event { v[5].LedgerPosted.Entries[0].Kind = 0; return v[5] },
	}
	for name, mutate := range cases {
		if err := mutate(valid()).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCloneSharesNothing(t *testing.T) {
	orig := valid()
	want := valid()
	c := CloneAll(orig)
	c[0].RoundStarted.Fixtures[0].Home = 9
	c[1].MatchCompleted.HomeGoals = 9
	c[2].LineupSubmitted.Team = 9
	c[3].SeasonEnded.Ranking[0] = 9
	c[4].SeasonStarted.Entrants[0] = 9
	c[5].LedgerPosted.Entries[0].Amount = 9
	if !reflect.DeepEqual(orig, want) {
		t.Fatal("clone shares memory with the original")
	}
	if CloneAll(nil) != nil {
		t.Fatal("CloneAll(nil) is not nil")
	}
}
