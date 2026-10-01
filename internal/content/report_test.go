package content

import (
	"bytes"
	"strings"
	"testing"
)

func TestDescribeDefaultSetHasNoProblems(t *testing.T) {
	r := Describe(DefaultSet())
	if len(r.Problems) != 0 {
		t.Fatalf("problems in the default set: %v", r.Problems)
	}
	if r.Clubs != 32 || r.SquadSize != 20 || len(r.Nations) != 2 || len(r.Leagues) != 4 || len(r.Cups) != 1 {
		t.Fatalf("report %+v does not describe the default set", r)
	}
	for _, l := range r.Leagues {
		if l.Tier == 0 {
			t.Fatalf("league %d matches no division", l.ID)
		}
	}
	if c := r.Cups[0]; c.Entrants != 8 || c.Rounds != 3 {
		t.Fatalf("cup %+v, want 8 entrants in 3 rounds", c)
	}
	for _, q := range r.Roster {
		if q.Overall[0] > q.Overall[1] || q.Overall[1] > q.Overall[2] || q.Youth[1] > q.Overall[1] {
			t.Fatalf("%s overalls %v / youth %v are not ordered", q.Position, q.Overall, q.Youth)
		}
	}
}

func TestDescribeFindsProblems(t *testing.T) {
	cases := map[string]func(*Set){
		"fewer entrants than clubs": func(s *Set) { s.Leagues = s.Leagues[:3] },
		"league off the division blocks": func(s *Set) {
			s.Leagues[0].Entrants, s.Leagues[1].Entrants = 6, 10
		},
		"duplicate league": func(s *Set) { s.Leagues[1].ID = s.Leagues[0].ID },
		"cup takes too many places": func(s *Set) {
			s.Cups[0].Qualifiers = []Qualifier{{League: 1, Places: 9}, {League: 2, Places: 7}}
		},
		"cup reuses a league ID":        func(s *Set) { s.Cups[0].ID = s.Leagues[0].ID },
		"promotion to a missing league": func(s *Set) { s.Promotions[0].Lower = 99 },
		"broken definitions":            func(s *Set) { s.Definitions.SquadLimit = 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := DefaultSet()
			mutate(&s)
			if len(Describe(s).Problems) == 0 {
				t.Fatal("no problem reported")
			}
		})
	}
}

func TestDescribeDoesNotChangeTheSet(t *testing.T) {
	s := DefaultSet()
	s.Leagues[0], s.Leagues[3] = s.Leagues[3], s.Leagues[0] // unsorted input
	before := s.Leagues[0]
	if p := Describe(s).Problems; len(p) != 0 {
		t.Fatalf("league order changed the result: %v", p)
	}
	if s.Leagues[0] != before {
		t.Fatal("Describe reordered the caller's leagues")
	}
}

func TestReportWriteIsDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := Describe(DefaultSet()).Write(&a); err != nil {
		t.Fatal(err)
	}
	if err := Describe(DefaultSet()).Write(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() || !strings.Contains(a.String(), "no problems") {
		t.Fatalf("report differs between runs or reports problems:\n%s", a.String())
	}
}
