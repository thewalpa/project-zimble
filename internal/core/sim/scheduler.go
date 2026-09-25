package sim

import (
	"container/heap"
	"errors"
	"fmt"
	"slices"
)

type (
	TaskID    uint64
	PayloadID uint64
	TaskKind  uint16
)

// Phase orders work that is due at the same instant. Values are durable;
// never reorder.
type Phase uint8

const (
	PhaseExpiries     Phase = 1
	PhasePreparation  Phase = 2
	PhaseDecisions    Phase = 3
	PhaseFixtures     Phase = 4
	PhaseConsequences Phase = 5
	PhasePresentation Phase = 6
)

func (p Phase) Valid() bool { return p >= PhaseExpiries && p <= PhasePresentation }

// Task is a serializable queue record. Kind and PayloadID refer to a typed
// payload record owned by whoever scheduled the task; the queue never holds
// closures or payload data.
type Task struct {
	ID          TaskID
	DueAt       GameInstant
	Phase       Phase
	StableOrder uint64 // deterministic domain key, never completion order
	Kind        TaskKind
	PayloadID   PayloadID
}

// TaskSpec is a task before the scheduler assigns its ID.
type TaskSpec struct {
	DueAt       GameInstant
	Phase       Phase
	StableOrder uint64
	Kind        TaskKind
	PayloadID   PayloadID
}

// Before orders tasks lexicographically by (DueAt, Phase, StableOrder, ID).
func Before(a, b Task) bool {
	if a.DueAt != b.DueAt {
		return a.DueAt < b.DueAt
	}
	if a.Phase != b.Phase {
		return a.Phase < b.Phase
	}
	if a.StableOrder != b.StableOrder {
		return a.StableOrder < b.StableOrder
	}
	return a.ID < b.ID
}

func compareTasks(a, b Task) int {
	switch {
	case Before(a, b):
		return -1
	case Before(b, a):
		return 1
	}
	return 0
}

var (
	// ErrTargetBeforeNow is returned when asked to run to a time already passed.
	ErrTargetBeforeNow = errors.New("sim: target is before the current time")
	// ErrNotAfterCohort: a handler tried to schedule work that would not run
	// after the cohort it is handling.
	ErrNotAfterCohort = errors.New("sim: task must be due after the running cohort")
)

// Scheduler owns the world clock and the queue of pending tasks. It is not
// safe for concurrent use.
type Scheduler struct {
	now    GameInstant
	lastID TaskID
	queue  taskHeap

	// While a cohort handler runs: the cohort's instant and phase.
	running  bool
	runAt    GameInstant
	runPhase Phase
}

// NewScheduler returns an empty scheduler whose clock reads start.
func NewScheduler(start GameInstant) (*Scheduler, error) {
	if !start.Valid() {
		return nil, fmt.Errorf("sim: start instant %d outside supported range", start)
	}
	return &Scheduler{now: start}, nil
}

func (s *Scheduler) Now() GameInstant { return s.now }

func (s *Scheduler) Len() int { return len(s.queue) }

// Schedule validates spec, assigns the next task ID and queues it.
//
// A cohort handler may schedule follow-up work, but only after its own
// cohort: at a later instant, or at the same instant in a later phase (which
// then runs in the same RunUntil call if the target allows). Anything else is
// ErrNotAfterCohort, so same-instant work cannot loop or run out of order.
func (s *Scheduler) Schedule(spec TaskSpec) (Task, error) {
	switch {
	case s.running && (spec.DueAt < s.runAt || (spec.DueAt == s.runAt && spec.Phase <= s.runPhase)):
		return Task{}, fmt.Errorf("%w: due %d phase %d, cohort %d phase %d", ErrNotAfterCohort, spec.DueAt, spec.Phase, s.runAt, s.runPhase)
	case !spec.DueAt.Valid():
		return Task{}, fmt.Errorf("sim: due instant %d outside supported range", spec.DueAt)
	case spec.DueAt < s.now:
		return Task{}, fmt.Errorf("sim: due instant %d is before now (%d)", spec.DueAt, s.now)
	case !spec.Phase.Valid():
		return Task{}, fmt.Errorf("sim: invalid phase %d", spec.Phase)
	case spec.Kind == 0:
		return Task{}, errors.New("sim: task kind must be non-zero")
	}
	s.lastID++
	t := Task{
		ID: s.lastID, DueAt: spec.DueAt, Phase: spec.Phase,
		StableOrder: spec.StableOrder, Kind: spec.Kind, PayloadID: spec.PayloadID,
	}
	heap.Push(&s.queue, t)
	return t, nil
}

