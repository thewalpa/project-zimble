// Package tick is a match engine that moves the ball and all 22 players
// across the pitch, five instants a second. Goals come out of play (passes,
// dribbles, tackles, shots and saves) rather than from a per-minute chance.
// It implements the same session contract as the simple engine and adds
// positional frames. See Params for the model.
package tick

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// EngineID names this engine in outcomes, checkpoints and stream domains.
const EngineID = "tick"

// MaxBench is the largest bench this engine accepts.
const MaxBench = 7

const maxSquad = matches.StartersPerTeam + MaxBench

// Engine starts tick sessions. It is immutable and safe to share.
type Engine struct {
	p Params
}

var _ matches.Engine = (*Engine)(nil)

// New returns an engine using p, typically DefaultParams().
func New(p Params) (*Engine, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &Engine{p: p}, nil
}

func (e *Engine) ID() string      { return EngineID }
func (e *Engine) Version() uint32 { return e.p.Version }

func (e *Engine) Capabilities() matches.Capabilities {
	return matches.Capabilities{Substitutions: true, Mentality: true, Penalties: true, PositionalFrames: true}
}

// Start validates and copies input; the session never references input's
// slices. random must come from matches.FixtureRandom (SplitMix64, current
// random.Version).
func (e *Engine) Start(input *matches.MatchInput, rs matches.RandomState) (matches.MatchSession, error) {
	if err := input.Validate(MaxBench); err != nil {
		return nil, err
	}
	if rs.Algorithm != matches.AlgorithmSplitMix64 || rs.Version != random.Version || rs.Words[1] != 0 || rs.Words[2] != 0 || rs.Words[3] != 0 {
		return nil, fmt.Errorf("%w: unsupported random state (algorithm %d, version %d)", matches.ErrInvalidInput, rs.Algorithm, rs.Version)
	}
	s := &session{
		p:       e.p,
		match:   input.Match,
		rules:   input.Rules,
		rng:     random.NewStream(rs.Words[0]),
		period:  matches.FirstHalf,
		goals:   make([]matches.Goal, 0, 16),
		pending: make([]matches.MatchEvent, 0, 8),
	}
	for _, side := range [...]matches.Side{matches.Home, matches.Away} {
		in := input.Team(side)
		t := &s.teams[side.Index()]
		t.id = in.Team
		t.mentality = in.Tactics.Mentality
		t.lastTouch = -1
		for i, p := range in.Starters {
			t.players[i] = player{id: p.Player, role: p.Role, r: p.Ratings, ready: e.readiness(p.Condition), state: onPitch, started: true}
			t.pitch[i] = uint8(i)
		}
		for i, p := range in.Bench {
			t.players[matches.StartersPerTeam+i] = player{id: p.Player, role: p.Role, r: p.Ratings, ready: e.readiness(p.Condition), state: onBench}
		}
		t.n = matches.StartersPerTeam + len(in.Bench)
		t.layOut()
	}
	s.refreshRatings()
	s.kickoff(matches.Home, true)
	return s, nil
}

// readiness is the per-10,000 multiplier for a kickoff condition.
func (e *Engine) readiness(condition uint8) int64 {
	floor := e.p.ConditionFloorPer10k
	return floor + (per10k-floor)*int64(condition)/matches.MaxCondition
}

// Restore is unsupported: this engine does not produce checkpoints yet.
func (e *Engine) Restore(matches.MatchCheckpoint) (matches.MatchSession, error) {
	return nil, fmt.Errorf("%w: %s engine has no checkpoints", matches.ErrUnsupported, EngineID)
}

type playerState uint8

const (
	onBench playerState = iota + 1
	onPitch
	substitutedOff
)

// Effective-rating slots, recomputed once a minute.
const (
	effGoalkeeping = iota
	effDefending
	effPassing
	effFinishing
	effPace
	effDribbling
	nEff
)

type player struct {
	id      ids.PlayerID
	role    matches.Role
	r       matches.Ratings
	ready   int64 // per 10,000, from condition at kickoff
	state   playerState
	started bool
	on, off uint16 // minutes; see matches.Participation

	eff    [nEff]int64 // effective ratings this minute
	sprint int64       // top speed this minute, cm per tick
	pos    vec
	// busy is the first tick at which the player may touch the ball
	// again: after a kick, a missed control or a lost challenge.
	busy uint32
	// drift moves his spot (depth, lateral in his side's frame) while his
	// side has the ball, until driftUntil, when it is drawn again.
	drift      vec
	driftUntil uint32
}

