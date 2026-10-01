package tick

import (
	"github.com/thewalpa/project-zimble/internal/matches"
)

// Directions. A side's frame measures depth from its own goal line
// (0) towards the opponent's (pitchL); lateral runs across in the same
// handedness, so the pitch turns half a circle when ends change.

// attacksUp reports whether side (a team index) attacks towards X = pitchL.
func (s *session) attacksUp(side int) bool { return (side == 0) != s.secondHalf }

// abs converts a side's (depth, lateral) to pitch coordinates.
func (s *session) abs(side int, depth, lateral int64) vec {
	if s.attacksUp(side) {
		return vec{depth, lateral}
	}
	return vec{pitchL - depth, pitchW - lateral}
}

// depth is how far p is from side's own goal line.
func (s *session) depth(side int, p vec) int64 {
	if s.attacksUp(side) {
		return p.x
	}
	return pitchL - p.x
}

func (s *session) goal(side int) vec    { return s.abs(side, pitchL, pitchW/2) } // the goal side attacks
func (s *session) ownGoal(side int) vec { return s.abs(side, 0, pitchW/2) }

// inBox reports whether p is in side's own penalty area.
func (s *session) inBox(side int, p vec) bool {
	return s.depth(side, p) <= boxDepth && abs64(p.y-pitchW/2) <= boxHalfWide
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *session) draw(n int64) int64 { return int64(s.rng.IntN(int(n))) }

// chance draws true with probability p ppm.
func (s *session) chance(p int64) bool { return s.draw(ppm) < p }

// step plays one tick.
func (s *session) step(dst *matches.MatchStepResult) {
	s.tick++
	var targets [2][matches.StartersPerTeam]vec
	var sprint [2][matches.StartersPerTeam]bool
	s.plan(&targets, &sprint)
	if s.ball.carried && !s.dead {
		s.stats.possession[s.ball.side]++
		s.act(&targets, &sprint)
	}
	s.move(&targets, &sprint)
	switch {
	case s.dead:
		s.takeRestart()
	case s.ball.carried:
		s.ball.pos = s.carrier().pos
		s.challenge()
	default:
		s.fly(dst)
	}
}

func (s *session) carrier() *player { return s.teams[s.ball.side].at(s.ball.slot) }

