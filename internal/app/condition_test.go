package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
)

func conditions(w *World) map[ids.PlayerID]int {
	out := map[ids.PlayerID]int{}
	for _, r := range w.medical.Records() {
		out[r.Player] = int(r.Condition)
	}
	return out
}

func stamina(w *World, id ids.PlayerID) uint8 {
	p, _ := w.players.Profile(id)
	return uint8(p.Attributes[players.Stamina])
}

// recoveryTasks returns the queued recovery tasks.
func recoveryTasks(w *World) []sim.Task {
	var out []sim.Task
	for _, t := range w.scheduler.Pending() {
		if t.Kind == taskRecovery {
			out = append(out, t)
		}
	}
	return out
}

// Resolving a batch costs exactly the minutes each participant played,
// computed independently from the same outcomes; nobody else changes.
func TestMatchExposureLowersCondition(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 1)
	mustContinue(t, w, seasonEnd(w)) // round 2 kicks off after a week of rest
	ready, _ := w.Pending()
	plan, err := w.prepareBatch(commandFor(ready, 0).Rounds)
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := w.simulateBatch(plan)
	if err != nil {
		t.Fatal(err)
	}
	before := conditions(w)
	resolveNow(t, w)
	after := conditions(w)

	params := medical.DefaultParams()
	want := map[ids.PlayerID]int{}
	for id, c := range before {
		want[id] = c
	}
	played := 0
	for _, o := range outcomes {
		for _, pt := range o.Participants {
			want[pt.Player] = max(before[pt.Player]-params.Drain(pt.Minutes(), stamina(w, pt.Player)), int(params.MinCondition))
			played++
		}
	}
	if played != 4*2*11 {
		t.Fatalf("%d participants", played)
	}
	if !reflect.DeepEqual(after, want) {
		t.Fatal("conditions after the round differ from an independent computation")
	}
}

