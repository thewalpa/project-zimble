// Package medical owns players' physical condition: how fit each player is
// to play, how matches wear it down and how rest restores it, and their
// injuries.
//
// Condition is an integer percentage, 0..MaxCondition (100 is fully fit). An
// injured player has DaysOut > 0 recovery days left; each daily recovery
// takes one off. Rules take the players' stamina as detached input, because
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
	"github.com/thewalpa/project-zimble/internal/core/random"
)

// Version identifies DefaultParams and the rules below. Bump it whenever the
// same condition, stamina and exposure would produce a different condition
// or injury.
const Version = 4

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

// MaxInjuryDays bounds one injury (the longest layoff, in recovery days).
const MaxInjuryDays = 180

// ErrStalePlan: the store changed after the plan was made.
var ErrStalePlan = errors.New("medical: plan is stale")

// Params are the condition rules. Condition is a whole number of points;
// rates are finer and each result is rounded half up once:
//
//   - A match costs minutes * (DrainBase + (MaxStamina-stamina)*DrainStaminaStep)
//     ten-thousandths of a point, never taking condition below MinCondition.
//   - A day of rest restores RecoveryBase + stamina*RecoveryStaminaStep
//     hundredths of a point, never above MaxCondition.
//   - A player exposed to a match for minutes is injured with probability
//     minutes * (InjuryBase + (MaxCondition-condition)*InjuryFatigueStep)
//     parts per million, before the match; the tired get hurt more. The
//     layoff is drawn from three classes by permille: minor (MinorPermille,
//     MinorDays), moderate (ModeratePermille, ModerateDays) and, for the
//     rest, serious (SeriousDays); days are inclusive ranges.
//
// With DefaultParams a player who plays 90 minutes every week holds level
// at stamina 69 and above, and the less fit lose a few points a week, so a
// team that never rotates its starters runs tired. Stamina acts through the
// drain, which is rounded once a match; a day of rest restores the same three
// points to everyone (a stamina step would round to a cliff between two whole
// points a day).
type Params struct {
	MinCondition                      uint8
	DrainBase, DrainStaminaStep       int // per 10,000 of a point, per minute
	RecoveryBase, RecoveryStaminaStep int // per 100 of a point, per day

	InjuryBase, InjuryFatigueStep        int // ppm per minute; extra ppm per minute per point of missing condition
	MinorPermille, ModeratePermille      int
	MinorDays, ModerateDays, SeriousDays [2]int
}

func DefaultParams() Params {
	return Params{
		MinCondition: 20,
		DrainBase:    1_500, DrainStaminaStep: 28, // stamina 30: 31 per 90'; 50: 26; 70: 21; 90: 16
		RecoveryBase: 300, RecoveryStaminaStep: 0, // 3 a day, 21 a week

		// A fit player risks about 5.9% a match, one at condition 90 about
		// 8.6%: roughly half an injury a player a season of 14 weekly rounds.
		InjuryBase: 650, InjuryFatigueStep: 30,
		MinorPermille: 500, ModeratePermille: 350, // 50% minor, 35% moderate, 15% serious; about 20 days on average
		MinorDays: [2]int{2, 7}, ModerateDays: [2]int{8, 28}, SeriousDays: [2]int{29, 120},
	}
}

