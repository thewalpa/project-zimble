// Package medical owns players' physical condition: how fit each player is
// to play, how matches wear it down and how rest restores it.
//
// Condition is an integer percentage, 0..MaxCondition (100 is fully fit). Rules take the players' stamina as detached input, because
// attributes belong to the players module; this package imports no other
// domain module.
//
// Changes are two-step so an application workflow can commit several
// modules atomically: Plan* validates and computes new conditions without
// touching the store, and Apply commits a plan, which cannot fail for a plan
// made from the store's current state.
package medical

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// Version identifies DefaultParams and the rules below. Bump it whenever the
// same condition, stamina and exposure would produce a different condition.
const Version = 2

// MaxCondition is full fitness.
const MaxCondition uint8 = 100

// Stamina bounds match the players module's 1..100 rating scale.
const (
	MinStamina = 1
	MaxStamina = 100
)

// MaxMinutes bounds one match's exposure (regulation plus a margin for
// extra time later).
const MaxMinutes = 150

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("medical: plan is stale")

// Params are the condition rules. Condition is a whole number of points;
// rates are finer and each result is rounded half up once:
//
//   - A match costs minutes * (DrainBase + (MaxStamina-stamina)*DrainStaminaStep)
//     ten-thousandths of a point, never taking condition below MinCondition.
//   - A day of rest restores RecoveryBase + stamina*RecoveryStaminaStep
//     hundredths of a point, never above MaxCondition.
//
// With DefaultParams a player of stamina about 45 who plays 90 minutes every
// week holds level; fitter players recover fully and less fit ones decline.
type Params struct {
	MinCondition                      uint8
	DrainBase, DrainStaminaStep       int // per 10,000 of a point, per minute
	RecoveryBase, RecoveryStaminaStep int // per 100 of a point, per day
}

func DefaultParams() Params {
	return Params{
		MinCondition: 20,
		DrainBase:    2_000, DrainStaminaStep: 20, // stamina 30: 31 per 90'; 90: 20
		RecoveryBase: 300, RecoveryStaminaStep: 2, // stamina 30: 4 a day; 90: 5
	}
}

func (p Params) Validate() error {
	if p.MinCondition == 0 || p.MinCondition > MaxCondition || p.DrainBase < 0 || p.DrainStaminaStep < 0 ||
		p.RecoveryBase < 0 || p.RecoveryStaminaStep < 0 || p.RecoveryBase+MaxStamina*p.RecoveryStaminaStep > 100*int(MaxCondition) {
		return fmt.Errorf("medical: invalid params %+v", p)
	}
	return nil
}

// Drain returns the whole points a match of minutes costs a player of
// stamina, rounded half up, before the MinCondition floor.
func (p Params) Drain(minutes uint16, stamina uint8) int {
	return (int(minutes)*(p.DrainBase+(MaxStamina-int(stamina))*p.DrainStaminaStep) + 5_000) / 10_000
}

// Recovery returns the whole points one day of rest restores, rounded half
// up, before the MaxCondition cap.
func (p Params) Recovery(stamina uint8) int {
	return (p.RecoveryBase + int(stamina)*p.RecoveryStaminaStep + 50) / 100
}

// Record is one player's condition.
type Record struct {
	Player    ids.PlayerID
	Condition uint8
}

// Exposure is the minutes a player played in one match.
type Exposure struct {
	Player  ids.PlayerID
	Minutes uint16
	Stamina uint8
}

// Rest is a player's stamina for a recovery day.
type Rest struct {
	Player  ids.PlayerID
	Stamina uint8
}

// Plan is a validated set of new conditions. It is applied with Apply.
type Plan struct {
	generation uint64
	rows       []Record // ascending player; only changed players
}

// Changes returns the planned conditions, ascending by player.
func (p Plan) Changes() []Record { return slices.Clone(p.rows) }

// Store holds every player's condition. Queries return copies.
type Store struct {
	params     Params
	rows       []Record // ascending player
	generation uint64   // increments on every Apply; plans are tied to one
}

