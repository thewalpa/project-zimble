package app

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

// taskPlayerYear is the yearly player step (see playerYear). It is due at
// 00:00 on the eve of each contract-year end (30 June by default), so clubs
// and the manager decide contracts on the new abilities. It has no payload
// and reschedules itself a year later, so exactly one is always queued, due
// at the next player-year instant. It runs in the Expiries phase, before the
// day's recovery and wages.
const taskPlayerYear sim.TaskKind = 6

// playerYearAfter returns the first player-year instant strictly after t:
// the day before a contract-year end.
func (w *World) playerYearAfter(t sim.GameInstant) (sim.GameInstant, error) {
	end, err := w.contractYearEnd(t)
	if err != nil {
		return 0, err
	}
	if p := end - sim.GameInstant(sim.Day); p > t {
		return p, nil
	}
	end, err = w.addYears(end, 1)
	return end - sim.GameInstant(sim.Day), err
}

func (w *World) schedulePlayerYear(at sim.GameInstant) error {
	if _, err := w.scheduler.Schedule(sim.TaskSpec{DueAt: at, Phase: sim.PhaseExpiries, Kind: taskPlayerYear}); err != nil {
		return fmt.Errorf("app: schedule player year at %d: %w", at, err)
	}
	return nil
}

// age returns a registered player's age in whole years at t.
func (w *World) age(player ids.PlayerID, t sim.GameInstant) (int, error) {
	p, ok := w.registry.Player(player)
	if !ok {
		return 0, fmt.Errorf("app: player %d is not registered", player)
	}
	return w.calendar.WholeYears(p.Born, t)
}

// playerYear handles the yearly player step at `at`, as one all-or-nothing
// change:
//
//  1. Every active player, in ID order, either retires (players.Retires, by
//     their age at `at` and whether a club employs them) or develops
//     (players.Develop). A retiring player's contract ends; they leave the
//     game for good, keeping their identity and final profile.
//  2. Each club replaces every player of its own who retired with a youth
//     player at the same position (worldgen.Youth), club by club in ID
//     order, retirees in ID order, with new IDs from the registry's
//     allocator. So every squad keeps its size per position and stays legal.
//     Then each AI club also fills its vacancies (positions below the roster
//     count, left by sales the free agents could not replace) with youth
//     players, in roster order. So every AI squad is full again before the
//     contract year, and its free agents can always refill every club (see
//     contractYear), however many players the manager keeps. A youth
//     contract runs Youth.ContractYears contract years from the coming
//     contract-year end, at the player's demand.
//  3. Every module plans its part, next year's task is queued, then every
//     plan is applied and the events are emitted: one PlayersDeveloped, then
//     one PlayerRetired per retirement, then one YouthJoined per youth.
func (w *World) playerYear(at sim.GameInstant, cohort []sim.Task) error {
	if len(cohort) != 1 || cohort[0].PayloadID != 0 {
		return fmt.Errorf("app: player-year cohort at %d has %d tasks, first payload %d", at, len(cohort), cohort[0].PayloadID)
	}
	year, err := w.yearOf(at)
	if err != nil {
		return err
	}
	var (
		profiles  players.Changes
		jobs      employment.Changes
		developed []events.Development
		retired   []events.PlayerRetired
	)
	for _, id := range w.players.PlayerIDs() {
		p, _ := w.players.Profile(id)
		if p.Retired {
			continue
		}
		age, err := w.age(id, at)
		if err != nil {
			return err
		}
		a, employed := w.employment.Assignment(id)
		if players.Retires(w.seed, id, age, year, employed) {
			profiles.Retirements = append(profiles.Retirements, id)
			retired = append(retired, events.PlayerRetired{Player: id, Club: a.Club, Team: a.Team, Age: uint8(age)})
			if employed {
				jobs.Departures = append(jobs.Departures, id)
			}
			continue
		}
		next := p
		next.Attributes = players.Develop(w.seed, p, age, year)
		profiles.Developments = append(profiles.Developments, players.Development{Player: id, Attributes: next.Attributes})
		developed = append(developed, events.Development{Player: id, Team: a.Team, Before: uint8(p.Overall()), After: uint8(next.Overall())})
	}

	end, err := w.contractYearEnd(at)
	if err != nil {
		return err
	}
	expires, err := w.addYears(end, w.defs.Youth.ContractYears)
	if err != nil {
		return err
	}
	var identities []registry.Player
	var joined []ids.PlayerID
	nextID := w.registry.LastPlayer()
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		var intake []players.Position
		for _, r := range retired {
			if r.Club == c.ID {
				old, _ := w.players.Profile(r.Player)
				intake = append(intake, old.Position)
			}
		}
		if c.ID != w.userClub {
			counts := w.squadCounts(team) // retirees are replaced one for one
			for _, q := range w.defs.Roster {
				for n := counts[q.Position]; n < q.Count; n++ {
					intake = append(intake, q.Position)
				}
			}
		}
		for _, pos := range intake {
			nextID++
			identity, profile, err := worldgen.Youth(w.defs, w.seed, nextID, pos, at, c.Nation)
			if err != nil {
				return err
			}
			identities = append(identities, identity)
			profiles.Additions = append(profiles.Additions, profile)
			jobs.Signings = append(jobs.Signings, employment.Assignment{
				Player: nextID, Club: c.ID, Team: team,
				Contract: employment.Contract{Expires: expires, WeeklyWage: w.defs.Economy.Demand(profile.Overall())},
			})
			joined = append(joined, nextID)
		}
	}

	registryPlan, err := w.registry.PlanPlayers(identities)
	if err != nil {
		return fmt.Errorf("app: player year %d: %w", year, err)
	}
	profilePlan, err := w.players.Plan(profiles)
	if err != nil {
		return fmt.Errorf("app: player year %d: %w", year, err)
	}
	jobPlan, err := w.employment.Plan(jobs)
	if err != nil {
		return fmt.Errorf("app: player year %d: %w", year, err)
	}
	medicalPlan, err := w.medical.PlanRoster(joined, profiles.Retirements)
	if err != nil {
		return fmt.Errorf("app: player year %d: %w", year, err)
	}
	next, err := w.playerYearAfter(at)
	if err != nil {
		return err
	}
	if err := w.schedulePlayerYear(next); err != nil {
		return err
	}

	// Committed. Nothing below can fail.
	if err := w.registry.Apply(registryPlan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	if err := w.players.Apply(profilePlan); err != nil {
		panic(fmt.Sprintf("app: unreachable: %v", err))
	}
	w.applyEmployment(jobPlan)
	w.applyMedical(medicalPlan)

	cause := taskCause(cohort[0].ID)
	if len(developed) > 0 {
		w.emit(at, cause, events.Event{Kind: events.KindPlayersDeveloped, PlayersDeveloped: &events.PlayersDeveloped{Players: developed}})
	}
	for i := range retired {
		w.emit(at, cause, events.Event{Kind: events.KindPlayerRetired, PlayerRetired: &retired[i]})
	}
	for _, s := range jobs.Signings {
		w.emit(at, cause, events.Event{Kind: events.KindYouthJoined, YouthJoined: &events.YouthJoined{
			Player: s.Player, Club: s.Club, Team: s.Team, Expires: s.Contract.Expires, WeeklyWage: s.Contract.WeeklyWage,
		}})
	}
	return nil
}

