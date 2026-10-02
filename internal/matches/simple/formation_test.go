package simple

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

// goalDifference is the home side's mean goals for minus against over n
// career-squad matches (paired by fixture), with roles applied to the home
// side before kickoff when they differ from its starting ones.
func goalDifference(t *testing.T, n int, roles [matches.StartersPerTeam]matches.Role) float64 {
	t.Helper()
	e := engine(t)
	total := 0
	for f := ids.FixtureID(1); f <= ids.FixtureID(n); f++ {
		s, err := e.Start(enginetest.CareerInput(f, 60, 60), rs(f))
		if err != nil {
			t.Fatal(err)
		}
		if roles != enginetest.StartingRoles {
			if err := s.Apply(enginetest.SetRoles(matches.Home, roles)); err != nil {
				t.Fatal(err)
			}
		}
		var dst matches.MatchStepResult
		advance(t, s, 90, &dst)
		advance(t, s, 90, &dst)
		total += int(dst.Outcome.Score[0]) - int(dst.Outcome.Score[1])
	}
	return float64(total) / float64(n)
}

// Roles count: on the specialist squads of a career, playing everyone in
// goal-scoring or in defending roles, against their trade, is worse than
// the natural shape.
func TestOutOfPositionRolesCostGoals(t *testing.T) {
	const n = 2000
	natural := goalDifference(t, n, enginetest.StartingRoles)
	for name, roles := range map[string][matches.StartersPerTeam]matches.Role{
		"everyone forward":  {1, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4},
		"everyone defender": {1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	} {
		got := goalDifference(t, n, roles)
		t.Logf("natural %+.2f goals a match, %s %+.2f", natural, name, got)
		if natural-got < 0.1 {
			t.Errorf("%s: goal difference %+.2f, natural %+.2f: the roles do not count", name, got, natural)
		}
	}
}
