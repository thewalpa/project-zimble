package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

const day = sim.GameInstant(sim.Day)

func firstKickoff(t *testing.T, w *World) sim.GameInstant {
	t.Helper()
	return w.competitions.Rounds(w.leagues[0].season)[0].Kickoff
}

func mustContinue(t *testing.T, w *World, until sim.GameInstant) ContinueResult {
	t.Helper()
	res, err := w.Continue(until)
	if err != nil {
		t.Fatalf("Continue(%d): %v", until, err)
	}
	return res
}

// state captures everything Continue may change.
type state struct {
	Now        sim.GameInstant
	Revision   Revision
	Tasks      []sim.Task
	Payloads   map[sim.PayloadID]competitions.RoundRef
	Schedules  []Schedule
	Tables     []Table
	Pending    []competitions.RoundInfo
	Commands   int
	Conditions []medical.Record
	Events     []events.Event
	Inbox      inbox.Snapshot
	Finance    finance.Snapshot
}

func snapshot(w *World) state {
	payloads := map[sim.PayloadID]competitions.RoundRef{}
	for k, v := range w.payloads {
		payloads[k] = v
	}
	return state{w.Now(), w.Revision(), w.scheduler.Pending(), payloads, w.Schedules(), w.Tables(),
		w.competitions.PendingRounds(), len(w.commands), w.medical.Records(), w.Events(), w.inbox.Snapshot(), w.finance.Snapshot()}
}

// kickoffTasks returns the queued round kickoff tasks in queue order.
func kickoffTasks(w *World) []sim.Task {
	var out []sim.Task
	for _, t := range w.scheduler.Pending() {
		if t.Kind == taskRoundKickoff {
			out = append(out, t)
		}
	}
	return out
}

func TestNewWorldSchedulesOneTaskPerRound(t *testing.T) {
	w := newWorld(t, 42)
	if w.Now() != 0 || w.Calendar().Epoch() != DefaultEpoch() {
		t.Fatalf("now=%d epoch=%v", w.Now(), w.Calendar().Epoch())
	}
	sched := w.Schedules()[0]
	want, _ := w.Calendar().Instant(sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15})
	tasks := kickoffTasks(w)
	if len(tasks) != 14 || len(w.payloads) != 14 || w.scheduler.Len() != 17 {
		t.Fatalf("%d kickoff tasks of %d, %d payloads; want 14 of 17 (with recovery, wages and season end)", len(tasks), w.scheduler.Len(), len(w.payloads))
	}
	for i, r := range sched.Rounds {
		kickoff := want + sim.GameInstant(i)*7*day
		if r.Kickoff != kickoff || r.Status != competitions.RoundScheduled {
			t.Fatalf("round %d kickoff %d status %s, want %d scheduled", r.Round, r.Kickoff, r.Status, kickoff)
		}
		task := tasks[i]
		ref := w.payloads[task.PayloadID]
		if task.DueAt != kickoff || task.Phase != sim.PhaseFixtures || task.Kind != taskRoundKickoff ||
			task.StableOrder != 1 || ref != (competitions.RoundRef{Season: w.leagues[0].season, Round: r.Round}) {
			t.Fatalf("task %d = %+v -> %v", i, task, ref)
		}
	}
	if sched.Rounds[0].Civil != (sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15}) {
		t.Fatalf("round 1 civil kickoff = %v", sched.Rounds[0].Civil)
	}
}