type team struct {
	id        ids.TeamID
	players   [maxSquad]player // starters in slot order, then bench
	n         int
	pitch     [matches.StartersPerTeam]uint8 // indices into players, by slot
	lateral   [matches.StartersPerTeam]int64 // width of each slot's spot
	entered   [MaxBench]uint8                // substitutes in the order they came on
	nEntered  int
	subs      uint8
	mentality matches.Mentality
	lastTouch int    // players index of the side's latest touch, or -1
	level     levels // this minute, from the players on the pitch
}

// levels are the effective ratings a side's opponents meet in contests:
// its outfield averages and its keeper's.
type levels struct {
	defending, passing, pace, goalkeeping int64
}

func (t *team) find(id ids.PlayerID) int {
	for i := range t.n {
		if t.players[i].id == id {
			return i
		}
	}
	return -1
}

func (t *team) slotOf(idx int) int {
	for slot, i := range t.pitch {
		if int(i) == idx {
			return slot
		}
	}
	return -1
}

func (t *team) at(slot int) *player { return &t.players[t.pitch[slot]] }

// layOut spreads each line's players evenly across the width, in slot
// order.
func (t *team) layOut() {
	var count, seen [5]int64
	for slot := range t.pitch {
		count[t.at(slot).role]++
	}
	for slot := range t.pitch {
		r := t.at(slot).role
		seen[r]++
		t.lateral[slot] = pitchW * seen[r] / (count[r] + 1)
	}
}

// restartKind is how dead-ball play resumes.
type restartKind uint8

const (
	restartKickoff restartKind = iota + 1
	restartThrowIn
	restartCorner
	restartGoalKick
)

// restart is a dead ball waiting for its taker.
type restart struct {
	kind      restartKind
	side      int // team index taking it
	slot      int
	spot      vec
	notBefore uint32
	timeout   uint32
}

// ball is the ball's state. While carried it moves with its carrier.
type ball struct {
	pos, vel vec
	decel    int64 // cm/tick lost per tick

	carried      bool
	side, slot   int   // carrier, while carried; the kicker's side and slot after a kick
	shot         bool  // loose after a shot
	finishing    int64 // the shooter's effective Finishing, while shot
	passTo       int   // receiving slot of a pass in flight, or -1
	aim          vec
	protect      uint32 // no tackle before this tick
	free         uint32 // no control before this tick
	setPiece     bool   // the carrier must pass: a restart was just taken
	setPieceFrom uint32
}

// stats are counters for tests and tuning; not part of the outcome yet.
type stats struct {
	shots, onTarget, passes, completed, tackles, saves [2]int
	possession                                         [2]int // ticks with the ball
}

// session owns all match state. It is not safe for concurrent use.
type session struct {
	p          Params
	match      ids.FixtureID
	rules      matches.Rules
	rng        *random.Stream
	teams      [2]team
	period     matches.Period
	minute     uint16
	tick       uint32 // ticks played since kickoff
	secondHalf bool
	ball       ball
	dead       bool // the ball is out of play; see restart
	restart    restart
	lastSide   int // side of the latest touch
	score      [2]uint16
	pens       [2]uint16
	decided    bool
	seq        uint32
	goals      []matches.Goal
	pending    []matches.MatchEvent
	stats      stats
}

func (s *session) finished() bool { return s.period == matches.FullTime }

func (s *session) nextSeq() uint32 { s.seq++; return s.seq }

// Advance simulates minutes up to request.ToMinute, stopping at half time
// and full time. After full time it refills dst with the final state and no
// events.
func (s *session) Advance(req matches.AdvanceRequest, dst *matches.MatchStepResult) error {
	if dst == nil {
		return fmt.Errorf("%w: nil destination", matches.ErrInvalidRequest)
	}
	if req.ToMinute > matches.RegulationMinutes || req.ToMinute < s.minute {
		return fmt.Errorf("%w: minute %d with session at %d", matches.ErrInvalidRequest, req.ToMinute, s.minute)
	}

	dst.Events = append(dst.Events[:0], s.pending...)
	dst.Frames = dst.Frames[:0]
	s.pending = s.pending[:0]
	if s.period == matches.HalfTime && req.ToMinute > s.minute {
		s.period, s.secondHalf = matches.SecondHalf, true
		s.kickoff(matches.Away, true)
	}
	for !s.finished() && s.period != matches.HalfTime && s.minute < req.ToMinute {
		s.minute++
		s.refreshRatings()
		for range TicksPerMinute {
			s.step(dst)
			if req.Frames {
				dst.Frames = append(dst.Frames, s.frame())
			}
		}
		switch s.minute {
		case matches.HalfTimeMinute:
			s.endPeriod(dst, matches.FirstHalf, matches.HalfTime)
		case matches.RegulationMinutes:
			s.endPeriod(dst, matches.SecondHalf, matches.FullTime)
			if s.rules.Knockout && s.score[0] == s.score[1] {
				s.pens, s.decided = s.shootout(), true
			}
		}
	}
	s.fill(dst)
	return nil
}

