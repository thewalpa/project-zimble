package selection

import (
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// lineup returns a valid 4-4-2 of players base+1..base+11 with a bench of
// base+12..base+14.
func lineup(base ids.PlayerID) Lineup {
	roles := []matches.Role{matches.Goalkeeper,
		matches.Defender, matches.Defender, matches.Defender, matches.Defender,
		matches.Midfielder, matches.Midfielder, matches.Midfielder, matches.Midfielder,
		matches.Forward, matches.Forward}
	l := Lineup{Tactics: matches.Tactics{Mentality: matches.Balanced}}
	for i, r := range roles {
		l.Starters = append(l.Starters, Slot{Player: base + ids.PlayerID(i) + 1, Role: r})
	}
	l.Bench = []ids.PlayerID{base + 12, base + 13, base + 14}
	return l
}

func TestLineupValidateRejectsBadShapes(t *testing.T) {
	if err := lineup(0).Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Lineup){
		"ten starters":       func(l *Lineup) { l.Starters = l.Starters[1:] },
		"twelve starters":    func(l *Lineup) { l.Starters = append(l.Starters, Slot{Player: 99, Role: matches.Forward}) },
		"no goalkeeper":      func(l *Lineup) { l.Starters[0].Role = matches.Defender },
		"two goalkeepers":    func(l *Lineup) { l.Starters[1].Role = matches.Goalkeeper },
		"invalid role":       func(l *Lineup) { l.Starters[3].Role = 9 },
		"zero role":          func(l *Lineup) { l.Starters[3].Role = 0 },
		"zero player":        func(l *Lineup) { l.Starters[4].Player = 0 },
		"duplicate starter":  func(l *Lineup) { l.Starters[4].Player = l.Starters[5].Player },
		"starter on bench":   func(l *Lineup) { l.Bench[0] = l.Starters[2].Player },
		"duplicate bench":    func(l *Lineup) { l.Bench[1] = l.Bench[0] },
		"zero bench player":  func(l *Lineup) { l.Bench[2] = 0 },
		"invalid mentality":  func(l *Lineup) { l.Tactics.Mentality = 7 },
		"mentality not set":  func(l *Lineup) { l.Tactics = matches.Tactics{} },
		"no starters at all": func(l *Lineup) { l.Starters = nil },
	}
	for name, mutate := range cases {
		l := lineup(0)
		mutate(&l)
		if err := l.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Out of position and an empty bench are legal.
	l := lineup(0)
	l.Starters[10].Role = matches.Defender
	l.Bench = nil
	if err := l.Validate(); err != nil {
		t.Fatalf("legal lineup rejected: %v", err)
	}
}

func TestSubmitInsertsReplacesAndKeepsOrder(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []Entry{{Fixture: 9, Team: 2, Lineup: lineup(0)}, {Fixture: 3, Team: 5, Lineup: lineup(20)}, {Fixture: 9, Team: 1, Lineup: lineup(40)}} {
		if err := s.Submit(e); err != nil {
			t.Fatal(err)
		}
	}
	replacement := lineup(60)
	replacement.Tactics.Mentality = matches.Attacking
	if err := s.Submit(Entry{Fixture: 9, Team: 2, Lineup: replacement}); err != nil {
		t.Fatal(err)
	}
	var keys [][2]uint64
	for _, e := range s.Entries() {
		keys = append(keys, [2]uint64{uint64(e.Fixture), uint64(e.Team)})
	}
	if want := [][2]uint64{{3, 5}, {9, 1}, {9, 2}}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	if got, ok := s.Lineup(9, 2); !ok || !got.Equal(replacement) {
		t.Fatalf("Lineup(9, 2) = %+v, %v; want the replacement", got, ok)
	}
	if _, ok := s.Lineup(9, 3); ok {
		t.Fatal("found a lineup that was never submitted")
	}
}

func TestSubmitRejectsWithoutChange(t *testing.T) {
	s, _ := New([]Entry{{Fixture: 1, Team: 1, Lineup: lineup(0)}})
	want := s.Snapshot()
	bad := lineup(0)
	bad.Starters[1].Role = matches.Goalkeeper
	for name, e := range map[string]Entry{
		"invalid lineup": {Fixture: 1, Team: 1, Lineup: bad},
		"zero fixture":   {Fixture: 0, Team: 1, Lineup: lineup(0)},
		"zero team":      {Fixture: 1, Team: 0, Lineup: lineup(0)},
	} {
		if err := s.Submit(e); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if !reflect.DeepEqual(s.Snapshot(), want) {
		t.Fatal("rejected submissions changed the store")
	}
}

func TestNewRejectsInvalidEntries(t *testing.T) {
	bad := lineup(0)
	bad.Bench[0] = bad.Starters[0].Player
	for name, entries := range map[string][]Entry{
		"duplicate key":  {{Fixture: 1, Team: 1, Lineup: lineup(0)}, {Fixture: 1, Team: 1, Lineup: lineup(20)}},
		"invalid lineup": {{Fixture: 1, Team: 1, Lineup: bad}},
		"zero IDs":       {{Lineup: lineup(0)}},
	} {
		if _, err := New(entries); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStoreSharesNoMemory(t *testing.T) {
	input := []Entry{{Fixture: 1, Team: 1, Lineup: lineup(0)}}
	s, err := New(input)
	if err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	input[0].Lineup.Starters[0].Player = 77
	input[0].Lineup.Bench[0] = 77

	out := s.Entries()
	out[0].Lineup.Starters[1].Player = 78
	got, _ := s.Lineup(1, 1)
	got.Bench[1] = 78

	e := Entry{Fixture: 2, Team: 1, Lineup: lineup(20)}
	if err := s.Submit(e); err != nil {
		t.Fatal(err)
	}
	e.Lineup.Starters[2].Player = 79

	if snap := s.Snapshot(); !reflect.DeepEqual(snap[0], want[0]) || snap[1].Lineup.Starters[2].Player == 79 {
		t.Fatal("store shares memory with its callers")
	}
}