func TestContinueBeforeAndAtKickoff(t *testing.T) {
	w := newWorld(t, 42)
	k := firstKickoff(t, w)
	fixturesBefore := w.competitions.Fixtures(w.leagues[0].season)

	if res := mustContinue(t, w, k-1); res != (ReachedTarget{Now: k - 1}) {
		t.Fatalf("before kickoff: %#v", res)
	}
	if len(kickoffTasks(w)) != 14 || len(w.competitions.PendingRounds()) != 0 {
		t.Fatal("a task ran before its kickoff")
	}

	res := mustContinue(t, w, k)
	ready, ok := res.(FixtureRoundReady)
	if !ok || ready.At != k || len(ready.Rounds) != 1 {
		t.Fatalf("at kickoff: %#v", res)
	}
	r := ready.Rounds[0]
	if r.Round != (competitions.RoundRef{Season: w.leagues[0].season, Round: 1}) || r.Kickoff != k || len(r.Fixtures) != 4 {
		t.Fatalf("ready round = %+v", r)
	}
	if w.Now() != k || len(kickoffTasks(w)) != 13 || len(w.payloads) != 13 {
		t.Fatalf("now=%d tasks=%d payloads=%d", w.Now(), len(kickoffTasks(w)), len(w.payloads))
	}
	if !reflect.DeepEqual(w.competitions.Fixtures(w.leagues[0].season), fixturesBefore) {
		t.Fatal("kickoff changed fixtures")
	}
	if got := w.Schedules()[0].Rounds; got[0].Status != competitions.RoundAwaitingResults || got[1].Status != competitions.RoundScheduled {
		t.Fatalf("statuses = %s, %s", got[0].Status, got[1].Status)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestContinuePastKickoffStopsAtKickoff(t *testing.T) {
	w := newWorld(t, 42)
	k := firstKickoff(t, w)
	res := mustContinue(t, w, k+30*day)
	if ready, ok := res.(FixtureRoundReady); !ok || ready.At != k || w.Now() != k {
		t.Fatalf("result %#v, now %d; want stop at %d", res, w.Now(), k)
	}
}

func TestContinueWhilePausedIsIdempotent(t *testing.T) {
	w := newWorld(t, 42)
	k := firstKickoff(t, w)
	first := mustContinue(t, w, k)
	before := snapshot(w)
	for _, target := range []sim.GameInstant{k, k + 1, k + 7*day, k + 200*day} {
		if res := mustContinue(t, w, target); !reflect.DeepEqual(res, first) {
			t.Fatalf("Continue(%d) while paused = %#v, want %#v", target, res, first)
		}
		if !reflect.DeepEqual(snapshot(w), before) {
			t.Fatalf("Continue(%d) while paused changed state", target)
		}
	}
	// Round 2 is due at k+7 days but must not be dispatched while round 1 waits.
	if r2, _ := w.competitions.Round(competitions.RoundRef{Season: w.leagues[0].season, Round: 2}); r2.Status != competitions.RoundScheduled {
		t.Fatalf("round 2 status = %s", r2.Status)
	}
}

func TestContinueRejectsPastTarget(t *testing.T) {
	w := newWorld(t, 42)
	if _, err := w.Continue(-1); !errors.Is(err, ErrTargetBeforeNow) {
		t.Fatalf("err = %v", err)
	}
	mustContinue(t, w, 5*day)
	before := snapshot(w)
	if _, err := w.Continue(5*day - 1); !errors.Is(err, ErrTargetBeforeNow) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.Continue(sim.MaxInstant + 1); err == nil {
		t.Fatal("out-of-range target accepted")
	}
	if !reflect.DeepEqual(snapshot(w), before) {
		t.Fatal("rejected Continue changed state")
	}
	// Also rejected while paused.
	k := firstKickoff(t, w)
	mustContinue(t, w, k)
	if _, err := w.Continue(k - 1); !errors.Is(err, ErrTargetBeforeNow) {
		t.Fatalf("paused err = %v", err)
	}
	if _, err := w.Continue(sim.MaxInstant + 1); err == nil {
		t.Fatal("out-of-range target accepted while paused")
	}
}

func TestOneContinueEqualsSeveral(t *testing.T) {
	k := firstKickoff(t, newWorld(t, 42))
	for _, target := range []sim.GameInstant{k - 1, k, k + 45*day} {
		one, many := newWorld(t, 42), newWorld(t, 42)
		resOne := mustContinue(t, one, target)
		var resMany ContinueResult
		for _, step := range []sim.GameInstant{0, day, 10 * day, k - day, k - 1, target} {
			if step <= target && step >= many.Now() {
				resMany = mustContinue(t, many, step)
			}
		}
		// Revision counts commits, so more calls legitimately commit more
		// often; everything else must match.
		stOne, stMany := snapshot(one), snapshot(many)
		stOne.Revision, stMany.Revision = 0, 0
		for _, st := range []*state{&stOne, &stMany} { // events carry their commit's revision
			for i := range st.Events {
				st.Events[i].Revision, st.Events[i].Sequence = 0, 0
			}
		}
		if ready, ok := resOne.(FixtureRoundReady); ok {
			ready.Revision = 0
			resOne = ready
		}
		if ready, ok := resMany.(FixtureRoundReady); ok {
			ready.Revision = 0
			resMany = ready
		}
		if !reflect.DeepEqual(resOne, resMany) || !reflect.DeepEqual(stOne, stMany) {
			t.Fatalf("target %d: one call %#v, several calls %#v", target, resOne, resMany)
		}
	}
}

func TestFailedKickoffLeavesStateUnchanged(t *testing.T) {
	w := newWorld(t, 42)
	k := firstKickoff(t, w)
	mustContinue(t, w, k-1) // the kickoff is the next due cohort
	before := snapshot(w)

	// Remove round 1's payload record: its handler must fail.
	task := kickoffTasks(w)[0]
	ref := w.payloads[task.PayloadID]
	delete(w.payloads, task.PayloadID)
	if _, err := w.Continue(k + day); err == nil {
		t.Fatal("Continue succeeded with a missing payload")
	}
	if err := w.Validate(); err == nil {
		t.Fatal("Validate accepted a task with a missing payload")
	}
	w.payloads[task.PayloadID] = ref
	if !reflect.DeepEqual(snapshot(w), before) {
		t.Fatal("failed Continue changed clock, tasks or round state")
	}
	// Retrying after repair dispatches the same task.
	if res, ok := mustContinue(t, w, k+day).(FixtureRoundReady); !ok || res.At != k {
		t.Fatalf("retry = %#v", res)
	}
}

// A failure anywhere in a kickoff cohort must leave every round in the cohort
// untouched, whichever task fails. (Tasks of another kind form their own
// cohort, so the bad task here is a kickoff whose payload is missing.)
func TestFailedCohortIsAllOrNothing(t *testing.T) {
	for _, stableOrder := range []uint64{0, 999} { // bad task first or last
		w := newWorld(t, 42)
		k := firstKickoff(t, w)
		mustContinue(t, w, k-1)
		if _, err := w.scheduler.Schedule(sim.TaskSpec{DueAt: k, Phase: sim.PhaseFixtures, StableOrder: stableOrder, Kind: taskRoundKickoff, PayloadID: 999}); err != nil {
			t.Fatal(err)
		}
		before := snapshot(w)
		if _, err := w.Continue(k); err == nil {
			t.Fatalf("stableOrder %d: Continue succeeded with a missing kickoff payload", stableOrder)
		}
		if !reflect.DeepEqual(snapshot(w), before) {
			t.Fatalf("stableOrder %d: failed cohort partially applied", stableOrder)
		}
	}
}

// addLeague creates another league season with the same timing and teams as
// the main league and schedules its rounds, bypassing world creation. Its
// kickoffs coincide with the main league's and every team is double-booked,
// which world validation and round resolution must reject.
func addLeague(t *testing.T, w *World, comp ids.CompetitionID) {
	t.Helper()
	ref := competitions.SeasonRef{Competition: comp, Season: 1}
	entrants, _ := w.competitions.Entrants(w.leagues[0].season)
	timing := competitions.Timing{FirstKickoff: firstKickoff(t, w), RoundInterval: sim.Week}
	if err := w.competitions.CreateLeagueSeason(w.seed, ref, entrants, timing); err != nil {
		t.Fatal(err)
	}
	if err := w.scheduleRounds(ref); err != nil {
		t.Fatal(err)
	}
}

// twoLeagueWorld builds 16 clubs split into two leagues with identical
// timing, so every round of both leagues kicks off together. The leagues are
// passed out of ID order to check canonical ordering.
func twoLeagueWorld(t *testing.T) *World {
	t.Helper()
	defs := content.Default()
	defs.ClubCount = 16
	defs.Towns = append(defs.Towns,
		content.Town{Name: "Ashenby", Short: "ASH"}, content.Town{Name: "Brindle", Short: "BRN"},
		content.Town{Name: "Corvale", Short: "COR"}, content.Town{Name: "Drummond", Short: "DRM"})
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	second := content.DefaultLeague()
	second.ID, second.Name = 2, "Second League"
	w, err := load(defs, []content.League{second, content.DefaultLeague()}, DefaultEpoch(), snap)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestSimultaneousKickoffsAreDeterministic(t *testing.T) {
	run := func() (ContinueResult, state) {
		w := twoLeagueWorld(t)
		if len(w.leagues) != 2 || w.leagues[0].def.ID != 1 || w.leagues[1].def.ID != 2 {
			t.Fatalf("leagues not in ID order: %+v", w.leagues)
		}
		res := mustContinue(t, w, firstKickoff(t, w)+10*day)
		if again := mustContinue(t, w, firstKickoff(t, w)+20*day); !reflect.DeepEqual(again, res) {
			t.Fatal("repeated Continue changed the ready cohort")
		}
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
		return res, snapshot(w)
	}
	res, st := run()
	ready, ok := res.(FixtureRoundReady)
	if !ok || len(ready.Rounds) != 2 {
		t.Fatalf("result = %#v", res)
	}
	for i, comp := range []ids.CompetitionID{1, 2} {
		r := ready.Rounds[i].Round
		if r.Season.Competition != comp || r.Round != 1 || ready.Rounds[i].Kickoff != ready.At {
			t.Fatalf("ready[%d] = %+v, want competition %d round 1", i, ready.Rounds[i], comp)
		}
	}
	if len(st.Tasks) != 2*14-2+1+1+2 {
		t.Fatalf("%d tasks remain, want 26 kickoffs, a recovery, wages and 2 season ends", len(st.Tasks))
	}
	res2, st2 := run()
	if !reflect.DeepEqual(res, res2) || !reflect.DeepEqual(st, st2) {
		t.Fatal("simultaneous kickoffs are not deterministic")
	}
}

func TestWorldRejectsDoubleBookedLeague(t *testing.T) {
	w := newWorld(t, 42)
	addLeague(t, w, 2)
	if err := w.Validate(); err == nil {
		t.Fatal("Validate accepted a league outside the world's definitions sharing every team")
	}
}

func TestScheduleQueryDoesNotConsumeTasks(t *testing.T) {
	w := newWorld(t, 42)
	before := snapshot(w)
	for range 3 {
		w.Schedules()
		w.Tables()
		w.Summary()
	}
	if !reflect.DeepEqual(snapshot(w), before) {
		t.Fatal("read-only queries changed world state")
	}
}

func TestLoadRejectsInvalidEpochs(t *testing.T) {
	defs := content.Default()
	snap, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	for name, epoch := range map[string]sim.CivilTime{
		"zero":                    {},
		"not a real date":         {Year: 2025, Month: 2, Day: 30},
		"after the first kickoff": {Year: 2025, Month: 8, Day: 9, Hour: 15, Minute: 1},
	} {
		if _, err := load(defs, []content.League{content.DefaultLeague()}, epoch, snap); err == nil {
			t.Errorf("%s: load succeeded", name)
		}
	}
	if _, err := NewWorld(Config{Seed: 42}); err == nil {
		t.Error("NewWorld accepted a config without an epoch")
	}
	// Epoch exactly at the first kickoff is allowed: round 1 is due at once.
	w, err := load(defs, []content.League{content.DefaultLeague()}, sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15}, snap)
	if err != nil {
		t.Fatal(err)
	}
	if res, ok := mustContinue(t, w, 0).(FixtureRoundReady); !ok || res.At != 0 {
		t.Fatalf("Continue(0) = %#v", res)
	}
}