func (s *session) endPeriod(dst *matches.MatchStepResult, ended, next matches.Period) {
	dst.Events = append(dst.Events, matches.MatchEvent{Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventPeriodEnd, Period: ended})
	s.period = next
}

func (s *session) frame() matches.Frame {
	f := matches.Frame{Millis: s.tick * tickMillis, Ball: s.ball.pos.point()}
	if s.ball.carried && !s.dead {
		f.Carrier = s.teams[s.ball.side].at(s.ball.slot).id
	}
	for side := range s.teams {
		for slot := range matches.StartersPerTeam {
			f.Players[side][slot] = s.teams[side].at(slot).pos.point()
		}
	}
	return f
}

// fill writes position, view and outcome, reusing dst's buffers.
func (s *session) fill(dst *matches.MatchStepResult) {
	dst.Position = matches.MatchPosition{Period: s.period, Minute: s.minute}
	switch s.period {
	case matches.FullTime:
		dst.Status, dst.Stop = matches.MatchFinished, matches.StopFullTime
	case matches.HalfTime:
		dst.Status, dst.Stop = matches.MatchDecisionRequired, matches.StopHalfTime
	default:
		dst.Status, dst.Stop = matches.MatchRunning, matches.StopTarget
	}
	dst.View = matches.MatchView{Score: s.score, Shootout: s.pens}
	for side := range s.teams {
		t := &s.teams[side]
		dst.View.Mentality[side] = t.mentality
		dst.View.SubstitutionsUsed[side] = t.subs
		for slot := range t.pitch {
			dst.View.OnPitch[side][slot] = t.at(slot).id
		}
	}

	o := &dst.Outcome
	goals, parts := o.Goals[:0], o.Participants[:0]
	*o = matches.MatchOutcome{
		Match: s.match, Status: matches.ResultPending, Resolution: matches.ResolutionRegulation,
		EngineID: EngineID, EngineVersion: s.p.Version, Goals: goals, Participants: parts,
	}
	if !s.finished() {
		return
	}
	o.Status = matches.ResultCompleted
	o.Score = s.score
	if s.decided {
		o.Resolution, o.Shootout = matches.ResolutionPenalties, s.pens
	}
	o.Goals = append(o.Goals, s.goals...)
	for side := range s.teams {
		t := &s.teams[side]
		add := func(p *player) {
			off := p.off
			if p.state == onPitch {
				off = matches.RegulationMinutes
			}
			o.Participants = append(o.Participants, matches.Participation{
				Player: p.id, Side: matches.Side(side + 1), Started: p.started, OnMinute: p.on, OffMinute: off,
			})
		}
		for i := range matches.StartersPerTeam {
			add(&t.players[i])
		}
		for _, i := range t.entered[:t.nEntered] {
			add(&t.players[i])
		}
	}
}