// New validates the records and params and returns a store holding its own
// copy. Every player may appear once, with condition in
// params.MinCondition..MaxCondition (the rules never leave that range).
func New(params Params, records []Record) (*Store, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	rows := slices.Clone(records)
	slices.SortFunc(rows, func(a, b Record) int { return cmp.Compare(a.Player, b.Player) })
	for i, r := range rows {
		if !r.Player.Valid() || (i > 0 && rows[i-1].Player == r.Player) {
			return nil, fmt.Errorf("medical: player ID %d invalid or duplicated", r.Player)
		}
		if r.Condition < params.MinCondition || r.Condition > MaxCondition {
			return nil, fmt.Errorf("medical: player %d condition %d outside %d..%d", r.Player, r.Condition, params.MinCondition, MaxCondition)
		}
	}
	return &Store{params: params, rows: rows}, nil
}

func (s *Store) Params() Params { return s.params }

func (s *Store) index(player ids.PlayerID) (int, bool) {
	return slices.BinarySearchFunc(s.rows, player, func(r Record, p ids.PlayerID) int { return cmp.Compare(r.Player, p) })
}

// Condition returns a player's condition.
func (s *Store) Condition(player ids.PlayerID) (uint8, bool) {
	i, ok := s.index(player)
	if !ok {
		return 0, false
	}
	return s.rows[i].Condition, true
}

// Records returns every player's condition in ascending player order.
func (s *Store) Records() []Record { return slices.Clone(s.rows) }

// Snapshot exports the store's authoritative state as a fresh copy.
// New(params, Snapshot()) restores an equivalent store.
func (s *Store) Snapshot() []Record { return s.Records() }

// PlanExposure computes the conditions after matches. Each player may appear
// once, must be known, and needs minutes 0..MaxMinutes and a valid stamina.
func (s *Store) PlanExposure(exposures []Exposure) (Plan, error) {
	ex := slices.Clone(exposures)
	slices.SortFunc(ex, func(a, b Exposure) int { return cmp.Compare(a.Player, b.Player) })
	plan := Plan{generation: s.generation}
	for i, e := range ex {
		if i > 0 && ex[i-1].Player == e.Player {
			return Plan{}, fmt.Errorf("medical: player %d exposed twice", e.Player)
		}
		if e.Minutes > MaxMinutes || e.Stamina < MinStamina || e.Stamina > MaxStamina {
			return Plan{}, fmt.Errorf("medical: player %d exposure %+v out of range", e.Player, e)
		}
		cur, ok := s.Condition(e.Player)
		if !ok {
			return Plan{}, fmt.Errorf("medical: unknown player %d", e.Player)
		}
		next := max(int(cur)-s.params.Drain(e.Minutes, e.Stamina), int(s.params.MinCondition))
		plan.rows = append(plan.rows, Record{Player: e.Player, Condition: uint8(next)})
	}
	return plan, nil
}

// PlanRecovery computes one day of rest for every player. rest must cover
// exactly the store's players, each once, with a valid stamina.
func (s *Store) PlanRecovery(rest []Rest) (Plan, error) {
	rs := slices.Clone(rest)
	slices.SortFunc(rs, func(a, b Rest) int { return cmp.Compare(a.Player, b.Player) })
	if len(rs) != len(s.rows) {
		return Plan{}, fmt.Errorf("medical: rest for %d players, store has %d", len(rs), len(s.rows))
	}
	plan := Plan{generation: s.generation, rows: make([]Record, 0, len(rs))}
	for i, r := range rs {
		if r.Player != s.rows[i].Player {
			return Plan{}, fmt.Errorf("medical: rest for player %d, expected %d", r.Player, s.rows[i].Player)
		}
		if r.Stamina < MinStamina || r.Stamina > MaxStamina {
			return Plan{}, fmt.Errorf("medical: player %d stamina %d out of range", r.Player, r.Stamina)
		}
		next := min(int(s.rows[i].Condition)+s.params.Recovery(r.Stamina), int(MaxCondition))
		plan.rows = append(plan.rows, Record{Player: r.Player, Condition: uint8(next)})
	}
	return plan, nil
}

// Apply commits a plan made from the store's current state. It fails only
// with ErrStalePlan, if the store changed after the plan was made; then
// nothing changes.
func (s *Store) Apply(p Plan) error {
	if p.generation != s.generation {
		return fmt.Errorf("%w: made at generation %d, store at %d", ErrStalePlan, p.generation, s.generation)
	}
	for _, r := range p.rows {
		i, _ := s.index(r.Player) // plans only hold known players
		s.rows[i].Condition = r.Condition
	}
	s.generation++
	return nil
}
