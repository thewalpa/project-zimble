package app

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// Task kinds interpreted by app. Values are durable; never reorder.
const taskRoundKickoff sim.TaskKind = 1

// ErrTargetBeforeNow is returned by Continue for a target earlier than Now.
var ErrTargetBeforeNow = sim.ErrTargetBeforeNow

// ContinueResult is the outcome of Continue: ReachedTarget or
// FixtureRoundReady.
type ContinueResult interface{ continueResult() }

// ReachedTarget means no interruption occurred and the clock is at Now.
type ReachedTarget struct {
	Now sim.GameInstant
}

// FixtureRoundReady means one or more rounds have kicked off and await
// results. The world does not advance until ResolveRounds resolves them;
// Revision is the value to pass as its ExpectedRevision.
type FixtureRoundReady struct {
	At       sim.GameInstant // current world time
	Revision Revision
	Rounds   []ReadyRound // by kickoff, competition, season, round
}

// ReadyRound is a round awaiting results.
type ReadyRound struct {
	Round    competitions.RoundRef
	Kickoff  sim.GameInstant
	Fixtures []ids.FixtureID
}

func (ReachedTarget) continueResult()     {}
func (FixtureRoundReady) continueResult() {}

// Now returns the current world time.
func (w *World) Now() sim.GameInstant { return w.scheduler.Now() }

// Calendar returns the career calendar.
func (w *World) Calendar() sim.Calendar { return w.calendar }

// Continue advances the world toward until (inclusive).
//
//   - until < Now or outside the supported range: returns an error
//     (ErrTargetBeforeNow for a past target); nothing changes.
//   - If any round awaits results, returns FixtureRoundReady for those rounds
//     without advancing time or running tasks.
//   - Otherwise runs due task cohorts in (DueAt, Phase, StableOrder, ID)
//     order. A kickoff cohort (every round kickoff task sharing an instant and
//     phase) is dispatched atomically: its rounds become awaiting results, the
//     clock moves to the kickoff and FixtureRoundReady is returned.
//   - If nothing interrupts, the clock moves to until and ReachedTarget is
//     returned.
//
// A failing cohort stays queued and leaves the clock and round state as they
// were before that cohort. The world revision increments when Continue
// changes the clock or dispatches work.
func (w *World) Continue(until sim.GameInstant) (ContinueResult, error) {
	if !until.Valid() {
		return nil, fmt.Errorf("app: continue target %d outside supported range", until)
	}
	if until < w.Now() {
		return nil, fmt.Errorf("app: continue to %d: %w (now %d)", until, ErrTargetBeforeNow, w.Now())
	}
	if ready, ok := w.pendingRounds(); ok {
		return ready, nil
	}
	nowBefore, queued := w.Now(), w.scheduler.Len()
	stopped, err := w.scheduler.RunUntil(until, w.handleCohort)
	if w.Now() != nowBefore || w.scheduler.Len() != queued {
		w.revision++
	}
	if err != nil {
		return nil, err
	}
	if stopped {
		ready, ok := w.pendingRounds()
		if !ok {
			return nil, errors.New("app: scheduler stopped without a pending round")
		}
		return ready, nil
	}
	return ReachedTarget{Now: w.Now()}, nil
}

// Pending reports the batch awaiting results, if any. It is a read-only
// query: unlike Continue it never runs tasks or moves the clock.
func (w *World) Pending() (FixtureRoundReady, bool) { return w.pendingRounds() }

func (w *World) pendingRounds() (FixtureRoundReady, bool) {
	pending := w.competitions.PendingRounds()
	if len(pending) == 0 {
		return FixtureRoundReady{}, false
	}
	ready := FixtureRoundReady{At: w.Now(), Revision: w.revision}
	for _, r := range pending {
		ready.Rounds = append(ready.Rounds, ReadyRound{Round: r.Ref, Kickoff: r.Kickoff, Fixtures: r.Fixtures})
	}
	return ready, true
}