// plan sets every player's target spot for this tick.
func (s *session) plan(targets *[2][matches.StartersPerTeam]vec, sprint *[2][matches.StartersPerTeam]bool) {
	b := s.ball.pos
	possession := s.lastSide
	switch {
	case s.dead:
		possession = s.restart.side
	case s.ball.carried:
		possession = s.ball.side
	}
	for side := range s.teams {
		t := &s.teams[side]
		inPossession := side == possession
		ballDepth := s.depth(side, b)
		// Runs stop at the offside line, except a forward's run in behind
		// while a team-mate has the ball.
		line := s.secondLast(side)
		onside := max(line, ballDepth, pitchL/2)
		behind := min(line+s.p.RunDepth, pitchL-boxDepth/2)
		building := inPossession && s.ball.carried && !s.dead
		for slot := range t.pitch {
			p := t.at(slot)
			if p.role == matches.Goalkeeper {
				targets[side][slot] = s.keeperSpot(side)
				continue
			}
			if s.tick >= p.driftUntil {
				p.drift = vec{s.spread(s.p.DriftDepth), s.spread(s.p.DriftWidth)}
				p.driftUntil = s.tick + s.p.DriftTicks + uint32(s.draw(int64(s.p.DriftTicks)))
			}
			d := s.p.DefendDepth[p.role] + s.p.MentalityDefendDepth[t.mentality]
			lat := t.lateral[slot]
			if inPossession {
				d = s.p.AttackDepth[p.role] + s.p.MentalityAttackDepth[t.mentality] + p.drift.x
				lat += p.drift.y
			}
			d += (ballDepth - pitchL/2) * s.p.ShiftPermille / permille
			if inPossession {
				d = min(d, onside)
			}
			if building && p.role == matches.Forward && slot != s.ball.slot {
				// A run in behind bites deepest against a high line: the
				// defending side's MentalityConcedePermille prices the room
				// its line leaves.
				if s.tick >= p.runUntil && s.chance(s.p.RunPPM[t.mentality]*s.p.MentalityConcedePermille[s.teams[1-side].mentality]/permille) {
					p.runUntil = s.tick + s.p.RunTicks
				}
				if s.tick < p.runUntil {
					d, sprint[side][slot] = max(d, behind), true
				}
			}
			if s.dead && s.restart.kind == restartKickoff {
				d = min(d, pitchL/2-100)
			}
			d = min(max(d, 500), pitchL-500)
			lat += (s.abs(side, 0, b.y).y - pitchW/2) * s.p.LateralPermille / permille
			lat = min(max(lat, 200), pitchW-200)
			targets[side][slot] = s.abs(side, d, lat)
		}
		if !inPossession && !s.dead {
			spots := targets[side]
			s.mark(side, &targets[side])
			if s.ball.carried {
				// The back line holds: it follows a runner only so far
				// while the carrier looks for a pass.
				for slot := range t.pitch {
					if t.at(slot).role == matches.Defender {
						d := max(s.depth(side, targets[side][slot]), s.depth(side, spots[slot])-s.p.LineHold)
						targets[side][slot] = s.abs(side, d, s.abs(side, 0, targets[side][slot].y).y)
					}
				}
			}
		}
	}

	switch {
	case s.dead:
		r := &s.restart
		for slot := range matches.StartersPerTeam {
			// Opponents keep their distance from a set piece.
			if tgt := targets[1-r.side][slot]; within(tgt, r.spot, s.p.RestartDistance-1) {
				away := tgt.sub(r.spot)
				if away == (vec{}) {
					away = s.ownGoal(1 - r.side).sub(r.spot)
				}
				targets[1-r.side][slot] = clampPitch(r.spot.add(away.withLength(s.p.RestartDistance)))
			}
		}
		targets[r.side][r.slot], sprint[r.side][r.slot] = r.spot, true

	case s.ball.carried:
		// The nearest defenders press the carrier; the next one covers.
		def := 1 - s.ball.side
		c := s.carrier().pos
		order, count := s.nearest(def, c, false)
		n := 1
		if s.depth(def, c) > pitchL*2/3 { // a high press
			n = s.p.Pressers[s.teams[def].mentality]
		}
		n = min(n, count-1)
		for _, slot := range order[:n] {
			targets[def][slot], sprint[def][slot] = c, true
		}
		cover := order[n]
		targets[def][cover], sprint[def][cover] = c.add(s.ownGoal(def).sub(c).scale(300, permille)), true

	default:
		// A loose ball: the receiver runs onto a pass while the opponents
		// hold their shape, ready to close him down. Otherwise the nearest
		// player of each side chases the ball, a keeper only in his area.
		ahead := s.ball.pos.add(s.ball.vel.scale(2, 1))
		for side := range s.teams {
			if s.ball.passTo >= 0 {
				if side == s.lastSide {
					targets[side][s.ball.passTo], sprint[side][s.ball.passTo] = s.ball.aim, true
				}
				continue
			}
			order, n := s.nearest(side, ahead, s.inBox(side, s.ball.pos))
			for _, slot := range order[:n] {
				if !s.flagged(side, slot) { // an offside player leaves it
					targets[side][slot], sprint[side][slot] = ahead, true
					break
				}
			}
		}
	}
}

// mark has the defending side's outfield players pick up opponents.
// Pairs are matched nearest first, by the opponent's distance from the
// defender's spot, each player at most once and only within MarkRadius. A
// defender stands MarkDistance goal-side of a man within TightMarkRadius of
// his spot; for a man farther away he holds a point between his spot and
// that position, nearer the spot the farther the man is, so the shape
// changes smoothly as opponents move.
func (s *session) mark(side int, targets *[matches.StartersPerTeam]vec) {
	const n = matches.StartersPerTeam
	t, opp := &s.teams[side], &s.teams[1-side]
	goal := s.ownGoal(side)
	// Each defender's opponents within MarkRadius of his spot, nearest
	// first (ties by slot).
	var cand [n][n]int
	var d2 [n][n]int64
	var count, next [n]int
	r2, tight2 := s.p.MarkRadius*s.p.MarkRadius, s.p.TightMarkRadius*s.p.TightMarkRadius
	var men [n]vec
	var outfield [n]bool
	for o := range opp.pitch {
		q := opp.at(o)
		men[o], outfield[o] = q.pos, q.role != matches.Goalkeeper
	}
	for slot := range t.pitch {
		if t.at(slot).role == matches.Goalkeeper {
			continue
		}
		spot := targets[slot]
		for o, m := range men {
			if !outfield[o] {
				continue
			}
			d := dist2(m, spot)
			if d >= r2 {
				continue
			}
			k := count[slot]
			for ; k > 0 && d2[slot][k-1] > d; k-- {
				cand[slot][k], d2[slot][k] = cand[slot][k-1], d2[slot][k-1]
			}
			cand[slot][k], d2[slot][k] = o, d
			count[slot]++
		}
	}
	var marked [n]bool
	for {
		best := -1
		for slot := range t.pitch {
			for next[slot] < count[slot] && marked[cand[slot][next[slot]]] {
				next[slot]++
			}
			if next[slot] < count[slot] && (best < 0 || d2[slot][next[slot]] < d2[best][next[best]]) {
				best = slot
			}
		}
		if best < 0 {
			return
		}
		o, d := cand[best][next[best]], d2[best][next[best]]
		marked[o], count[best] = true, 0
		m := men[o]
		tight := m.add(goal.sub(m).withLength(s.p.MarkDistance))
		pull := min((r2-d)*permille/(r2-tight2), permille)
		targets[best] = targets[best].add(tight.sub(targets[best]).scale(pull, permille))
	}
}

