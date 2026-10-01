package simple

import (
	"github.com/thewalpa/project-zimble/internal/matches"
)

// playMinute simulates minute s.minute (already incremented): the home side
// attempts a chance, then the away side. Draw order depends only on the
// minute sequence, so chunking Advance calls cannot change results.
func (s *session) playMinute(dst *matches.MatchStepResult) {
	for _, side := range [...]matches.Side{matches.Home, matches.Away} {
		if int64(s.rng.IntN(ppm)) >= s.chancePPM(side) {
			continue
		}
		shooter := s.pickShooter(side)
		if int64(s.rng.IntN(ppm)) >= s.conversionPPM(side, shooter) {
			continue
		}
		t := &s.teams[side.Index()]
		scorer := t.players[shooter].id
		s.score[side.Index()]++
		s.goals = append(s.goals, matches.Goal{Minute: s.minute, Side: side, Scorer: scorer})
		dst.Events = append(dst.Events, matches.MatchEvent{
			Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventGoal, Side: side, Player: scorer,
		})
	}
}

// effective returns a rating in model units (ratingUnits per point) after
// readiness (condition at kickoff) and fatigue. Minutes already played this
// match (before the current one) drive fatigue.
func (s *session) effective(p *player, rating uint8) int64 {
	played := int64(s.minute) - 1 - int64(p.on)
	perMinute := max(s.p.FatigueBasePer100k-int64(p.r.Stamina)*s.p.FatigueStaminaStepPer100k, 0)
	fatigue := min(max(played, 0)*perMinute, s.p.FatigueCapPer100k)
	return int64(rating) * ratingUnits * p.ready * (per100k - fatigue) / (per10k * per100k)
}

// attackDefense returns a team's weighted mean attack and defense, in model
// units.
func (s *session) attackDefense(t *team) (attack, defense int64) {
	aw, dw := s.p.AttackWeights, s.p.DefenseWeights
	var aSum, aShare, dSum, dShare int64
	for _, i := range t.pitch {
		p := &t.players[i]
		fin, pas, pace := s.effective(p, p.r.Finishing), s.effective(p, p.r.Passing), s.effective(p, p.r.Pace)
		def := s.effective(p, p.r.Defending)
		as, ds := s.p.AttackShare[p.role], s.p.DefenseShare[p.role]
		aSum += as * (aw.A*fin + aw.B*pas + aw.C*pace)
		aShare += as
		dSum += ds * (dw.A*def + dw.B*pace + dw.C*pas)
		dShare += ds
	}
	return aSum / (aShare * aw.sum()), dSum / (dShare * dw.sum())
}

func (s *session) chancePPM(side matches.Side) int64 {
	own, opp := &s.teams[side.Index()], &s.teams[side.Opponent().Index()]
	attack, _ := s.attackDefense(own)
	_, defense := s.attackDefense(opp)
	c := s.p.BaseChancePPM * 2 * attack / max(attack+defense, 1)
	c = c * s.p.MentalityOwnPermille[own.mentality] / permille
	c = c * s.p.MentalityConcedePermille[opp.mentality] / permille
	if side == matches.Home {
		c = c * s.p.HomeAdvantagePermille / permille
	} else {
		// The edge is split: the home side's boost is the away side's
		// handicap (permille²/HomeAdvantagePermille).
		c = c * permille / s.p.HomeAdvantagePermille
	}
	return min(max(c, s.p.MinChancePPM), s.p.MaxChancePPM)
}

// pickShooter draws an on-pitch player weighted by ShotShare * Finishing.
func (s *session) pickShooter(side matches.Side) int {
	t := &s.teams[side.Index()]
	var weights [matches.StartersPerTeam]int64
	var total int64
	for slot, i := range t.pitch {
		p := &t.players[i]
		weights[slot] = s.p.ShotShare[p.role] * s.effective(p, p.r.Finishing)
		total += weights[slot]
	}
	draw := int64(s.rng.IntN(int(total))) // total > 0: Params guarantees outfield shares
	for slot, w := range weights {
		if draw < w {
			return int(t.pitch[slot])
		}
		draw -= w
	}
	return int(t.pitch[len(t.pitch)-1])
}

func (s *session) conversionPPM(side matches.Side, shooter int) int64 {
	t, opp := &s.teams[side.Index()], &s.teams[side.Opponent().Index()]
	p := &t.players[shooter]
	fin := s.effective(p, p.r.Finishing)
	keeper := int64(0)
	for _, i := range opp.pitch {
		if k := &opp.players[i]; k.role == matches.Goalkeeper {
			keeper = s.effective(k, k.r.Goalkeeping)
		}
	}
	c := s.p.BaseConversionPPM * (fin + s.p.ConversionOffset) / (keeper + s.p.ConversionOffset)
	return min(max(c, s.p.MinConversionPPM), s.p.MaxConversionPPM)
}

// maxSuddenDeath bounds the sudden-death pairs of a shootout; a draw after
// them (practically impossible) is settled by one even draw.
const maxSuddenDeath = 50

// shootout takes a penalty shootout at full time. Each side's takers are
// its players on the pitch, best effective finishing first (ties: slot
// order), in turn; the sides kick alternately, home first. After five kicks
// each, or as soon as one side cannot be caught, the shootout is decided;
// otherwise it goes to sudden death, pair by pair.
func (s *session) shootout() [2]uint16 {
	var takers [2][matches.StartersPerTeam]int
	var keeper [2]int64
	for side := range s.teams {
		t := &s.teams[side]
		for slot, i := range t.pitch {
			takers[side][slot] = int(i)
			if p := &t.players[i]; p.role == matches.Goalkeeper {
				keeper[side] = s.effective(p, p.r.Goalkeeping)
			}
		}
		order := takers[side][:]
		// Insertion sort: stable, and at most eleven players.
		for a := 1; a < len(order); a++ {
			for b := a; b > 0 && s.finishing(t, order[b]) > s.finishing(t, order[b-1]); b-- {
				order[b], order[b-1] = order[b-1], order[b]
			}
		}
	}
	var goals [2]uint16
	kick := func(side, n int) {
		taker := &s.teams[side].players[takers[side][n%matches.StartersPerTeam]]
		c := s.p.ShootoutConversionPPM + (s.effective(taker, taker.r.Finishing)-keeper[1-side])*s.p.ShootoutSkillPPM/ratingUnits
		if int64(s.rng.IntN(ppm)) < min(max(c, s.p.MinShootoutPPM), s.p.MaxShootoutPPM) {
			goals[side]++
		}
	}
	const firstKicks = 5
	for n := range firstKicks {
		for side := range 2 {
			kick(side, n)
			// Kicks left after this one: the home side's remaining turns,
			// and the away side's, including this round's if home just kicked.
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

func (s *session) finishing(t *team, i int) int64 {
	return s.effective(&t.players[i], t.players[i].r.Finishing)
}