// handleCohort validates every task before changing anything, then begins
// all the cohort's rounds in one competitions call. Only after that succeeds
// are the consumed payloads deleted; nothing after it can fail.
func (w *World) handleCohort(at sim.GameInstant, cohort []sim.Task) (bool, error) {
	refs := make([]competitions.RoundRef, 0, len(cohort))
	for _, t := range cohort {
		if t.Kind != taskRoundKickoff {
			return false, fmt.Errorf("app: task %d has unknown kind %d", t.ID, t.Kind)
		}
		ref, ok := w.payloads[t.PayloadID]
		if !ok {
			return false, fmt.Errorf("app: task %d references missing payload %d", t.ID, t.PayloadID)
		}
		refs = append(refs, ref)
	}
	if err := w.competitions.BeginRounds(refs, at); err != nil {
		return false, fmt.Errorf("app: kickoff at %d: %w", at, err)
	}
	for _, t := range cohort {
		delete(w.payloads, t.PayloadID)
	}
	return true, nil
}

// scheduleRounds queues one kickoff task per round of a season. StableOrder
// is the competition ID, so simultaneous kickoffs of different competitions
// run in competition order. The whole season is scheduled or none of it is.
func (w *World) scheduleRounds(ref competitions.SeasonRef) error {
	rounds := w.competitions.Rounds(ref)
	if len(rounds) == 0 {
		return fmt.Errorf("app: %s has no rounds", ref)
	}
	for _, r := range rounds {
		if r.Status != competitions.RoundScheduled || r.Kickoff < w.Now() {
			return fmt.Errorf("app: cannot schedule %s (%s, kickoff %d, now %d)", r.Ref, r.Status, r.Kickoff, w.Now())
		}
	}
	for _, r := range rounds {
		w.lastPayload++
		_, err := w.scheduler.Schedule(sim.TaskSpec{
			DueAt:       r.Kickoff,
			Phase:       sim.PhaseFixtures,
			StableOrder: uint64(ref.Competition),
			Kind:        taskRoundKickoff,
			PayloadID:   w.lastPayload,
		})
		if err != nil {
			// Unreachable after the checks above; kept as a guard.
			return fmt.Errorf("app: schedule %s: %w", r.Ref, err)
		}
		w.payloads[w.lastPayload] = r.Ref
	}
	return nil
}

// validateSchedule checks that queued tasks and round status agree:
// each scheduled round has exactly one kickoff task due at its kickoff, each
// round awaiting results has none and kicked off no later than now, and every
// payload belongs to exactly one task.
func (w *World) validateSchedule() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	tasksFor := map[competitions.RoundRef][]sim.Task{}
	payloadUses := map[sim.PayloadID]int{}
	for _, t := range w.scheduler.Pending() {
		if t.Kind != taskRoundKickoff {
			fail("task %d has unknown kind %d", t.ID, t.Kind)
			continue
		}
		payloadUses[t.PayloadID]++
		ref, ok := w.payloads[t.PayloadID]
		if !ok {
			fail("task %d references missing payload %d", t.ID, t.PayloadID)
			continue
		}
		tasksFor[ref] = append(tasksFor[ref], t)
	}
	for _, id := range slices.Sorted(maps.Keys(w.payloads)) {
		if payloadUses[id] != 1 {
			fail("payload %d is used by %d tasks", id, payloadUses[id])
		}
	}
	for _, season := range w.competitions.Seasons() {
		for _, r := range w.competitions.Rounds(season) {
			tasks := tasksFor[r.Ref]
			switch r.Status {
			case competitions.RoundScheduled:
				if len(tasks) != 1 {
					fail("%s is scheduled but has %d kickoff tasks", r.Ref, len(tasks))
				} else if tasks[0].DueAt != r.Kickoff || tasks[0].Phase != sim.PhaseFixtures {
					fail("%s kickoff task due %d phase %d, want %d phase %d", r.Ref, tasks[0].DueAt, tasks[0].Phase, r.Kickoff, sim.PhaseFixtures)
				}
			case competitions.RoundAwaitingResults, competitions.RoundCompleted:
				if len(tasks) != 0 {
					fail("%s already kicked off but still has %d tasks", r.Ref, len(tasks))
				}
				if r.Kickoff > w.Now() {
					fail("%s is %s but kicks off in the future", r.Ref, r.Status)
				}
			}
			delete(tasksFor, r.Ref)
		}
	}
	for _, t := range w.scheduler.Pending() { // queue order, not map order
		if ref, ok := w.payloads[t.PayloadID]; ok && len(tasksFor[ref]) > 0 {
			fail("kickoff task %d for unknown %s", t.ID, ref)
		}
	}
	return errs
}
