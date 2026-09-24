// Package simple is the first match engine: each minute, each side may
// create a chance from team strengths, mentality and fatigue, and a chance
// may become a goal. Regulation time only. See Params for the model.
package simple

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// EngineID names this engine in outcomes, checkpoints and stream domains.
const EngineID = "simple"

// MaxBench is the largest bench this engine accepts.
const MaxBench = 7

const maxSquad = matches.StartersPerTeam + MaxBench

// Engine starts simple sessions. It is immutable and safe to share.
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
	return matches.Capabilities{Substitutions: true, Mentality: true}
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
		for i, p := range in.Starters {
			t.players[i] = player{id: p.Player, role: p.Role, r: p.Ratings, state: onPitch, started: true}
			t.pitch[i] = uint8(i)
		}
		for i, p := range in.Bench {
			t.players[matches.StartersPerTeam+i] = player{id: p.Player, role: p.Role, r: p.Ratings, state: onBench}
		}
		t.n = matches.StartersPerTeam + len(in.Bench)
	}
	return s, nil
}

// Restore is unsupported: this engine does not produce checkpoints.
func (e *Engine) Restore(matches.MatchCheckpoint) (matches.MatchSession, error) {
	return nil, fmt.Errorf("%w: %s engine has no checkpoints", matches.ErrUnsupported, EngineID)
}

type playerState uint8

const (
	onBench playerState = iota + 1
	onPitch
	substitutedOff
)

type player struct {
	id      ids.PlayerID
	role    matches.Role
	r       matches.Ratings
	state   playerState
	started bool
	on, off uint16 // minutes; see matches.Participation
}

type team struct {
	id        ids.TeamID
	players   [maxSquad]player // starters in slot order, then bench
	n         int
	pitch     [matches.StartersPerTeam]uint8 // indices into players, by slot
	entered   [MaxBench]uint8                // substitutes in the order they came on
	nEntered  int
	subs      uint8
	mentality matches.Mentality
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

// session owns all match state. It is not safe for concurrent use.
type session struct {
	p       Params
	match   ids.FixtureID
	rules   matches.Rules
	rng     *random.Stream
	teams   [2]team
	period  matches.Period
	minute  uint16
	score   [2]uint16
	seq     uint32
	goals   []matches.Goal
	pending []matches.MatchEvent // command events for the next Advance
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
	s.pending = s.pending[:0]
	if s.period == matches.HalfTime && req.ToMinute > s.minute {
		s.period = matches.SecondHalf
	}
	for !s.finished() && s.period != matches.HalfTime && s.minute < req.ToMinute {
		s.minute++
		s.playMinute(dst)
		switch s.minute {
		case matches.HalfTimeMinute:
			s.endPeriod(dst, matches.FirstHalf, matches.HalfTime)
		case matches.RegulationMinutes:
			s.endPeriod(dst, matches.SecondHalf, matches.FullTime)
		}
	}
	s.fill(dst)
	return nil
}

func (s *session) endPeriod(dst *matches.MatchStepResult, ended, next matches.Period) {
	dst.Events = append(dst.Events, matches.MatchEvent{Seq: s.nextSeq(), Minute: s.minute, Kind: matches.EventPeriodEnd, Period: ended})
	s.period = next
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
	dst.View = matches.MatchView{Score: s.score}
	for side := range s.teams {
		t := &s.teams[side]
		dst.View.Mentality[side] = t.mentality
		dst.View.SubstitutionsUsed[side] = t.subs
		for slot, i := range t.pitch {
			dst.View.OnPitch[side][slot] = t.players[i].id
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

// Apply validates cmd completely before changing anything.
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
		t.pitch[slot] = uint8(in)
		t.entered[t.nEntered] = uint8(in)
		t.nEntered++
		t.subs++
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