// Apply validates cmd completely before changing anything. A substitute
// takes the place, and the ball if he had it, of the player he replaces.
func (s *session) Apply(cmd matches.MatchCommand) error {
	if s.finished() {
		return matches.ErrMatchFinished
	}
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{matches.ErrInvalidCommand}, args...)...)
	}
	if !cmd.Side.Valid() {
		return invalid("side %d", cmd.Side)
	}
	t := &s.teams[cmd.Side.Index()]
	switch cmd.Kind {
	case matches.CommandSetMentality:
		if !cmd.Mentality.Valid() {
			return invalid("mentality %d", cmd.Mentality)
		}
		if cmd.Mentality == t.mentality {
			return invalid("%s mentality is already %s", cmd.Side, cmd.Mentality)
		}
		t.mentality = cmd.Mentality
		s.pending = append(s.pending, matches.MatchEvent{
			Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventMentalityChange, Side: cmd.Side, Mentality: cmd.Mentality,
		})
		return nil

	case matches.CommandSubstitute:
		if s.minute == 0 {
			return invalid("substitutions start after kickoff; change the lineup instead")
		}
		if t.subs >= s.rules.MaxSubstitutions {
			return invalid("%s has used all %d substitutions", cmd.Side, s.rules.MaxSubstitutions)
		}
		out, in := t.find(cmd.Out), t.find(cmd.In)
		if out < 0 || t.players[out].state != onPitch {
			return invalid("player %d is not on the pitch for %s", cmd.Out, cmd.Side)
		}
		if in < 0 || t.players[in].state != onBench {
			return invalid("player %d is not an unused %s substitute", cmd.In, cmd.Side)
		}
		if (t.players[out].role == matches.Goalkeeper) != (t.players[in].role == matches.Goalkeeper) {
			return invalid("goalkeepers must be replaced by goalkeepers")
		}
		slot := t.slotOf(out)
		t.players[out].state, t.players[out].off = substitutedOff, s.minute
		t.players[in].state, t.players[in].on = onPitch, s.minute
		t.players[in].pos, t.players[in].busy = t.players[out].pos, t.players[out].busy
		t.pitch[slot] = uint8(in)
		t.entered[t.nEntered] = uint8(in)
		t.nEntered++
		t.subs++
		if t.lastTouch == out {
			t.lastTouch = -1
		}
		t.layOut()
		s.rate(cmd.Side.Index(), in)
		s.measure(cmd.Side.Index())
		s.setPace(0)
		s.setPace(1)
		s.pending = append(s.pending, matches.MatchEvent{
			Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventSubstitution, Side: cmd.Side, Player: cmd.In, Other: cmd.Out,
		})
		return nil
	}
	return invalid("unknown command kind %d", cmd.Kind)
}

// Checkpoint is unsupported; see Capabilities.
func (s *session) Checkpoint() (matches.MatchCheckpoint, error) {
	return matches.MatchCheckpoint{}, fmt.Errorf("%w: %s engine has no checkpoints", matches.ErrUnsupported, EngineID)
}

// refreshRatings recomputes every player's effective ratings, each side's
// levels and the sprint speeds for the minute about to be played
// (s.minute, already incremented).
func (s *session) refreshRatings() {
	for side := range s.teams {
		for i := range s.teams[side].n {
			s.rate(side, i)
		}
		s.measure(side)
	}
	s.setPace(0)
	s.setPace(1)
}

// measure sets side's levels from the effective ratings of its players on
// the pitch.
func (s *session) measure(side int) {
	t := &s.teams[side]
	var sum levels
	n := int64(0)
	for slot := range t.pitch {
		p := t.at(slot)
		if p.role == matches.Goalkeeper {
			sum.goalkeeping = p.eff[effGoalkeeping]
			continue
		}
		sum.defending += p.eff[effDefending]
		sum.passing += p.eff[effPassing]
		sum.pace += p.eff[effPace]
		n++
	}
	sum.defending /= n
	sum.passing /= n
	sum.pace /= n
	t.level = sum
}

// against is the contest rating of skill meeting an opposing skill or
// level: ContestReference plus ContestPermille of the difference, within
// [0, per10k].
func (s *session) against(skill, opposing int64) int64 {
	return min(max(s.p.ContestReference+(skill-opposing)*s.p.ContestPermille/permille, 0), per10k)
}

// rate sets one player's effective ratings. Minutes already played this
// match (before the current one) drive fatigue.
func (s *session) rate(side, i int) {
	p := &s.teams[side].players[i]
	played := max(int64(s.minute)-1-int64(p.on), 0)
	if p.state != onPitch {
		played = 0
	}
	perMinute := max(s.p.FatigueBasePer100k-int64(p.r.Stamina)*s.p.FatigueStaminaStepPer100k, 0)
	fatigue := min(played*perMinute, s.p.FatigueCapPer100k)
	home := int64(0)
	if side == matches.Home.Index() {
		home = s.p.HomeAdvantage
	}
	for k, v := range [nEff]uint8{p.r.Goalkeeping, p.r.Defending, p.r.Passing, p.r.Finishing, p.r.Pace, p.r.Dribbling} {
		p.eff[k] = int64(v)*pointUnits*p.ready/per10k*(per100k-fatigue)/per100k + home
	}
}

// setPace sets side's sprint speeds from its players' Pace against the
// opponents' average.
func (s *session) setPace(side int) {
	t := &s.teams[side]
	for i := range t.n {
		p := &t.players[i]
		p.sprint = s.p.MinSprint + (s.p.MaxSprint-s.p.MinSprint)*s.against(p.eff[effPace], s.teams[1-side].level.pace)/per10k
	}
}