// keeperSpot is on the line between the ball and the middle of the goal.
func (s *session) keeperSpot(side int) vec {
	lat := pitchW/2 + (s.abs(side, 0, s.ball.pos.y).y-pitchW/2)*s.p.KeeperTrackPermille/permille
	lat = min(max(lat, pitchW/2-postHalf), pitchW/2+postHalf)
	return s.abs(side, s.p.KeeperDepth, lat)
}

func (s *session) keeperSlot(side int) int {
	t := &s.teams[side]
	for slot := range t.pitch {
		if t.at(slot).role == matches.Goalkeeper {
			return slot
		}
	}
	return 0 // unreachable: a side always has one goalkeeper
}

// nearest returns side's slots ordered by distance to p (ties by slot),
// and how many there are: outfield players only unless keeper is set.
func (s *session) nearest(side int, p vec, keeper bool) (order [matches.StartersPerTeam]int, n int) {
	t := &s.teams[side]
	var d [matches.StartersPerTeam]int64
	for slot := range t.pitch {
		q := t.at(slot)
		if q.role == matches.Goalkeeper && !keeper {
			continue
		}
		order[n], d[n] = slot, dist2(q.pos, p)
		for k := n; k > 0 && d[k] < d[k-1]; k-- {
			order[k], order[k-1] = order[k-1], order[k]
			d[k], d[k-1] = d[k-1], d[k]
		}
		n++
	}
	return order, n
}

// move steps every player towards his target.
func (s *session) move(targets *[2][matches.StartersPerTeam]vec, sprint *[2][matches.StartersPerTeam]bool) {
	for side := range s.teams {
		t := &s.teams[side]
		for slot := range t.pitch {
			p := t.at(slot)
			tgt := clampPitch(targets[side][slot])
			speed := p.sprint
			if s.ball.carried && !s.dead && side == s.ball.side && slot == s.ball.slot {
				speed = speed * s.p.DribblePermille / permille
			} else if !sprint[side][slot] && within(p.pos, tgt, s.p.SprintDistance) {
				speed = speed * s.p.JogPermille / permille
			}
			p.pos = moveToward(p.pos, tgt, speed)
		}
	}
}

// act is the carrier's decision: shoot, pass or dribble.
func (s *session) act(targets *[2][matches.StartersPerTeam]vec, sprint *[2][matches.StartersPerTeam]bool) {
	side, slot := s.ball.side, s.ball.slot
	t := &s.teams[side]
	c := t.at(slot)
	targets[side][slot], sprint[side][slot] = c.pos, false
	if s.tick < c.busy {
		return
	}
	if s.ball.setPiece {
		if !s.pass(side, slot) && s.tick < s.ball.setPieceFrom+s.p.RestartTimeoutTicks {
			return // wait for a teammate to get free
		}
		s.ball.setPiece = false
		return
	}

	goal := s.goal(side)
	gd := dist(c.pos, goal)
	if gd < s.p.ShotRange {
		left := s.p.ShotRange - gd
		p := s.p.ShotPPM * left / s.p.ShotRange * left / s.p.ShotRange * s.p.MentalityShotPermille[t.mentality] / permille
		// A high line leaves room to shoot more often; a deep block less.
		p = p * s.p.MentalityConcedePermille[s.teams[1-side].mentality] / permille
		if gd <= s.p.CertainShotRange || s.chance(p) {
			s.shoot(side, slot)
			return
		}
	}
	pressure := int64(pitchL * pitchL)
	nearestOpp := vec{}
	for oslot := range matches.StartersPerTeam {
		o := s.teams[1-side].at(oslot)
		if d := dist2(o.pos, c.pos); d < pressure {
			pressure, nearestOpp = d, o.pos
		}
	}
	pressure = isqrt(pressure)
	passPPM := s.p.PassPPM
	if pressure < s.p.PressureDistance {
		passPPM = s.p.PressuredPassPPM
	}
	if s.running(side) {
		// He looks for the run: the counter pass lands more often against a
		// high line.
		passPPM = max(passPPM, s.p.RunPassPPM*s.p.MentalityConcedePermille[s.teams[1-side].mentality]/permille)
	}
	if s.chance(passPPM) && s.pass(side, slot) {
		return
	}
	// Dribble at goal, stepping away from a close opponent ahead.
	tgt := goal
	if pressure < 2*s.p.PressureDistance && s.depth(side, nearestOpp) > s.depth(side, c.pos) {
		if nearestOpp.y > c.pos.y {
			tgt.y = c.pos.y - 800
		} else {
			tgt.y = c.pos.y + 800
		}
		tgt.x = s.abs(side, s.depth(side, c.pos)+500, 0).x
	}
	targets[side][slot] = tgt
}

