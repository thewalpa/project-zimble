package ai

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
)

var rules = matches.Rules{MaxSubstitutions: 3, MaxBench: 7}

func rating(v uint8) matches.Ratings {
	return matches.Ratings{Goalkeeping: v, Defending: v, Passing: v, Finishing: v, Pace: v, Stamina: v}
}

// squad builds 3 GK, 7 DF, 6 MF, 4 FW with ratings varying by ID.
func squad() []Candidate {
	var out []Candidate
	id := ids.PlayerID(1)
	for _, g := range []struct {
		role matches.Role
		n    int
	}{{matches.Goalkeeper, 3}, {matches.Defender, 7}, {matches.Midfielder, 6}, {matches.Forward, 4}} {
		for range g.n {
			out = append(out, Candidate{Player: id, Natural: g.role, Ratings: rating(uint8(5 * (5 + (id*7)%13))), Condition: matches.MaxCondition})
			id++
		}
	}
	return out
}

func mustSelect(t *testing.T, c []Candidate) matches.TeamInput {
	t.Helper()
	in, err := SelectTeam(9, c, rules)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestSelectionIsLegalAndValidates(t *testing.T) {
	sel := mustSelect(t, squad())
	mi := matches.MatchInput{Match: 1, Home: sel, Away: mustSelect(t, shift(squad(), 100)), Rules: rules}
	mi.Away.Team = 10
	if err := mi.Validate(7); err != nil {
		t.Fatalf("selected lineup rejected by the match contract: %v", err)
	}
	roles := map[matches.Role]int{}
	for _, p := range sel.Starters {
		roles[p.Role]++
	}
	if roles[matches.Goalkeeper] != 1 || roles[matches.Defender] != 4 || roles[matches.Midfielder] != 4 || roles[matches.Forward] != 2 {
		t.Fatalf("starting roles %v, want 4-4-2", roles)
	}
	if len(sel.Bench) != 7 || sel.Bench[0].Role != matches.Goalkeeper {
		t.Fatalf("bench %+v: want 7 players, goalkeeper first", sel.Bench)
	}
	if sel.Tactics.Mentality != matches.Balanced {
		t.Fatal("mentality not balanced")
	}
}

func shift(c []Candidate, by ids.PlayerID) []Candidate {
	for i := range c {
		c[i].Player += by
	}
	return c
}

func TestSelectionPicksBestNaturalPlayers(t *testing.T) {
	c := squad()
	sel := mustSelect(t, c)
	for _, p := range sel.Starters {
		for _, other := range c {
			if other.Natural != p.Role || other.Player == p.Player || inStarters(sel, other.Player) {
				continue
			}
			if RoleScore(other.Ratings, p.Role) > RoleScore(p.Ratings, p.Role) {
				t.Fatalf("benched %d (%d) is better than starter %d (%d) at role %d",
					other.Player, RoleScore(other.Ratings, p.Role), p.Player, RoleScore(p.Ratings, p.Role), p.Role)
			}
		}
	}
}

func inStarters(sel matches.TeamInput, id ids.PlayerID) bool {
	for _, p := range sel.Starters {
		if p.Player == id {
			return true
		}
	}
	return false
}

func TestSelectionIgnoresInputOrderAndDoesNotModifyIt(t *testing.T) {
	c := squad()
	want := mustSelect(t, c)
	reversed := slices.Clone(c)
	slices.Reverse(reversed)
	orig := slices.Clone(reversed)
	if got := mustSelect(t, reversed); !reflect.DeepEqual(got, want) {
		t.Fatal("candidate order changed the selection")
	}
	if !reflect.DeepEqual(reversed, orig) {
		t.Fatal("SelectTeam modified its input")
	}
}

func TestTiesBreakByLowerPlayerID(t *testing.T) {
	c := squad()
	for i := range c {
		c[i].Ratings = rating(50)
	}
	sel := mustSelect(t, c)
	if sel.Starters[0].Player != 1 || sel.Starters[1].Player != 4 || sel.Starters[5].Player != 11 || sel.Starters[9].Player != 17 {
		t.Fatalf("equal ratings did not pick lowest IDs: %+v", sel.Starters)
	}
}

func TestSelectionFillsOutfieldGaps(t *testing.T) {
	var c []Candidate
	for _, x := range squad() {
		if x.Natural != matches.Forward {
			c = append(c, x) // no forwards at all
		}
	}
	sel := mustSelect(t, c)
	if sel.Starters[9].Role != matches.Forward || sel.Starters[10].Role != matches.Forward {
		t.Fatalf("forward slots not filled: %+v", sel.Starters[9:])
	}
}

func TestSelectionRejectsImpossibleSquads(t *testing.T) {
	noKeeper := slices.DeleteFunc(squad(), func(c Candidate) bool { return c.Natural == matches.Goalkeeper })
	dup := squad()
	dup[5].Player = dup[4].Player
	cases := map[string][]Candidate{
		"no goalkeeper": noKeeper,
		"ten players":   squad()[:10],
		"duplicate ID":  dup,
		"zero ID":       append(squad(), Candidate{Player: 0, Natural: matches.Defender, Ratings: rating(25), Condition: matches.MaxCondition}),
		"invalid role":  append(squad(), Candidate{Player: 99, Natural: 0, Ratings: rating(25), Condition: matches.MaxCondition}),
	}
	for name, c := range cases {
		if _, err := SelectTeam(9, c, rules); !errors.Is(err, ErrNoLegalLineup) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := SelectTeam(0, squad(), rules); err == nil {
		t.Error("zero team accepted")
	}
}

func TestSmallBenchRules(t *testing.T) {
	sel, err := SelectTeam(9, squad(), matches.Rules{MaxSubstitutions: 0, MaxBench: 0})
	if err != nil || len(sel.Bench) != 0 {
		t.Fatalf("bench %v, err %v", sel.Bench, err)
	}
	// One goalkeeper and 11 outfield players: one spare, and no keeper for
	// the bench.
	sel, err = SelectTeam(9, squad()[2:14], rules)
	if err != nil || len(sel.Bench) != 1 || sel.Bench[0].Role == matches.Goalkeeper {
		t.Fatalf("bench %v, err %v; want the single spare outfield player", sel.Bench, err)
	}
}

// A tired player gives way to a fresher one of similar ability, but not to
// a much weaker one; condition also travels into the match input.
func TestSelectionWeighsCondition(t *testing.T) {
	c := squad()
	fresh := mustSelect(t, c)
	star := fresh.Starters[0].Player // the chosen goalkeeper
	var backup Candidate
	for _, x := range c {
		if x.Natural == matches.Goalkeeper && x.Player != star && RoleScore(x.Ratings, matches.Goalkeeper) > RoleScore(backup.Ratings, matches.Goalkeeper) {
			backup = x
		}
	}
	var starScore int
	for _, x := range c {
		if x.Player == star {
			starScore = RoleScore(x.Ratings, matches.Goalkeeper)
		}
	}
	backupScore := RoleScore(backup.Ratings, matches.Goalkeeper)
	if backupScore >= starScore || backupScore == 0 {
		t.Fatalf("setup: star %d, backup %d", starScore, backupScore)
	}
	// Just tired enough that the backup's fit score is higher.
	condition := uint8(backupScore*int(matches.MaxCondition)/starScore - 1)
	for i := range c {
		if c[i].Player == star {
			c[i].Condition = condition
		}
	}
	tired := mustSelect(t, c)
	if tired.Starters[0].Player != backup.Player {
		t.Fatalf("goalkeeper %d at condition %d still starts over %d", star, condition, backup.Player)
	}
	if tired.Bench[0].Player != star || tired.Bench[0].Condition != condition {
		t.Fatalf("tired goalkeeper not first on the bench with their condition: %+v", tired.Bench[0])
	}
	// A little tiredness is not enough.
	for i := range c {
		if c[i].Player == star {
			c[i].Condition = matches.MaxCondition - 1
		}
	}
	if mustSelect(t, c).Starters[0].Player != star {
		t.Fatal("a barely tired better goalkeeper was dropped")
	}
	for i := range c {
		if c[i].Player == star {
			c[i].Condition = 0
		}
	}
	if _, err := SelectTeam(1, c, matches.Rules{MaxSubstitutions: 3, MaxBench: 7}); err == nil {
		t.Fatal("zero condition accepted")
	}
}
