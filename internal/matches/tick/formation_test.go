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

// commonShapes are the shapes a 4-4-2 lineup takes by moving players
// between lines; the players keep their ratings, as with SetRoles.
var commonShapes = []struct {
	name  string
	roles [matches.StartersPerTeam]matches.Role
}{
	{"4-3-3", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 4}},
	{"3-5-2", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 3, 3, 3, 3, 3, 4, 4}},
	{"5-3-2", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4}},
	{"4-5-1", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 2, 3, 3, 3, 3, 3, 4}},
	{"5-4-1", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 4}},
	{"3-4-3", [matches.StartersPerTeam]matches.Role{1, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4}},
}

// No formation is free: against 4-4-2 between equal career squads, home
// and away, no common shape earns much more than its opponent. In v9 a
// shape that differed from the opponent's left a man free and paid: 4-3-3
// earned +0.75 points a match over an even share, 3-5-2 +0.49, 5-3-2
// +0.40 (v10: +0.19, -0.11, +0.09; the one-forward shapes cost 0.20-0.30).
// The bound is about two standard errors above v10's 4-3-3.
func TestNoFormationIsFree(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 6,000 matches")
	}
	const n = 500
	points := func(wins, losses, matches int) float64 { return float64(3*wins+matches-wins-losses) / float64(matches) }
	for _, sh := range commonShapes {
		shaped := func(home bool) tally {
			return simulate(t, n, func(f ids.FixtureID) *matches.MatchInput {
				in := enginetest.CareerInput(f, 60, 60)
				side := &in.Away
				if home {
					side = &in.Home
				}
				for i := range side.Starters {
					side.Starters[i].Role = sh.roles[i]
				}
				return in
			})
		}
		home, away := shaped(true), shaped(false)
		// Half the gap between the shape's points and 4-4-2's: its edge over
		// an even share.
		edge := (points(home.homeWins, home.awayWins, n) - points(home.awayWins, home.homeWins, n) +
			points(away.awayWins, away.homeWins, n) - points(away.homeWins, away.awayWins, n)) / 4
		t.Logf("%s v 4-4-2: %+.3f points a match over an even share; goals %.2f-%.2f", sh.name, edge,
			float64(home.scored[0]+away.scored[1])/(2*n), float64(home.scored[1]+away.scored[0])/(2*n))
		if edge > 0.3 {
			t.Errorf("%s earns %+.2f points a match over an even share against 4-4-2, want at most +0.30", sh.name, edge)
		}
	}
}