// pass picks the best open pass and plays it: to a team-mate's feet, or a
// through ball into the space behind the opponents' line for him to run
// onto. It reports false when nobody is worth passing to. The passer
// overlooks a team-mate offside by up to OffsideVision.
func (s *session) pass(side, slot int) bool {
	t := &s.teams[side]
	c := t.at(slot)
	seen := s.offsideLine(side) + s.p.OffsideVision
	line := s.secondLast(side)
	lead := s.abs(side, s.p.ThroughBallLead, 0).sub(s.abs(side, 0, 0)) // towards goal
	best, bestScore, bestAim := -1, int64(0), vec{}
	consider := func(j int, aim vec, run int64) {
		if within(c.pos, aim, s.p.MinPass-1) || !within(c.pos, aim, s.p.MaxPass) {
			return
		}
		open := s.openness(side, c.pos, aim, run)
		if open < s.p.BlockedDistance {
			return
		}
		progress := s.depth(side, aim) - s.depth(side, c.pos)
		score := progress*s.p.ProgressPermille[t.mentality]/permille + open - dist(c.pos, aim)/4 + s.draw(s.p.PassNoise)
		if t.at(j).role == matches.Goalkeeper {
			score -= 1500
		}
		if best < 0 || score > bestScore {
			best, bestScore, bestAim = j, score, aim
		}
	}
	for j := range t.pitch {
		q := t.at(j).pos
		if j == slot || s.depth(side, q) > seen {
			continue
		}
		consider(j, q, 0)
		if through := q.add(lead); t.at(j).role != matches.Goalkeeper && s.depth(side, through) > line &&
			s.depth(side, through) <= pitchL-boxDepth/2 {
			run := s.p.ThroughBallLead
			if s.tick < t.at(j).runUntil {
				run -= s.p.RunStart // already sprinting
			}
			consider(j, through, run)
		}
	}
	if best < 0 {
		return false
	}
	aim := bestAim
	d := dist(c.pos, aim)
	miss := d * (per10k - s.against(c.eff[effPassing], s.teams[1-side].level.defending)) / per10k * s.p.PassErrorPermille / permille
	aim = clampPitch(aim.add(vec{s.spread(miss), s.spread(miss)}))
	// Fast enough to arrive at PassArrivalSpeed: v0² = v1² + 2ad.
	speed := isqrt(s.p.PassArrivalSpeed*s.p.PassArrivalSpeed + 2*s.p.GroundDecel*dist(c.pos, aim))
	s.kick(side, slot, aim, min(max(speed, s.p.MinPassSpeed), s.p.MaxPassSpeed), s.p.GroundDecel)
	s.ball.passTo, s.ball.aim = best, aim
	s.stats.passes[side]++
	return true
}

// openness is how free a pass from from to aim is: the space around the
// aim or along the lane, whichever is less. The space is two thirds of the
// nearest opponent's distance from the aim for a pass to feet, and for a
// through ball the head start the receiver, run cm from the aim, has on
// that opponent. A marker at the passer's feet does not block the lane.
func (s *session) openness(side int, from, aim vec, run int64) int64 {
	opp := &s.teams[1-side]
	d := dist(from, aim)
	near, lane := int64(pitchL*pitchL), int64(1000*1000) // squared
	for o := range opp.pitch {
		op := opp.at(o).pos
		near = min(near, dist2(op, aim))
		if t, l := closest(from, aim, op); t*d/permille > s.p.TackleRadius {
			lane = min(lane, l)
		}
	}
	space := min(isqrt(near)*2/3, 1000)
	if run > 0 {
		space = min(isqrt(near)-run, 1000)
	}
	return min(space, isqrt(lane))
}

