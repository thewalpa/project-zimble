package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

// goals returns the home side's goals for and against over n career-squad
// matches, with roles applied to it before kickoff.
func goals(t *testing.T, n int, roles [matches.StartersPerTeam]matches.Role) (for_, against int) {
	t.Helper()
	e := engine(t)
	for f := ids.FixtureID(1); f <= ids.FixtureID(n); f++ {
		s := enginetest.Start(t, e, enginetest.CareerInput(f, 60, 60))
		if roles != enginetest.StartingRoles {
			if err := s.Apply(enginetest.SetRoles(matches.Home, roles)); err != nil {
				t.Fatal(err)
			}
		}
		out := playOut(t, s.(*session), false).Outcome
		for_, against = for_+int(out.Score[0]), against+int(out.Score[1])
	}
	return for_, against
}

// Roles count: a side whose ten outfield players all play as defenders sits
// deep and rarely scores, and one that plays them all as forwards leaves its
// goal open. Both are far from the natural shape's goals.
func TestRolesChangePlay(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 90 career matches")
	}
	const n = 30
	nf, na := goals(t, n, enginetest.StartingRoles)
	df, da := goals(t, n, [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2})
	ff, fa := goals(t, n, [matches.StartersPerTeam]matches.Role{1, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4})
	t.Logf("%d matches: natural %d-%d, all defenders %d-%d, all forwards %d-%d", n, nf, na, df, da, ff, fa)
	if df*2 > nf {
		t.Errorf("all defenders scored %d, natural %d: they should rarely score", df, nf)
	}
	if fa < 2*na {
		t.Errorf("all forwards conceded %d, natural %d: they should leave the goal open", fa, na)
	}
}
