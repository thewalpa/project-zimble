package sim

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func newScheduler(t *testing.T) *Scheduler {
	t.Helper()
	s, err := NewScheduler(0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustSchedule(t *testing.T, s *Scheduler, spec TaskSpec) Task {
	t.Helper()
	task, err := s.Schedule(spec)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestBeforeUsesEveryTieBreaker(t *testing.T) {
	base := Task{ID: 5, DueAt: 100, Phase: PhaseFixtures, StableOrder: 7}
	cases := map[string]Task{
		"DueAt":       {ID: 9, DueAt: 99, Phase: PhasePresentation, StableOrder: 99},
		"Phase":       {ID: 9, DueAt: 100, Phase: PhaseDecisions, StableOrder: 99},
		"StableOrder": {ID: 9, DueAt: 100, Phase: PhaseFixtures, StableOrder: 6},
		"ID":          {ID: 4, DueAt: 100, Phase: PhaseFixtures, StableOrder: 7},
	}
	for name, earlier := range cases {
		if !Before(earlier, base) || Before(base, earlier) {
			t.Errorf("%s: tie-breaker not applied", name)
		}
	}
	if Before(base, base) {
		t.Error("Before is not irreflexive")
	}
}

func TestQueueRunsTasksInLexicographicOrder(t *testing.T) {
	s := newScheduler(t)
	// Scheduled out of order; IDs are assigned 1..6 in this order.
	specs := []TaskSpec{
		{DueAt: 20, Phase: PhaseDecisions, StableOrder: 1, Kind: 1},   // ID 1
		{DueAt: 10, Phase: PhaseFixtures, StableOrder: 2, Kind: 1},    // ID 2
		{DueAt: 10, Phase: PhaseFixtures, StableOrder: 1, Kind: 1},    // ID 3
		{DueAt: 10, Phase: PhasePreparation, StableOrder: 9, Kind: 1}, // ID 4
		{DueAt: 10, Phase: PhaseFixtures, StableOrder: 1, Kind: 1},    // ID 5
		{DueAt: 5, Phase: PhasePresentation, StableOrder: 9, Kind: 1}, // ID 6
	}
	for _, spec := range specs {
		mustSchedule(t, s, spec)
	}
	var order []TaskID
	var cohorts [][]TaskID
	_, err := s.RunUntil(100, func(_ GameInstant, c []Task) (bool, error) {
		var ids []TaskID
		for _, task := range c {
			ids = append(ids, task.ID)
			order = append(order, task.ID)
		}
		cohorts = append(cohorts, ids)
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []TaskID{6, 4, 3, 5, 2, 1}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	want := [][]TaskID{{6}, {4}, {3, 5, 2}, {1}}
	if !reflect.DeepEqual(cohorts, want) {
		t.Fatalf("cohorts = %v, want %v (grouped by DueAt and Phase)", cohorts, want)
	}
}

func TestScheduleRejectsInvalidTasks(t *testing.T) {
	s := newScheduler(t)
	if _, err := s.RunUntil(50, func(GameInstant, []Task) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	cases := map[string]TaskSpec{
		"in the past":    {DueAt: 49, Phase: PhaseFixtures, Kind: 1},
		"out of range":   {DueAt: MaxInstant + 1, Phase: PhaseFixtures, Kind: 1},
		"phase zero":     {DueAt: 60, Phase: 0, Kind: 1},
		"phase too high": {DueAt: 60, Phase: PhasePresentation + 1, Kind: 1},
		"kind zero":      {DueAt: 60, Phase: PhaseFixtures, Kind: 0},
	}
	for name, spec := range cases {
		if _, err := s.Schedule(spec); err == nil {
			t.Errorf("%s: Schedule succeeded", name)
		}
	}
	if s.Len() != 0 {
		t.Fatal("rejected tasks were queued")
	}
	if task := mustSchedule(t, s, TaskSpec{DueAt: 50, Phase: PhaseFixtures, Kind: 1}); task.ID != 1 {
		t.Fatalf("first accepted task has ID %d, want 1", task.ID)
	}
	if _, err := NewScheduler(MaxInstant + 1); err == nil {
		t.Error("NewScheduler accepted an out-of-range start")
	}
}

func TestRunUntilTargetSemantics(t *testing.T) {
	s := newScheduler(t)
	mustSchedule(t, s, TaskSpec{DueAt: 100, Phase: PhaseFixtures, Kind: 1})
	calls := 0
	h := func(GameInstant, []Task) (bool, error) { calls++; return false, nil }

	if _, err := s.RunUntil(99, h); err != nil || calls != 0 || s.Now() != 99 || s.Len() != 1 {
		t.Fatalf("before due: err=%v calls=%d now=%d len=%d", err, calls, s.Now(), s.Len())
	}
	if _, err := s.RunUntil(98, h); !errors.Is(err, ErrTargetBeforeNow) || s.Now() != 99 {
		t.Fatalf("past target: err=%v now=%d", err, s.Now())
	}
	if _, err := s.RunUntil(100, h); err != nil || calls != 1 || s.Now() != 100 || s.Len() != 0 {
		t.Fatalf("inclusive target: err=%v calls=%d now=%d len=%d", err, calls, s.Now(), s.Len())
	}
	if _, err := s.RunUntil(100, h); err != nil || calls != 1 {
		t.Fatalf("task ran twice: calls=%d", calls)
	}
}

func TestRunUntilStopsAfterCommittingCohort(t *testing.T) {
	s := newScheduler(t)
	mustSchedule(t, s, TaskSpec{DueAt: 10, Phase: PhaseFixtures, Kind: 1})
	mustSchedule(t, s, TaskSpec{DueAt: 20, Phase: PhaseFixtures, Kind: 1})
	stopped, err := s.RunUntil(1000, func(GameInstant, []Task) (bool, error) { return true, nil })
	if err != nil || !stopped || s.Now() != 10 || s.Len() != 1 {
		t.Fatalf("stopped=%v err=%v now=%d len=%d", stopped, err, s.Now(), s.Len())
	}
}

func TestRunUntilFailureLosesNothing(t *testing.T) {
	s := newScheduler(t)
	mustSchedule(t, s, TaskSpec{DueAt: 10, Phase: PhaseFixtures, Kind: 1})
	mustSchedule(t, s, TaskSpec{DueAt: 20, Phase: PhaseFixtures, Kind: 2})
	before := s.Pending()
	boom := errors.New("boom")
	failKind2 := func(_ GameInstant, c []Task) (bool, error) {
		if c[0].Kind == 2 {
			return false, boom
		}
		return false, nil
	}
	if _, err := s.RunUntil(30, failKind2); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	// The first cohort committed; the failing one is still queued and the
	// clock stopped at the last committed cohort, not at the target.
	if s.Now() != 10 || !reflect.DeepEqual(s.Pending(), before[1:]) {
		t.Fatalf("now=%d pending=%v", s.Now(), s.Pending())
	}
	if _, err := s.RunUntil(30, func(GameInstant, []Task) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	if s.Now() != 30 || s.Len() != 0 {
		t.Fatalf("retry: now=%d len=%d", s.Now(), s.Len())
	}
}

func TestOneCallEqualsSeveralCalls(t *testing.T) {
	build := func() *Scheduler {
		s := newScheduler(t)
		for i, due := range []GameInstant{5, 5, 17, 40, 40, 90} {
			mustSchedule(t, s, TaskSpec{DueAt: due, Phase: PhaseFixtures, StableOrder: uint64(10 - i), Kind: 1})
		}
		return s
	}
	record := func(log *[]Task) CohortHandler {
		return func(_ GameInstant, c []Task) (bool, error) { *log = append(*log, c...); return false, nil }
	}
	var oneLog, manyLog []Task
	one, many := build(), build()
	if _, err := one.RunUntil(60, record(&oneLog)); err != nil {
		t.Fatal(err)
	}
	for _, target := range []GameInstant{0, 4, 5, 16, 17, 39, 60} {
		if _, err := many.RunUntil(target, record(&manyLog)); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(oneLog, manyLog) || one.Now() != many.Now() || !reflect.DeepEqual(one.Pending(), many.Pending()) {
		t.Fatalf("one call: now=%d log=%v; many calls: now=%d log=%v", one.Now(), oneLog, many.Now(), manyLog)
	}
}

func TestPendingIsACopy(t *testing.T) {
	s := newScheduler(t)
	mustSchedule(t, s, TaskSpec{DueAt: 10, Phase: PhaseFixtures, Kind: 1})
	p := s.Pending()
	p[0].DueAt = 999
	if s.Pending()[0].DueAt != 10 || s.Len() != 1 {
		t.Fatal("Pending exposed or consumed queue state")
	}
}
