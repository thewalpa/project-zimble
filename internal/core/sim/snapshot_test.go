package sim

import (
	"reflect"
	"testing"
)

func queued(t *testing.T) *Scheduler {
	t.Helper()
	s := newScheduler(t)
	for _, due := range []GameInstant{50, 20, 20, 90} {
		mustSchedule(t, s, TaskSpec{DueAt: due, Phase: PhaseFixtures, StableOrder: uint64(due), Kind: 1, PayloadID: PayloadID(due)})
	}
	if _, err := s.RunUntil(20, func(GameInstant, []Task) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSchedulerSnapshotRoundTrip(t *testing.T) {
	s := queued(t)
	snap := s.Snapshot()
	if snap.Now != 20 || snap.LastTaskID != 4 || len(snap.Tasks) != 2 {
		t.Fatalf("snapshot %+v", snap)
	}
	r, err := RestoreScheduler(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), snap) {
		t.Fatal("round trip differs")
	}
	// Restoring runs nothing and the allocator continues after saved IDs.
	task, err := r.Schedule(TaskSpec{DueAt: 30, Phase: PhaseFixtures, Kind: 1})
	if err != nil || task.ID != 5 {
		t.Fatalf("new task %+v, %v; want ID 5", task, err)
	}
	snap.Tasks[0].DueAt = 999
	if s.Pending()[0].DueAt == 999 {
		t.Fatal("snapshot shares memory with the scheduler")
	}
}

func TestRestoreSchedulerRejectsInvalidSnapshots(t *testing.T) {
	cases := map[string]func(*SchedulerSnapshot){
		"clock out of range":  func(s *SchedulerSnapshot) { s.Now = MaxInstant + 1 },
		"allocator below IDs": func(s *SchedulerSnapshot) { s.LastTaskID = 3 },
		"zero task ID":        func(s *SchedulerSnapshot) { s.Tasks[0].ID = 0 },
		"duplicate task ID":   func(s *SchedulerSnapshot) { s.Tasks[1].ID = s.Tasks[0].ID },
		"task in the past":    func(s *SchedulerSnapshot) { s.Tasks[0].DueAt = 19 },
		"invalid phase":       func(s *SchedulerSnapshot) { s.Tasks[0].Phase = 0 },
		"zero kind":           func(s *SchedulerSnapshot) { s.Tasks[0].Kind = 0 },
	}
	for name, mutate := range cases {
		snap := queued(t).Snapshot()
		mutate(&snap)
		if _, err := RestoreScheduler(snap); err == nil {
			t.Errorf("%s: RestoreScheduler succeeded", name)
		}
	}
}
