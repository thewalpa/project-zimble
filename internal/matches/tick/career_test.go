package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

func playCareer(t *testing.T, n, home, away int, hm, am matches.Mentality) tally {
	t.Helper()
	return simulate(t, n, func(f ids.FixtureID) *matches.MatchInput {
		in := enginetest.CareerInput(f, home, away)
		in.Home.Tactics.Mentality, in.Away.Tactics.Mentality = hm, am
		return in
	})
}

// A career's league matches score what real leagues do (2.6–2.9 goals), with
// a home edge, on the specialist squads careers field: career keepers
// outrate their shooters, so the goal level is calibrated on this profile
// and not on the flat one. Bounds are loose: this guards the level the
// calibration set, not its tuning.
func TestCareerGoalLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 1500 matches")
	}
	const n = 1500
	r := playCareer(t, n, 60, 60, matches.Balanced, matches.Balanced)
	goals := float64(r.goals) / n
	t.Logf("career 60 v 60: %v", r)
	if goals < 2.4 || goals > 2.9 {
		t.Errorf("career squads average %.2f goals, want 2.4..2.9", goals)
	}
	if r.homeWins <= r.awayWins || r.matches-r.homeWins-r.awayWins < n/5 || r.matches-r.homeWins-r.awayWins > n/3 {
		t.Errorf("home %d, draw %d, away %d: no home edge or an implausible draw rate", r.homeWins, n-r.homeWins-r.awayWins, r.awayWins)
	}
}