// secondLast is the depth, seen from side, of the second-last opponent,
// goalkeeper included.
func (s *session) secondLast(side int) int64 {
	opp := &s.teams[1-side]
	first, second := int64(-1), int64(-1)
	for slot := range opp.pitch {
		d := s.depth(side, opp.at(slot).pos)
		if d > first {
			first, second = d, first
		} else if d > second {
			second = d
		}
	}
	return second
}

// offsideLine is the depth, seen from side, beyond which its players are in
// an offside position: the second-last opponent, unless the ball or the
// halfway line is further.
func (s *session) offsideLine(side int) int64 {
	return max(s.secondLast(side), s.depth(side, s.ball.pos), pitchL/2)
}

// offsidePositions returns side's slots in an offside position as the
// player at slot plays the ball.
func (s *session) offsidePositions(side, slot int) uint16 {
	line := s.offsideLine(side)
	t := &s.teams[side]
	var mask uint16
	for j := range t.pitch {
		if j != slot && s.depth(side, t.at(j).pos) > line {
			mask |= 1 << j
		}
	}
	return mask
}

// running reports whether one of side's players is making a run in behind.
func (s *session) running(side int) bool {
	t := &s.teams[side]
	for slot := range t.pitch {
		if s.tick < t.at(slot).runUntil {
			return true
		}
	}
	return false
}

// flagged reports whether side's slot was in an offside position when his
// side last played the ball.
func (s *session) flagged(side, slot int) bool {
	return side == s.ball.offsideSide && s.ball.offside&(1<<slot) != 0
}

// caughtOffside gives the opponents a free kick where side's slot reached
// the ball.
func (s *session) caughtOffside(side, slot int) {
	s.stats.offsides[side]++
	s.setRestart(restartFreeKick, 1-side, clampPitch(s.teams[side].at(slot).pos))
}

// spread draws an offset in [-n, n].
func (s *session) spread(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return s.draw(2*n+1) - n
}

func (s *session) shoot(side, slot int) {
	c := s.teams[side].at(slot)
	aim := s.goal(side)
	aim.y += s.spread(postHalf - 60)
	d := dist(c.pos, aim)
	skill := s.against(c.eff[effFinishing], s.teams[1-side].level.goalkeeping)
	miss := d * (per10k - skill) / per10k * s.p.ShotErrorPermille / permille
	aim.y += s.spread(miss)
	// Aim beyond the line so the ball crosses it.
	aim = aim.add(aim.sub(c.pos).withLength(300))
	speed := s.p.MinShotSpeed + (s.p.MaxShotSpeed-s.p.MinShotSpeed)*skill/per10k
	s.kick(side, slot, aim, speed, s.p.AirDecel)
	s.ball.shot, s.ball.finishing = true, c.eff[effFinishing]
	s.ball.onGoal = abs64(aim.y-pitchW/2) < postHalf
	s.stats.shots[side]++
}

func (s *session) kick(side, slot int, aim vec, speed, decel int64) {
	p := s.teams[side].at(slot)
	s.ball.offside, s.ball.offsideSide = 0, side
	if k := s.restart.kind; !s.ball.setPiece || k == restartKickoff || k == restartFreeKick {
		s.ball.offside = s.offsidePositions(side, slot)
	}
	s.ball.carried, s.ball.shot, s.ball.setPiece = false, false, false
	s.ball.side, s.ball.slot, s.ball.passTo = side, slot, -1
	s.ball.vel = aim.sub(p.pos).withLength(speed)
	s.ball.decel = decel
	p.busy = s.tick + s.p.KickTicks
	s.ball.free = s.tick + 1 // nobody controls it in its first instant
	s.touch(side, slot)
}

func (s *session) touch(side, slot int) {
	s.lastSide = side
	s.teams[side].lastTouch = int(s.teams[side].pitch[slot])
}

// challenge lets defenders near the carrier tackle.
func (s *session) challenge() {
	if s.tick < s.ball.protect {
		return
	}
	def := 1 - s.ball.side
	c := s.carrier()
	for slot := range matches.StartersPerTeam {
		d := s.teams[def].at(slot)
		if s.tick < d.busy || !within(d.pos, c.pos, s.p.TackleRadius) || !s.chance(s.p.TackleAttemptPPM) {
			continue
		}
		tackle, dribble := d.eff[effDefending], c.eff[effDribbling]
		p := s.p.TacklePPM * 2 * tackle / max(tackle+dribble, 1)
		if !s.chance(min(max(p, s.p.MinControlPPM), s.p.MaxControlPPM)) {
			d.busy = s.tick + s.p.BeatenTicks
			continue
		}
		s.stats.tackles[def]++
		c.busy = s.tick + s.p.MissTicks
		if s.chance(s.p.WinBallPPM) {
			s.gain(def, slot)
			return
		}
		// Knocked loose.
		dir := vec{s.spread(100), s.spread(100)}
		if dir == (vec{}) {
			dir = vec{1, 0}
		}
		s.ball.carried, s.ball.shot, s.ball.passTo = false, false, -1
		s.ball.vel, s.ball.decel = dir.withLength(40+s.draw(60)), s.p.GroundDecel
		s.ball.offside, s.ball.offsideSide = s.offsidePositions(def, slot), def
		d.busy = s.tick + s.p.KickTicks
		s.touch(def, slot)
		return
	}
}

