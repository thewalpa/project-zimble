package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches"
)

// scene is a first-half moment with the home side (0) attacking towards
// X = PitchLength: the home slot 1 passes along the middle of the pitch to
// slot 9. Every other player stands on a touchline near the home goal, out
// of the way, except the away keeper on his goal line and an away
// defender, the second-last opponent, at defenderX.
type scene struct {
	passerX, receiverX, defenderX int64
	restart                       restartKind // a set piece the pass is taken from, or 0
}

const passer, receiver = 1, 9

// play makes the pass, after arrange if given, and follows the ball until
// someone controls it, it stops, or play stops.
func (sc scene) play(t *testing.T, arrange func(*session)) *session {
	t.Helper()
	s := start(t, input(1, 60, 60))
	s.dead = false
	for side := range s.teams {
		for slot := range matches.StartersPerTeam {
			p := s.teams[side].at(slot)
			p.pos, p.busy = vec{300 + int64(slot)*100, int64(side) * pitchW}, 0
		}
	}
	mid := int64(pitchW / 2)
	s.teams[0].at(passer).pos = vec{sc.passerX, mid}
	s.teams[0].at(receiver).pos = vec{sc.receiverX, mid}
	keeper, defender := s.keeperSlot(1), 2
	if defender == keeper {
		defender++
	}
	s.teams[1].at(keeper).pos = vec{pitchL, 200}
	s.teams[1].at(defender).pos = vec{sc.defenderX, 200}
	if arrange != nil {
		arrange(s)
	}
	s.gain(0, passer)
	if sc.restart != 0 {
		s.restart.kind, s.ball.setPiece = sc.restart, true
	}
	s.kick(0, passer, s.teams[0].at(receiver).pos, 250, s.p.GroundDecel)
	s.ball.passTo, s.ball.aim = receiver, s.teams[0].at(receiver).pos
	var dst matches.MatchStepResult
	for range 100 {
		s.tick++
		s.fly(&dst)
		if s.dead || s.ball.carried || s.ball.vel == (vec{}) {
			break
		}
	}
	return s
}

func caught(s *session) bool {
	return s.dead && s.restart.kind == restartFreeKick && s.restart.side == 1 && s.stats.offsides == [2]int{1, 0}
}

func TestOffsideLaw(t *testing.T) {
	for name, tc := range map[string]struct {
		sc     scene
		caught bool
	}{
		"beyond the second-last opponent": {scene{passerX: 6000, receiverX: 8000, defenderX: 7500}, true},
		"level with him":                  {scene{passerX: 6000, receiverX: 7500, defenderX: 7500}, false},
		"short of him":                    {scene{passerX: 6000, receiverX: 7000, defenderX: 7500}, false},
		"behind the ball":                 {scene{passerX: 9000, receiverX: 8000, defenderX: 7500}, false},
		"in his own half":                 {scene{passerX: 3000, receiverX: 5000, defenderX: 4000}, false},
		"from a throw-in":                 {scene{passerX: 6000, receiverX: 8000, defenderX: 7500, restart: restartThrowIn}, false},
		"from a corner":                   {scene{passerX: 6000, receiverX: 8000, defenderX: 7500, restart: restartCorner}, false},
		"from a free kick":                {scene{passerX: 6000, receiverX: 8000, defenderX: 7500, restart: restartFreeKick}, true},
	} {
		s := tc.sc.play(t, nil)
		if got := caught(s); got != tc.caught {
			t.Errorf("%s: caught offside %t, want %t (dead %t, restart %+v, offsides %v)", name, got, tc.caught, s.dead, s.restart, s.stats.offsides)
		}
		if tc.caught {
			if at := s.restart.spot; at.x < tc.sc.receiverX-s.p.ControlRadius || at.x > tc.sc.receiverX+s.p.ControlRadius {
				t.Errorf("%s: free kick at %v, receiver at %d", name, at, tc.sc.receiverX)
			}
		} else if s.stats.offsides != [2]int{} {
			t.Errorf("%s: offsides %v", name, s.stats.offsides)
		}
	}
}

// A team-mate in an offside position who is not the receiver leaves the
// ball alone, even when it runs past his feet.
func TestOffsidePlayerLeavesTheBall(t *testing.T) {
	sc := scene{passerX: 6000, receiverX: 9500, defenderX: 7500}
	s := sc.play(t, func(s *session) {
		s.teams[0].at(receiver - 1).pos = vec{8000, pitchW / 2} // on the path, offside
	})
	if s.stats.offsides != [2]int{1, 0} || s.restart.spot.x < 9000 {
		t.Fatalf("offsides %v, free kick at %v: want the receiver caught, not the player on the path", s.stats.offsides, s.restart.spot)
	}
}
