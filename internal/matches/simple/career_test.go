package simple

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

// careerRun is a seeded batch on the specialist squads a career fields.
type careerRun struct {
	n, goals, home, draw, away int
}

// points returns the points per match of the home and the away side.
func (r careerRun) points() (home, away float64) {
	return float64(3*r.home+r.draw) / float64(r.n), float64(3*r.away+r.draw) / float64(r.n)
}

func playCareer(t *testing.T, home, away int, hm, am matches.Mentality) careerRun {
	t.Helper()
	const n = 6000
	var r careerRun
	var dst matches.MatchStepResult
	e := engine(t)
	for f := ids.FixtureID(1); f <= n; f++ {
		in := enginetest.CareerInput(f, home, away)
		in.Home.Tactics.Mentality, in.Away.Tactics.Mentality = hm, am
		s, err := e.Start(in, rs(f))
		if err != nil {
			t.Fatal(err)
		}
		advance(t, s, 90, &dst)
		advance(t, s, 90, &dst)
		h, a := int(dst.Outcome.Score[0]), int(dst.Outcome.Score[1])
		r.n++
		r.goals += h + a
		switch {
		case h > a:
			r.home++
		case a > h:
			r.away++
		default:
			r.draw++
		}
	}
	return r
}

// A career's league matches score what real leagues do (2.6–2.9 goals), with
// the real home edge, on the squads careers field. Bounds are loose: this
// guards the level the career calibration set, not its tuning.
func TestCareerGoalLevel(t *testing.T) {
	r := playCareer(t, 60, 60, matches.Balanced, matches.Balanced)
	goals := float64(r.goals) / float64(r.n)
	t.Logf("career 60 v 60: %.2f goals, home %d, draw %d, away %d of %d", goals, r.home, r.draw, r.away, r.n)
	if goals < 2.4 || goals > 2.9 {
		t.Errorf("career squads average %.2f goals, want 2.4..2.9", goals)
	}
	if r.home <= r.away || r.draw < r.n/5 || r.draw > r.n/3 {
		t.Errorf("home %d, draw %d, away %d: no home edge or an implausible draw rate", r.home, r.draw, r.n-r.home-r.draw)
	}
}

// Mentality is a trade-off in the career default too: attacking pays about
// nothing at equal teams and defensive pays the underdog, so the manager
// chooses a mentality by the match rather than always attacking. Points
// differences of a few hundredths are sampling noise.
func TestMentalityIsATradeOff(t *testing.T) {
	const slack = 0.08
	for _, c := range []struct {
		name       string
		home, away int
	}{{"equal", 60, 60}, {"home underdog", 55, 65}, {"home favourite", 65, 55}} {
		base := playCareer(t, c.home, c.away, matches.Balanced, matches.Balanced)
		baseHome, baseAway := base.points()
		att := playCareer(t, c.home, c.away, matches.Attacking, matches.Balanced)
		def := playCareer(t, c.home, c.away, matches.Defensive, matches.Balanced)
		attPts, _ := att.points()
		defPts, _ := def.points()
		t.Logf("%s, home side: attacking %+.3f, defensive %+.3f points a match (balanced %.3f, away %.3f)",
			c.name, attPts-baseHome, defPts-baseHome, baseHome, baseAway)
		if attPts-baseHome > slack {
			t.Errorf("%s: attacking gains %.3f points a match over balanced, want at most %.2f", c.name, attPts-baseHome, slack)
		}
		if c.home < c.away && defPts-baseHome < -slack/2 {
			t.Errorf("%s: defensive loses %.3f points a match for the underdog", c.name, baseHome-defPts)
		}
		if att.goals <= base.goals || def.goals >= base.goals {
			t.Errorf("%s: goals attacking %d, balanced %d, defensive %d", c.name, att.goals, base.goals, def.goals)
		}
	}
}