// gain gives side's slot the ball.
func (s *session) gain(side, slot int) {
	p := s.teams[side].at(slot)
	if !s.ball.carried && s.ball.passTo >= 0 && s.lastSide == side {
		s.stats.completed[side]++
	}
	s.ball = ball{pos: p.pos, carried: true, side: side, slot: slot, passTo: -1, protect: s.tick + s.p.ProtectTicks}
	p.busy = s.tick + s.p.FirstTouchTicks
	s.touch(side, slot)
}

// candidate is a player within reach of the ball's path this tick. Order
// breaks ties: two players arriving on a ball at rest contest it evenly.
type candidate struct {
	t, d, order int64
	side, slot  int
	hands       bool
}

func (a candidate) after(b candidate) bool {
	if a.t != b.t {
		return a.t > b.t
	}
	if a.d != b.d {
		return a.d > b.d
	}
	return a.order > b.order
}

// fly moves a loose ball one tick: someone along its path may control it;
// otherwise it may leave the pitch.
func (s *session) fly(dst *matches.MatchStepResult) {
	from := s.ball.pos
	to := from.add(s.ball.vel)
	// Where the path leaves the pitch (permille of it), the point on the
	// line it crosses first, and whether that is a goal line.
	cross, exit, goalLine := int64(permille), vec{}, false
	if to.x < 0 || to.x > pitchL {
		x := int64(0)
		if to.x > pitchL {
			x = pitchL
		}
		cross, goalLine = (x-from.x)*permille/(to.x-from.x), true
		exit = vec{x, from.y + (to.y-from.y)*(x-from.x)/(to.x-from.x)}
	}
	if to.y < 0 || to.y > pitchW {
		y := int64(0)
		if to.y > pitchW {
			y = pitchW
		}
		if t := (y - from.y) * permille / (to.y - from.y); t < cross {
			cross, goalLine = t, false
			exit = vec{from.x + (to.x-from.x)*(y-from.y)/(to.y-from.y), y}
		}
	}

	var cands [2 * matches.StartersPerTeam]candidate
	n := 0
	for side := range s.teams {
		if s.tick < s.ball.free {
			break
		}
		for slot := range matches.StartersPerTeam {
			p := s.teams[side].at(slot)
			if s.tick < p.busy {
				continue
			}
			hands := p.role == matches.Goalkeeper && s.inBox(side, p.pos)
			reach := s.p.ControlRadius
			if hands {
				reach = s.p.KeeperReach
			}
			t, d2 := closest(from, to, p.pos)
			if d2 > reach*reach || t > cross {
				continue
			}
			if s.flagged(side, slot) && (side != s.lastSide || slot != s.ball.passTo) {
				continue // offside, he leaves it
			}
			d := isqrt(d2)
			c := candidate{t: t, d: d, order: s.draw(ppm), side: side, slot: slot, hands: hands}
			k := n
			for ; k > 0 && cands[k-1].after(c); k-- {
				cands[k] = cands[k-1]
			}
			cands[k] = c
			n++
		}
	}
	speed := s.ball.vel.norm()
	for _, c := range cands[:n] {
		if s.flagged(c.side, c.slot) {
			s.caughtOffside(c.side, c.slot)
			return
		}
		p := s.teams[c.side].at(c.slot)
		reach := s.p.ControlRadius
		var chance int64
		switch {
		case c.hands && s.ball.shot && c.side != s.lastSide:
			reach = s.p.KeeperReach
			skill := s.against(p.eff[effGoalkeeping], s.ball.finishing)
			chance = s.p.SavePPM + (skill-5000)*s.p.SaveSkillPPM/5000 - max(speed-s.p.EasySpeed, 0)*s.p.SaveSpeedPenaltyPPM
		default:
			if c.hands {
				reach = s.p.KeeperReach
			}
			opp := &s.teams[1-c.side].level
			skill := s.against(p.eff[effPassing], opp.defending)
			if c.hands {
				skill = s.against(p.eff[effGoalkeeping], opp.passing)
			} else if c.side != s.lastSide {
				skill = s.against(p.eff[effDefending], opp.passing)
			}
			chance = s.p.ControlPPM + (skill-5000)*s.p.ControlSkillPPM/5000 - max(speed-s.p.EasySpeed, 0)*s.p.ControlSpeedPenaltyPPM
			if c.side != s.lastSide {
				chance = chance * s.p.InterceptPermille / permille
			}
		}
		chance = chance * (3*reach - c.d) / (3 * reach)
		if s.chance(min(max(chance, s.p.MinControlPPM), s.p.MaxControlPPM)) {
			if c.hands && s.ball.shot && c.side != s.lastSide {
				s.saved(c.side)
			}
			s.gain(c.side, c.slot)
			return
		}
		p.busy = s.tick + s.p.MissTicks
		contact := from.add(to.sub(from).scale(c.t, permille))
		switch {
		case c.hands && s.ball.shot && c.side != s.lastSide:
			if !s.chance(s.p.ParryPPM) {
				continue // beaten
			}
			// Parried away from goal.
			s.saved(c.side)
			s.deflect(c, contact, vec{s.goal(c.side).x - contact.x, s.spread(2000)}.withLength(speed/3))
			return
		case s.chance(s.p.DeflectPPM):
			// A heavy touch: the ball bounces off, slower.
			v := s.ball.vel.scale(1, 2)
			s.deflect(c, contact, v.add(vec{s.spread(v.norm() / 2), s.spread(v.norm() / 2)}))
			return
		}
	}

	if cross < permille {
		s.out(dst, clampPitch(exit), goalLine)
		return
	}
	s.ball.pos = to
	if s.ball.passTo >= 0 && s.ball.vel.dot(s.ball.aim.sub(to)) < 0 {
		s.ball.passTo = -1 // past its aim: anybody's ball
	}
	if v := speed - s.ball.decel; v >= s.p.MinBallSpeed {
		s.ball.vel = s.ball.vel.withLength(v)
	} else {
		// At rest it is anybody's ball.
		s.ball.vel = vec{}
		s.ball.shot, s.ball.passTo = false, -1
	}
}