func (p Params) Validate() error {
	if p.MinCondition == 0 || p.MinCondition > MaxCondition || p.DrainBase < 0 || p.DrainStaminaStep < 0 ||
		p.RecoveryBase < 0 || p.RecoveryStaminaStep < 0 || p.RecoveryBase+MaxStamina*p.RecoveryStaminaStep > 100*int(MaxCondition) {
		return fmt.Errorf("medical: invalid params %+v", p)
	}
	if p.InjuryBase < 0 || p.InjuryFatigueStep < 0 || MaxMinutes*(p.InjuryBase+int(MaxCondition)*p.InjuryFatigueStep) > 1_000_000 ||
		p.MinorPermille < 0 || p.ModeratePermille < 0 || p.MinorPermille+p.ModeratePermille > 1000 {
		return fmt.Errorf("medical: invalid injury params %+v", p)
	}
	for _, d := range [][2]int{p.MinorDays, p.ModerateDays, p.SeriousDays} {
		if d[0] < 1 || d[1] < d[0] || d[1] > MaxInjuryDays {
			return fmt.Errorf("medical: invalid injury days %v", d)
		}
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

// Record is one player's condition and injury. DaysOut is the number of
// daily recoveries until he is fit again; zero means not injured.
type Record struct {
	Player    ids.PlayerID
	Condition uint8
	DaysOut   uint16
}

// Injury is a new injury: the player is out for Days recovery days.
type Injury struct {
	Player ids.PlayerID
	Days   uint16
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
	rows       []Record // ascending player; only changed players, or every player if replace
	replace    bool     // rows is the complete new record set (PlanRoster)
	injured    []Injury
	recovered  []ids.PlayerID
}

// Changes returns the planned records, ascending by player.
func (p Plan) Changes() []Record { return slices.Clone(p.rows) }

// Injured returns the injuries the plan starts, ascending by player.
func (p Plan) Injured() []Injury { return slices.Clone(p.injured) }

// Recovered returns the players whose last injury day the plan takes,
// ascending.
func (p Plan) Recovered() []ids.PlayerID { return slices.Clone(p.recovered) }

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
		if r.DaysOut > MaxInjuryDays {
			return nil, fmt.Errorf("medical: player %d out for %d days, max %d", r.Player, r.DaysOut, MaxInjuryDays)
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

// DaysOut returns the recovery days left of a player's injury, and whether he
// is injured (false, with zero days, for a fit or unknown player).
func (s *Store) DaysOut(player ids.PlayerID) (uint16, bool) {
	i, ok := s.index(player)
	if !ok || s.rows[i].DaysOut == 0 {
		return 0, false
	}
	return s.rows[i].DaysOut, true
}

// Records returns every player's condition in ascending player order.
func (s *Store) Records() []Record { return slices.Clone(s.rows) }

// Snapshot exports the store's authoritative state as a fresh copy.
// New(params, Snapshot()) restores an equivalent store.
func (s *Store) Snapshot() []Record { return s.Records() }

// Roll draws the injuries of one match from rng, which the caller derives
// per match. Every exposure, in ascending player order, takes one draw for
// whether he is hurt and, if so, more for the layoff, so the result depends
// only on the store, the exposures and the stream. A player already injured
// is not hurt again. Roll validates like PlanExposure and changes nothing.
func (s *Store) Roll(exposures []Exposure, rng *random.Stream) ([]Injury, error) {
	ex := slices.Clone(exposures)
	slices.SortFunc(ex, func(a, b Exposure) int { return cmp.Compare(a.Player, b.Player) })
	var out []Injury
	for i, e := range ex {
		if i > 0 && ex[i-1].Player == e.Player {
			return nil, fmt.Errorf("medical: player %d exposed twice", e.Player)
		}
		if e.Minutes > MaxMinutes {
			return nil, fmt.Errorf("medical: player %d exposure %+v out of range", e.Player, e)
		}
		j, ok := s.index(e.Player)
		if !ok {
			return nil, fmt.Errorf("medical: unknown player %d", e.Player)
		}
		r := s.rows[j]
		ppm := int(e.Minutes) * (s.params.InjuryBase + int(MaxCondition-r.Condition)*s.params.InjuryFatigueStep)
		if rng.IntN(1_000_000) >= ppm || r.DaysOut > 0 {
			continue
		}
		days := s.params.SeriousDays
		switch class := rng.IntN(1000); {
		case class < s.params.MinorPermille:
			days = s.params.MinorDays
		case class < s.params.MinorPermille+s.params.ModeratePermille:
			days = s.params.ModerateDays
		}
		out = append(out, Injury{Player: e.Player, Days: uint16(rng.IntRange(days[0], days[1]))})
	}
	return out, nil
}

// PlanExposure computes the records after matches. Each player may appear
// once, must be known, and needs minutes 0..MaxMinutes and a valid stamina.
// injuries (from Roll, possibly filtered) start layoffs: each player once,
// known and not already injured, for 1..MaxInjuryDays days.
func (s *Store) PlanExposure(exposures []Exposure, injuries []Injury) (Plan, error) {
	ex := slices.Clone(exposures)
	slices.SortFunc(ex, func(a, b Exposure) int { return cmp.Compare(a.Player, b.Player) })
	inj := slices.Clone(injuries)
	slices.SortFunc(inj, func(a, b Injury) int { return cmp.Compare(a.Player, b.Player) })
	plan := Plan{generation: s.generation, injured: inj}
	for i, x := range inj {
		j, ok := s.index(x.Player)
		switch {
		case !ok:
			return Plan{}, fmt.Errorf("medical: injury for unknown player %d", x.Player)
		case i > 0 && inj[i-1].Player == x.Player:
			return Plan{}, fmt.Errorf("medical: player %d injured twice", x.Player)
		case s.rows[j].DaysOut > 0:
			return Plan{}, fmt.Errorf("medical: player %d is already injured", x.Player)
		case x.Days == 0 || x.Days > MaxInjuryDays:
			return Plan{}, fmt.Errorf("medical: injury of %d days for player %d", x.Days, x.Player)
		}
	}
	for i, e := range ex {
		if i > 0 && ex[i-1].Player == e.Player {
			return Plan{}, fmt.Errorf("medical: player %d exposed twice", e.Player)
		}
		if e.Minutes > MaxMinutes || e.Stamina < MinStamina || e.Stamina > MaxStamina {
			return Plan{}, fmt.Errorf("medical: player %d exposure %+v out of range", e.Player, e)
		}
		j, ok := s.index(e.Player)
		if !ok {
			return Plan{}, fmt.Errorf("medical: unknown player %d", e.Player)
		}
		next := max(int(s.rows[j].Condition)-s.params.Drain(e.Minutes, e.Stamina), int(s.params.MinCondition))
		row := Record{Player: e.Player, Condition: uint8(next), DaysOut: s.rows[j].DaysOut}
		if k, hurt := slices.BinarySearchFunc(inj, e.Player, func(x Injury, p ids.PlayerID) int { return cmp.Compare(x.Player, p) }); hurt {
			row.DaysOut = inj[k].Days
		}
		plan.rows = append(plan.rows, row)
	}
	for _, x := range inj {
		if _, exposed := slices.BinarySearchFunc(ex, x.Player, func(e Exposure, p ids.PlayerID) int { return cmp.Compare(e.Player, p) }); !exposed {
			return Plan{}, fmt.Errorf("medical: injury for player %d, who played no match", x.Player)
		}
	}
	return plan, nil
}

// PlanRecovery computes one day of rest for every player, which is also a day
// off every injury. rest must cover
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
		days := s.rows[i].DaysOut
		if days > 0 {
			days--
			if days == 0 {
				plan.recovered = append(plan.recovered, r.Player)
			}
		}
		plan.rows = append(plan.rows, Record{Player: r.Player, Condition: uint8(next), DaysOut: days})
	}
	return plan, nil
}

// PlanRoster adds records for new players, fully fit, and removes the
// records of players who leave the game (retire). New players must be
// unknown, departing ones known; each appears once.
func (s *Store) PlanRoster(admit, discharge []ids.PlayerID) (Plan, error) {
	gone := map[ids.PlayerID]bool{}
	for _, id := range discharge {
		if _, ok := s.index(id); !ok || gone[id] {
			return Plan{}, fmt.Errorf("medical: cannot discharge player %d: unknown or listed twice", id)
		}
		gone[id] = true
	}
	var rows []Record
	for _, r := range s.rows {
		if !gone[r.Player] {
			rows = append(rows, r)
		}
	}
	for _, id := range admit {
		if _, known := s.index(id); known || !id.Valid() {
			return Plan{}, fmt.Errorf("medical: cannot admit player %d: invalid or already known", id)
		}
		rows = append(rows, Record{Player: id, Condition: MaxCondition})
	}
	slices.SortFunc(rows, func(a, b Record) int { return cmp.Compare(a.Player, b.Player) })
	for i := 1; i < len(rows); i++ {
		if rows[i].Player == rows[i-1].Player {
			return Plan{}, fmt.Errorf("medical: player %d admitted twice", rows[i].Player)
		}
	}
	return Plan{generation: s.generation, rows: rows, replace: true}, nil
}

// Apply commits a plan made from the store's current state. It fails only
// with ErrStalePlan, if the store changed after the plan was made; then
// nothing changes.
func (s *Store) Apply(p Plan) error {
	if p.generation != s.generation {
		return fmt.Errorf("%w: made at generation %d, store at %d", ErrStalePlan, p.generation, s.generation)
	}
	if p.replace {
		s.rows = slices.Clone(p.rows)
		s.generation++
		return nil
	}
	for _, r := range p.rows {
		i, _ := s.index(r.Player) // plans only hold known players
		s.rows[i] = r
	}
	s.generation++
	return nil
}