// Each midnight restores every player by their daily recovery, capped at
// full; exactly one recovery task is always queued for the next midnight.
func TestDailyRecovery(t *testing.T) {
	w := newWorld(t, 42)
	if got := recoveryTasks(w); len(got) != 1 || got[0].DueAt != sim.GameInstant(sim.Day) {
		t.Fatalf("new world recovery tasks %+v", got)
	}
	playBatches(t, w, 1) // round 1 kicked off at 15:00 and was resolved
	tired := conditions(w)
	k := w.Now()
	for d := 1; d <= 3; d++ {
		mustContinue(t, w, k+sim.GameInstant(d)*day)
		params := medical.DefaultParams()
		for id, c := range conditions(w) {
			want := min(tired[id]+d*params.Recovery(stamina(w, id)), int(medical.MaxCondition))
			if c != want {
				t.Fatalf("day %d: player %d condition %d, want %d", d, id, c, want)
			}
		}
		rec := recoveryTasks(w)
		if len(rec) != 1 || rec[0].DueAt <= w.Now() || rec[0].DueAt > w.Now()+day || rec[0].DueAt%day != 0 {
			t.Fatalf("day %d: recovery tasks %+v at %d", d, rec, w.Now())
		}
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	// Recovery never interrupts Continue.
	if res := mustContinue(t, w, w.Now()+2*day); res != (ReachedTarget{Now: w.Now()}) {
		t.Fatalf("Continue = %#v", res)
	}
}

// Condition reaches selection: planned inputs carry the current condition,
// and over a season the AI rotates tired players out.
func TestConditionDrivesSelection(t *testing.T) {
	w := newWorld(t, 42)
	first := map[ids.TeamID][]ids.PlayerID{}
	rotated := 0
	for {
		res := mustContinue(t, w, seasonEnd(w))
		ready, ok := res.(FixtureRoundReady)
		if !ok {
			break
		}
		plan, err := w.prepareBatch(commandFor(ready, 0).Rounds)
		if err != nil {
			t.Fatal(err)
		}
		cond := conditions(w)
		for _, p := range plan {
			for _, side := range []matches.Side{matches.Home, matches.Away} {
				in := p.input.Team(side)
				var xi []ids.PlayerID
				for _, pl := range append(in.Starters, in.Bench...) {
					if int(pl.Condition) != cond[pl.Player] {
						t.Fatalf("player %d input condition %d, medical %d", pl.Player, pl.Condition, cond[pl.Player])
					}
				}
				for _, pl := range in.Starters {
					xi = append(xi, pl.Player)
				}
				if prev, seen := first[in.Team]; !seen {
					first[in.Team] = xi
				} else if !reflect.DeepEqual(prev, xi) {
					rotated++
				}
			}
		}
		resolveNow(t, w)
	}
	t.Logf("%d team-rounds started a different XI than round 1", rotated)
	if rotated == 0 {
		t.Fatal("condition never changed an AI lineup")
	}
	minimum := int(medical.MaxCondition)
	for _, c := range conditions(w) {
		minimum = min(minimum, c)
	}
	if minimum == int(medical.MaxCondition) {
		t.Fatal("nobody is tired after a season")
	}
}

// Saving between matches (mid-recovery) and continuing gives the same world
// as never saving.
func TestSaveMidRecoveryContinuesIdentically(t *testing.T) {
	straight := newWorld(t, 42)
	playBatches(t, straight, 3)
	mid := straight.Now() + 3*day + 5*60 // three midnights later, 20:00
	mustContinue(t, straight, mid)
	loaded := roundTrip(t, straight)
	if !reflect.DeepEqual(conditions(loaded), conditions(straight)) {
		t.Fatal("conditions changed on load")
	}
	a, b := playSeason(t, straight), playSeason(t, loaded)
	mustContinue(t, straight, seasonEnd(straight))
	mustContinue(t, loaded, seasonEnd(loaded))
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(loaded.Snapshot(), straight.Snapshot()) {
		t.Fatal("season after a mid-recovery save differs")
	}
}

// Recovery in one Continue call equals recovery over many calls.
func TestRecoveryDoesNotDependOnContinueChunking(t *testing.T) {
	one, many := newWorld(t, 42), newWorld(t, 42)
	playBatches(t, one, 1)
	playBatches(t, many, 1)
	target := one.Now() + 5*day + 17
	mustContinue(t, one, target)
	for step := many.Now() + 7*60; step < target; step += 11 * 60 {
		mustContinue(t, many, step)
	}
	mustContinue(t, many, target)
	a, b := one.Snapshot(), many.Snapshot()
	a.Revision, b.Revision = 0, 0
	for _, s := range []*WorldSnapshot{&a, &b} { // events carry their commit's revision
		for i := range s.Events {
			s.Events[i].Revision, s.Events[i].Sequence = 0, 0
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("chunked Continue gives a different world")
	}
}

func TestSquadView(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 1)
	squad, ok := w.Squad(userClub)
	if !ok || len(squad) != 20 {
		t.Fatalf("squad of %d, %v", len(squad), ok)
	}
	team, _ := w.registry.SeniorTeam(userClub)
	tired := 0
	for i, p := range squad {
		prof, _ := w.players.Profile(p.Player)
		c, _ := w.medical.Condition(p.Player)
		reg, _ := w.registry.Player(p.Player)
		a, _ := w.employment.Assignment(p.Player)
		switch {
		case i > 0 && squad[i-1].Player >= p.Player:
			t.Fatal("squad not in ascending ID order")
		case a.Team != team || p.Name != reg.FullName() || p.Position != prof.Position ||
			p.Attributes != prof.Attributes || p.Overall != prof.Overall() || p.Condition != c:
			t.Fatalf("row %+v disagrees with the owning modules", p)
		}
		if p.Condition < medical.MaxCondition {
			tired++
		}
	}
	if tired != 11 {
		t.Fatalf("%d tired players after one match, want the 11 starters", tired)
	}
	if _, ok := w.Squad(99); ok {
		t.Fatal("unknown club has a squad")
	}
}

func TestRestoreRejectsInvalidMedicalState(t *testing.T) {
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playBatches(t, w, 2)
		mustContinue(t, w, w.Now()+2*day)
		return w.Snapshot()
	}
	recovery := func(s *WorldSnapshot) *sim.Task {
		for i, task := range s.Scheduler.Tasks {
			if task.Kind == taskRecovery {
				return &s.Scheduler.Tasks[i]
			}
		}
		t.Fatal("no recovery task")
		return nil
	}
	cases := map[string]func(*WorldSnapshot){
		"missing condition":     func(s *WorldSnapshot) { s.Medical = s.Medical[1:] },
		"condition for unknown": func(s *WorldSnapshot) { s.Medical[0].Player = 999 },
		"condition too high":    func(s *WorldSnapshot) { s.Medical[3].Condition = medical.MaxCondition + 1 },
		"condition below floor": func(s *WorldSnapshot) { s.Medical[3].Condition = medical.DefaultParams().MinCondition - 1 },
		"duplicate condition":   func(s *WorldSnapshot) { s.Medical[1].Player = s.Medical[0].Player },
		"no recovery task": func(s *WorldSnapshot) {
			var kept []sim.Task
			for _, task := range s.Scheduler.Tasks {
				if task.Kind != taskRecovery {
					kept = append(kept, task)
				}
			}
			s.Scheduler.Tasks = kept
		},
		"two recovery tasks": func(s *WorldSnapshot) {
			extra := *recovery(s)
			extra.ID = s.Scheduler.LastTaskID + 1
			extra.DueAt += day
			s.Scheduler.LastTaskID++
			s.Scheduler.Tasks = append(s.Scheduler.Tasks, extra)
		},
		"recovery not at midnight": func(s *WorldSnapshot) { recovery(s).DueAt += 60 },
		"recovery skips a day":     func(s *WorldSnapshot) { recovery(s).DueAt += day },
		"recovery with payload":    func(s *WorldSnapshot) { recovery(s).PayloadID = 1 },
		"recovery wrong phase":     func(s *WorldSnapshot) { recovery(s).Phase = sim.PhaseFixtures },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	snap := build()
	snap.Versions.Medical++
	if _, err := Restore(snap); !errors.Is(err, ErrIncompatibleSave) {
		t.Errorf("medical version: err = %v", err)
	}
}