// Pending returns a copy of all queued tasks in execution order.
func (s *Scheduler) Pending() []Task {
	out := slices.Clone([]Task(s.queue))
	slices.SortFunc(out, compareTasks)
	return out
}

// CohortHandler handles every task sharing one (DueAt, Phase). It must either
// fully apply the cohort and return a nil error, or apply nothing (including
// scheduling nothing) and return an error. stop asks RunUntil to return after
// committing the cohort. It may schedule follow-up tasks (see Schedule).
type CohortHandler func(at GameInstant, cohort []Task) (stop bool, err error)

// RunUntil processes due cohorts in queue order. The target is inclusive:
// tasks due exactly at target run.
//
//   - target < Now: returns ErrTargetBeforeNow; nothing changes.
//   - For each cohort due at or before target: call handle. On error, return
//     it with that cohort still queued and the clock at the last committed
//     cohort. On success, remove the cohort and set the clock to its DueAt;
//     if stop is true, return stopped=true.
//   - When no cohort is due, set the clock to target.
func (s *Scheduler) RunUntil(target GameInstant, handle CohortHandler) (stopped bool, err error) {
	if !target.Valid() {
		return false, fmt.Errorf("sim: target %d outside supported range", target)
	}
	if target < s.now {
		return false, fmt.Errorf("%w: target %d, now %d", ErrTargetBeforeNow, target, s.now)
	}
	for len(s.queue) > 0 && s.queue[0].DueAt <= target {
		cohort := s.cohort()
		head := cohort[0]
		s.running, s.runAt, s.runPhase = true, head.DueAt, head.Phase
		stop, err := handle(head.DueAt, slices.Clone(cohort))
		s.running = false
		if err != nil {
			return false, err
		}
		for range cohort { // tasks scheduled by the handler sort after the cohort
			heap.Pop(&s.queue)
		}
		s.now = head.DueAt
		if stop {
			return true, nil
		}
	}
	s.now = target
	return false, nil
}

// cohort returns the queued tasks sharing the head's (DueAt, Phase), in order.
func (s *Scheduler) cohort() []Task {
	all := s.Pending()
	head := all[0]
	n := 1
	for n < len(all) && all[n].DueAt == head.DueAt && all[n].Phase == head.Phase {
		n++
	}
	return all[:n]
}

type taskHeap []Task

func (h taskHeap) Len() int           { return len(h) }
func (h taskHeap) Less(i, j int) bool { return Before(h[i], h[j]) }
func (h taskHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *taskHeap) Push(x any)        { *h = append(*h, x.(Task)) }
func (h *taskHeap) Pop() any {
	old := *h
	t := old[len(old)-1]
	*h = old[:len(old)-1]
	return t
}

// SchedulerSnapshot is the scheduler's authoritative state: the clock, the
// task ID allocator and every queued task in execution order.
type SchedulerSnapshot struct {
	Now        GameInstant
	LastTaskID TaskID
	Tasks      []Task
}

// Snapshot exports the scheduler's state as fresh copies.
func (s *Scheduler) Snapshot() SchedulerSnapshot {
	return SchedulerSnapshot{Now: s.now, LastTaskID: s.lastID, Tasks: s.Pending()}
}

// RestoreScheduler rebuilds a scheduler from a snapshot without running,
// adding or renumbering tasks. It rejects out-of-range instants, invalid
// phases or kinds, zero or duplicate task IDs, IDs the allocator could issue
// again (ID > LastTaskID), and tasks due before the saved clock.
func RestoreScheduler(snap SchedulerSnapshot) (*Scheduler, error) {
	if !snap.Now.Valid() {
		return nil, fmt.Errorf("sim: saved clock %d outside supported range", snap.Now)
	}
	seen := map[TaskID]bool{}
	for _, t := range snap.Tasks {
		switch {
		case t.ID == 0 || seen[t.ID]:
			return nil, fmt.Errorf("sim: task ID %d zero or duplicated", t.ID)
		case t.ID > snap.LastTaskID:
			return nil, fmt.Errorf("sim: task ID %d above allocator %d", t.ID, snap.LastTaskID)
		case !t.DueAt.Valid() || t.DueAt < snap.Now:
			return nil, fmt.Errorf("sim: task %d due %d, clock %d", t.ID, t.DueAt, snap.Now)
		case !t.Phase.Valid() || t.Kind == 0:
			return nil, fmt.Errorf("sim: task %d has invalid phase %d or kind %d", t.ID, t.Phase, t.Kind)
		}
		seen[t.ID] = true
	}
	s := &Scheduler{now: snap.Now, lastID: snap.LastTaskID, queue: taskHeap(slices.Clone(snap.Tasks))}
	heap.Init(&s.queue)
	return s, nil
}