// validateLifecycle checks players over time:
//
//   - every player was born before now; active players are at most
//     players.RetirementAge (the yearly step retires them at that age);
//   - active players have a condition record; retired players have none
//     and are employed by no club;
//   - exactly one player-year task is queued, in the Expiries phase with no
//     payload, due at the next player-year instant.
func (w *World) validateLifecycle() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	for _, p := range w.registry.Players() {
		profile, _ := w.players.Profile(p.ID) // Validate reports missing profiles
		_, hasCondition := w.medical.Condition(p.ID)
		_, employed := w.employment.Assignment(p.ID)
		age, err := w.calendar.WholeYears(p.Born, w.Now())
		switch {
		case err != nil || p.Born >= w.Now():
			fail("player %d was born at %d, not before now (%d)", p.ID, p.Born, w.Now())
		case profile.Retired && (hasCondition || employed):
			fail("retired player %d has a condition record (%t) or a club (%t)", p.ID, hasCondition, employed)
		case !profile.Retired && !hasCondition:
			fail("player %d has no condition record", p.ID)
		case !profile.Retired && age > players.RetirementAge:
			fail("player %d is %d and still active", p.ID, age)
		}
	}
	if n, m := len(w.medical.Records()), len(w.activePlayers()); n != m {
		fail("%d condition records for %d active players", n, m)
	}

	next, err := w.playerYearAfter(w.Now())
	if err != nil {
		return append(errs, err)
	}
	tasks := 0
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskPlayerYear {
			continue
		}
		tasks++
		if t.PayloadID != 0 || t.Phase != sim.PhaseExpiries || t.DueAt != next {
			fail("player-year task %d due %d phase %d payload %d, want due %d", t.ID, t.DueAt, t.Phase, t.PayloadID, next)
		}
	}
	if tasks != 1 {
		fail("%d player-year tasks queued, want 1", tasks)
	}
	return errs
}

// activePlayers returns every player who has not retired, in ID order.
func (w *World) activePlayers() []ids.PlayerID {
	var out []ids.PlayerID
	for _, id := range w.players.PlayerIDs() {
		if p, _ := w.players.Profile(id); !p.Retired {
			out = append(out, id)
		}
	}
	return out
}

// checkLifecycleEvent compares a retirement, youth or development event
// with the modules.
func (w *World) checkLifecycleEvent(e events.Event) error {
	switch e.Kind {
	case events.KindPlayerRetired:
		p := e.PlayerRetired
		if profile, ok := w.players.Profile(p.Player); !ok || !profile.Retired {
			return fmt.Errorf("player %d has not retired", p.Player)
		}
		if p.Team != 0 {
			return w.checkPlayerEvent(p.Player, p.Club, p.Team)
		}
	case events.KindYouthJoined:
		p := e.YouthJoined
		return w.checkPlayerEvent(p.Player, p.Club, p.Team)
	case events.KindPlayersDeveloped:
		for _, d := range e.PlayersDeveloped.Players {
			if _, ok := w.registry.Player(d.Player); !ok {
				return fmt.Errorf("unknown player %d", d.Player)
			}
			if team, ok := w.registry.Team(d.Team); d.Team != 0 && (!ok || team.Kind != registry.TeamSenior) {
				return fmt.Errorf("player %d's team %d is not a senior team", d.Player, d.Team)
			}
		}
	}
	return nil
}