// saved counts keeper's side stopping a shot, a save if it was on target.
func (s *session) saved(keeper int) {
	if s.ball.onGoal {
		s.stats.saves[keeper]++
		s.stats.onTarget[1-keeper]++
	}
}

// deflect sends the ball off a player's touch at contact.
// A team-mate's touch is a new play for offside; an opponent's is not.
func (s *session) deflect(c candidate, contact, vel vec) {
	s.ball.pos, s.ball.vel = contact, vel
	s.ball.shot, s.ball.passTo, s.ball.decel = false, -1, s.p.GroundDecel
	if c.side == s.ball.offsideSide {
		s.ball.offside = s.offsidePositions(c.side, c.slot)
	}
	s.touch(c.side, c.slot)
}

// out handles the ball leaving the pitch at p, over a goal line or a
// touchline.
func (s *session) out(dst *matches.MatchStepResult, p vec, goalLine bool) {
	s.ball.pos, s.ball.vel = p, vec{}
	if goalLine {
		// The side attacking this end.
		att := 0
		if s.depth(0, p) != pitchL {
			att = 1
		}
		switch {
		case abs64(p.y-pitchW/2) <= postHalf:
			s.scored(dst, att)
		case s.lastSide == att:
			s.setRestart(restartGoalKick, 1-att, s.abs(1-att, 550, pitchW/2))
		default:
			y := int64(0)
			if p.y > pitchW/2 {
				y = pitchW
			}
			s.setRestart(restartCorner, att, vec{p.x, y})
		}
		return
	}
	s.setRestart(restartThrowIn, 1-s.lastSide, p)
}

func (s *session) scored(dst *matches.MatchStepResult, side int) {
	t := &s.teams[side]
	idx := t.lastTouch
	if idx < 0 || t.players[idx].state != onPitch {
		// The scorer went off since his touch: credit the side's most
		// advanced player.
		best := int64(-1)
		for slot := range t.pitch {
			if d := s.depth(side, t.at(slot).pos); d > best {
				best, idx = d, int(t.pitch[slot])
			}
		}
	}
	scorer := t.players[idx].id
	s.score[side]++
	if s.ball.shot && s.ball.side == side {
		s.stats.onTarget[side]++
	}
	s.goals = append(s.goals, matches.Goal{Minute: s.minute, Side: matches.Side(side + 1), Scorer: scorer})
	dst.Events = append(dst.Events, matches.MatchEvent{
		Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventGoal, Side: matches.Side(side + 1), Player: scorer,
	})
	s.kickoff(matches.Side(2-side), false)
}

