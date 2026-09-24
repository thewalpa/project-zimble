package app

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// roundTrip saves and loads w through JSON (the storage encoding without its
// envelope) and checks that encoding is lossless and loading changes nothing.
func roundTrip(t *testing.T, w *World) *World {
	t.Helper()
	snap := w.Snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorldSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, snap) {
		t.Fatal("JSON encoding lost information")
	}
	r, err := Restore(decoded)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !reflect.DeepEqual(r.Snapshot(), snap) {
		t.Fatal("loading changed the world (tasks, IDs, revision or state)")
	}
	return r
}

// playBatches resolves up to n pending batches, continuing command IDs.
func playBatches(t *testing.T, w *World, n int) []RoundsResolved {
	t.Helper()
	var out []RoundsResolved
	for range n {
		res := mustContinue(t, w, seasonEnd(w))
		ready, ok := res.(FixtureRoundReady)
		if !ok {
			break
		}
		r, err := w.ResolveRounds(commandFor(ready, w.NextCommandID()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestEverySavePointRoundTrips(t *testing.T) {
	points := map[string]func() *World{
		"after creation": func() *World { return newWorld(t, 42) },
		"between rounds": func() *World { w := newWorld(t, 42); playBatches(t, w, 7); return w },
		"batch pending": func() *World {
			w := newWorld(t, 42)
			playBatches(t, w, 3)
			readyBatch(t, w)
			return w
		},
		"season complete": func() *World { w := newWorld(t, 42); playSeason(t, w); mustContinue(t, w, seasonEnd(w)); return w },
		"two leagues pending": func() *World {
			w := twoLeagueWorld(t)
			playBatches(t, w, 2)
			readyBatch(t, w)
			return w
		},
	}
	for name, build := range points {
		w := build()
		r := roundTrip(t, w)
		if err := r.Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(r.Tables(), w.Tables()) || !reflect.DeepEqual(r.Schedules(), w.Schedules()) || !reflect.DeepEqual(r.Summary(), w.Summary()) {
			t.Fatalf("%s: derived views differ after load", name)
		}
	}
}

// Save after round 7, load, and finish: everything matches an uninterrupted
// season driven by the same command sequence.
func TestSaveAfterRoundSevenAndFinish(t *testing.T) {
	straight := newWorld(t, 42)
	first := playBatches(t, straight, 7)
	restLive := playSeason(t, straight)

	w := newWorld(t, 42)
	if got := playBatches(t, w, 7); !reflect.DeepEqual(got, first) {
		t.Fatal("first half differs")
	}
	loaded := roundTrip(t, w)
	restLoaded := playSeason(t, loaded)

	if len(restLive) != 7 || !reflect.DeepEqual(restLoaded, restLive) {
		t.Fatal("remaining results or detailed outcomes differ after reload")
	}
	mustContinue(t, straight, seasonEnd(straight))
	mustContinue(t, loaded, seasonEnd(loaded))
	if loaded.Now() != straight.Now() || loaded.Revision() != straight.Revision() ||
		!reflect.DeepEqual(loaded.Tables(), straight.Tables()) {
		t.Fatal("clock, revision or final standings differ")
	}
	if !reflect.DeepEqual(loaded.Snapshot(), straight.Snapshot()) {
		t.Fatal("final world state differs")
	}
}

func TestSaveWhileBatchPending(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 3)
	ready := readyBatch(t, w)
	loaded := roundTrip(t, w)

	loadedReady := readyBatch(t, loaded)
	if !reflect.DeepEqual(loadedReady, ready) {
		t.Fatalf("pending batch changed: %+v vs %+v", loadedReady, ready)
	}
	cmd := commandFor(ready, w.NextCommandID())
	want, err := w.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(loaded.Snapshot(), w.Snapshot()) {
		t.Fatal("resolving the restored batch produced different outcomes")
	}
}

func TestRetryAfterLoad(t *testing.T) {
	w := newWorld(t, 42)
	original := playBatches(t, w, 5)
	loaded := roundTrip(t, w)
	before := loaded.Snapshot()

	recorded := before.Commands[2]
	got, err := loaded.ResolveRounds(recorded.Request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, original[2]) {
		t.Fatal("retry after load returned a different result")
	}
	if !reflect.DeepEqual(loaded.Snapshot(), before) {
		t.Fatal("retry after load changed the world")
	}
	for name, mutate := range map[string]func(*ResolveRounds){
		"different revision": func(c *ResolveRounds) { c.ExpectedRevision++ },
		"different rounds":   func(c *ResolveRounds) { c.Rounds = []competitions.RoundRef{{Season: c.Rounds[0].Season, Round: 9}} },
	} {
		cmd := recorded.Request
		cmd.Rounds = append([]competitions.RoundRef(nil), cmd.Rounds...)
		mutate(&cmd)
		if _, err := loaded.ResolveRounds(cmd); !errors.Is(err, ErrCommandIDReused) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if loaded.NextCommandID() != 6 {
		t.Fatalf("NextCommandID = %d, want 6", loaded.NextCommandID())
	}
}

func TestRestoredAllocatorsIssueFreshIDs(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 7)
	snap := w.Snapshot()
	loaded := roundTrip(t, w)

	usedTasks := map[sim.TaskID]bool{}
	for _, task := range snap.Scheduler.Tasks {
		usedTasks[task.ID] = true
	}
	usedPayloads := map[sim.PayloadID]bool{}
	for _, p := range snap.Payloads {
		usedPayloads[p.ID] = true
	}

	// Allocate through the same paths world creation uses.
	ref := competitions.SeasonRef{Competition: 9, Season: 1}
	entrants, _ := loaded.competitions.Entrants(loaded.leagues[0].season)
	timing := competitions.Timing{FirstKickoff: loaded.Now() + 7*day, RoundInterval: sim.Week}
	if err := loaded.competitions.CreateLeagueSeason(1, ref, entrants, timing); err != nil {
		t.Fatal(err)
	}
	for _, f := range loaded.competitions.Fixtures(ref) {
		if f.ID <= snap.Competitions.LastFixture {
			t.Fatalf("new fixture ID %d reuses the saved range (<= %d)", f.ID, snap.Competitions.LastFixture)
		}
	}
	if err := loaded.scheduleRounds(ref); err != nil {
		t.Fatal(err)
	}
	for _, task := range loaded.scheduler.Pending() {
		if task.Kind == taskRoundKickoff && loaded.payloads[task.PayloadID].Season == ref {
			if usedTasks[task.ID] || task.ID <= snap.Scheduler.LastTaskID {
				t.Fatalf("new task ID %d collides with saved IDs", task.ID)
			}
			if usedPayloads[task.PayloadID] || task.PayloadID <= snap.LastPayload {
				t.Fatalf("new payload ID %d collides with saved IDs", task.PayloadID)
			}
		}
	}
}

func TestSnapshotsShareNoMutableData(t *testing.T) {
	w := newWorld(t, 42)
	playBatches(t, w, 2)
	readyBatch(t, w)
	want := w.Snapshot()

	scribble := func(s *WorldSnapshot) {
		s.Content.Towns[0].Name = "X"
		s.Content.Roster[0].Count = 99
		s.Registry.Clubs[0].Name = "X"
		s.Players[0].Attributes[0] = 1
		s.Employment[0].Team = 99
		s.Competitions.Seasons[0].Fixtures[0].Home = 99
		s.Competitions.Seasons[0].Results[0].HomeGoals = 50
		s.Competitions.Seasons[0].Rounds[0].Status = competitions.RoundScheduled
		s.Scheduler.Tasks[0].DueAt = 1
		s.Payloads[0].Round.Round = 1
		s.Leagues[0].Definition.Name = "X"
		s.Commands[0].Request.Rounds[0].Round = 9
		s.Commands[0].Result.Rounds[0].Round = 9
		s.Commands[0].Result.Matches[0].Score[0] = 9
		if len(s.Commands[0].Result.Matches[0].Goals) > 0 {
			s.Commands[0].Result.Matches[0].Goals[0].Minute = 1
		}
	}

	snap := w.Snapshot()
	scribble(&snap)
	if !reflect.DeepEqual(w.Snapshot(), want) {
		t.Fatal("mutating an exported snapshot changed the world")
	}

	input := w.Snapshot()
	a, err := Restore(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Restore(input)
	if err != nil {
		t.Fatal(err)
	}
	scribble(&input)
	if !reflect.DeepEqual(a.Snapshot(), want) {
		t.Fatal("restored world shares memory with its snapshot")
	}
	if _, err := a.ResolveRounds(commandFor(readyBatch(t, a), a.NextCommandID())); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Snapshot(), want) {
		t.Fatal("two worlds restored from one snapshot share state")
	}
}

func TestRestoreRejectsIncompatibleVersions(t *testing.T) {
	base := newWorld(t, 42).Snapshot()
	for name, mutate := range map[string]func(*Versions){
		"random":         func(v *Versions) { v.Random++ },
		"schedule":       func(v *Versions) { v.Schedule++ },
		"AI selection":   func(v *Versions) { v.Selection++ },
		"engine ID":      func(v *Versions) { v.EngineID = "detailed" },
		"engine version": func(v *Versions) { v.EngineVersion++ },
	} {
		snap := newWorld(t, 42).Snapshot()
		mutate(&snap.Versions)
		if w, err := Restore(snap); !errors.Is(err, ErrIncompatibleSave) || w != nil {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Provenance versions may differ; they are preserved, not rewritten.
	base.Versions.Generator += 5
	base.Versions.Content += 5
	w, err := Restore(base)
	if err != nil {
		t.Fatalf("provenance version difference rejected: %v", err)
	}
	if w.Snapshot().Versions != base.Versions {
		t.Fatal("provenance versions not preserved")
	}
}

func TestRestoreRejectsInvalidState(t *testing.T) {
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playBatches(t, w, 3)
		readyBatch(t, w)
		return w.Snapshot()
	}
	cases := map[string]func(*WorldSnapshot){
		"content changed":            func(s *WorldSnapshot) { s.Content.Towns[0].Name = "Elsewhere" },
		"league rules changed":       func(s *WorldSnapshot) { s.Leagues[0].Definition.MaxBench = 5 },
		"invalid epoch":              func(s *WorldSnapshot) { s.Epoch = sim.CivilTime{} },
		"no leagues":                 func(s *WorldSnapshot) { s.Leagues = nil; s.ContentFingerprint = contentFingerprint(s.Content, nil) },
		"player without profile":     func(s *WorldSnapshot) { s.Players = s.Players[1:] },
		"assignment to unknown team": func(s *WorldSnapshot) { s.Employment[0].Team = 99 },
		"fixture allocator too low":  func(s *WorldSnapshot) { s.Competitions.LastFixture = 10 },
		"task allocator too low":     func(s *WorldSnapshot) { s.Scheduler.LastTaskID = 5 },
		"task due before clock":      func(s *WorldSnapshot) { s.Scheduler.Now = s.Scheduler.Tasks[0].DueAt + 1 },
		"payload allocator too low":  func(s *WorldSnapshot) { s.LastPayload = 5 },
		"payload for unknown round": func(s *WorldSnapshot) {
			s.Payloads[0].Round = competitions.RoundRef{Season: competitions.SeasonRef{Competition: 7, Season: 1}, Round: 1}
		},
		"task without payload":     func(s *WorldSnapshot) { s.Payloads = s.Payloads[1:] },
		"duplicate payload":        func(s *WorldSnapshot) { s.Payloads[1].ID = s.Payloads[0].ID },
		"task for completed round": func(s *WorldSnapshot) { s.Payloads[0].Round.Round = 1 },
		"clock before pending kickoff": func(s *WorldSnapshot) {
			s.Scheduler.Now = s.Competitions.Seasons[0].Rounds[3].Kickoff - 1
		},
		"command score differs": func(s *WorldSnapshot) { s.Commands[0].Result.Matches[0].Score[0]++ },
		"command goals differ": func(s *WorldSnapshot) {
			s.Commands[1].Result.Matches[1].Goals = nil
			s.Commands[1].Result.Matches[1].Score = [2]uint16{}
		},
		"command unknown fixture": func(s *WorldSnapshot) { s.Commands[0].Result.Matches[0].Fixture = 999 },
		"command pending round": func(s *WorldSnapshot) {
			s.Commands[0].Request.Rounds[0].Round = 4
			s.Commands[0].Result.Rounds[0].Round = 4
		},
		"command duplicate ID":    func(s *WorldSnapshot) { s.Commands[1].Request.ID = 1; s.Commands[1].Result.Command = 1 },
		"command ID mismatch":     func(s *WorldSnapshot) { s.Commands[0].Result.Command = 9 },
		"command future revision": func(s *WorldSnapshot) { s.Commands[2].Result.Revision = s.Revision + 1 },
		"command missing report":  func(s *WorldSnapshot) { s.Commands[0].Result.Matches = s.Commands[0].Result.Matches[1:] },
		"result without round": func(s *WorldSnapshot) {
			s.Competitions.Seasons[0].Rounds[0].Status = competitions.RoundAwaitingResults
		},
		"official result edited": func(s *WorldSnapshot) { s.Competitions.Seasons[0].Results[0].HomeGoals += 1 },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		w, err := Restore(snap)
		if err == nil || w != nil {
			t.Errorf("%s: Restore succeeded", name)
			continue
		}
		if !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v, want ErrInvalidSave", name, err)
		}
	}
}

// Detailed match outcomes kept in the command log survive a save.
func TestDetailedOutcomesSurviveSave(t *testing.T) {
	w := newWorld(t, 42)
	resolved := playBatches(t, w, 3)
	loaded := roundTrip(t, w)
	goals := 0
	for i, r := range resolved {
		again, err := loaded.ResolveRounds(ResolveRounds{ID: r.Command, ExpectedRevision: loaded.Snapshot().Commands[i].Request.ExpectedRevision, Rounds: r.Rounds})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again, r) {
			t.Fatalf("command %d outcome differs after load", r.Command)
		}
		for _, m := range again.Matches {
			goals += len(m.Goals)
		}
	}
	if goals == 0 {
		t.Fatal("no goal details to compare")
	}
}