// kickoff restarts play from the centre spot for side. At the start of a
// half the players line up at once; after a goal they walk back.
func (s *session) kickoff(side matches.Side, lineUp bool) {
	k := side.Index()
	slot := 0
	t := &s.teams[k]
	best := int64(-1)
	for sl := range t.pitch { // the most advanced by role, then slot
		if r := int64(t.at(sl).role); r > best {
			best, slot = r, sl
		}
	}
	centre := vec{pitchL / 2, pitchW / 2}
	s.ball = ball{pos: centre, passTo: -1}
	s.dead = true
	s.restart = restart{kind: restartKickoff, side: k, slot: slot, spot: centre, timeout: s.tick + s.p.RestartTimeoutTicks}
	if !lineUp {
		s.restart.notBefore = s.tick + s.p.CelebrationTicks
		return
	}
	var targets [2][matches.StartersPerTeam]vec
	var sprint [2][matches.StartersPerTeam]bool
	s.lastSide = k
	s.plan(&targets, &sprint)
	for sd := range s.teams {
		for sl := range matches.StartersPerTeam {
			s.teams[sd].at(sl).pos = clampPitch(targets[sd][sl])
		}
	}
}

func (s *session) setRestart(kind restartKind, side int, spot vec) {
	slot := s.keeperSlot(side)
	if kind != restartGoalKick {
		order, _ := s.nearest(side, spot, false)
		slot = order[0]
	}
	s.ball = ball{pos: spot, passTo: -1}
	s.dead = true
	s.restart = restart{kind: kind, side: side, slot: slot, spot: spot, notBefore: s.tick + s.p.RestartTicks, timeout: s.tick + s.p.RestartTimeoutTicks}
}

// takeRestart puts the ball back in play once its taker is on the spot.
func (s *session) takeRestart() {
	r := &s.restart
	p := s.teams[r.side].at(r.slot)
	if s.tick >= r.timeout {
		p.pos = r.spot // never wait forever
	}
	if s.tick < r.notBefore || p.pos != r.spot {
		return
	}
	s.dead = false

	s.gain(r.side, r.slot)
	s.ball.protect = s.tick + s.p.RestartTicks
	s.ball.setPiece, s.ball.setPieceFrom = true, s.tick
}

// maxSuddenDeath bounds the sudden-death pairs of a shootout; a draw after
// them (practically impossible) is settled by one even draw.
const maxSuddenDeath = 50

// shootout takes a penalty shootout at full time, as the simple engine
// does: each side's players on the pitch kick in order of effective
// Finishing (ties: slot order), home first; best of five, then sudden
// death.
func (s *session) shootout() [2]uint16 {
	var takers [2][matches.StartersPerTeam]*player
	var keeper [2]int64
	for side := range s.teams {
		t := &s.teams[side]
		for slot := range t.pitch {
			p := t.at(slot)
			takers[side][slot] = p
			if p.role == matches.Goalkeeper {
				keeper[side] = p.eff[effGoalkeeping]
			}
		}
		order := takers[side][:]
		for a := 1; a < len(order); a++ {
			for b := a; b > 0 && order[b].eff[effFinishing] > order[b-1].eff[effFinishing]; b-- {
				order[b], order[b-1] = order[b-1], order[b]
			}
		}
	}
	var goals [2]uint16
	kick := func(side, n int) {
		taker := takers[side][n%matches.StartersPerTeam]
		c := s.p.ShootoutConversionPPM + (taker.eff[effFinishing]-keeper[1-side])*s.p.ShootoutSkillPPM/pointUnits
		if s.chance(min(max(c, s.p.MinShootoutPPM), s.p.MaxShootoutPPM)) {
			goals[side]++
		}
	}
	const firstKicks = 5
	for n := range firstKicks {
		for side := range 2 {
			kick(side, n)
			left := [2]int{firstKicks - n - 1, firstKicks - n - 1 + (1 - side)}
			if int(goals[0])+left[0] < int(goals[1]) || int(goals[1])+left[1] < int(goals[0]) {
				return goals
			}
		}
	}
	for n := firstKicks; n < firstKicks+maxSuddenDeath && goals[0] == goals[1]; n++ {
		kick(0, n)
		kick(1, n)
	}
	if goals[0] == goals[1] {
		goals[s.rng.IntN(2)]++
	}
	return goals
}
