package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
)

func world(t *testing.T) *app.World {
	t.Helper()
	w, err := app.NewWorld(app.DefaultConfig(random.Seed(42)))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// advance resolves n batches through the public API.
func advance(t *testing.T, w *app.World, n int) {
	t.Helper()
	end := w.Schedules()[0].Rounds[13].Kickoff + 1440
	for range n {
		res, err := w.Continue(end)
		if err != nil {
			t.Fatal(err)
		}
		ready, ok := res.(app.FixtureRoundReady)
		if !ok {
			return
		}
		cmd := app.ResolveRounds{ID: w.NextCommandID(), ExpectedRevision: ready.Revision}
		for _, r := range ready.Rounds {
			cmd.Rounds = append(cmd.Rounds, r.Round)
		}
		if _, err := w.ResolveRounds(cmd); err != nil {
			t.Fatal(err)
		}
	}
}

func encoded(t *testing.T, w *app.World) []byte {
	t.Helper()
	data, err := Encode(w.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// withPayload re-envelopes a (possibly tampered) payload with a valid
// checksum, to reach checks behind the checksum.
func withPayload(t *testing.T, format string, schema int, payload []byte) []byte {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compact.Bytes())
	data, err := json.Marshal(envelope{Format: format, Schema: schema, PayloadSHA256: hex.EncodeToString(sum[:]), Payload: compact.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	w := world(t)
	advance(t, w, 4)
	data := encoded(t, w)
	snap, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap, w.Snapshot()) {
		t.Fatal("decoded snapshot differs")
	}
	again, err := Encode(snap)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatal("encoding is not deterministic")
	}
}

func TestDecodeRejectsTruncatedFiles(t *testing.T) {
	data := encoded(t, world(t))
	for n := 0; n < len(data)-1; n += 1 + n/50 {
		if _, err := Decode(data[:n]); !errors.Is(err, ErrMalformedSave) {
			t.Fatalf("prefix of %d/%d bytes: err = %v", n, len(data), err)
		}
	}
	// The trailing newline is optional; everything before it is required.
	if _, err := Decode(data[:len(data)-2]); !errors.Is(err, ErrMalformedSave) {
		t.Fatal("file missing its closing brace accepted")
	}
}

func TestDecodeRejectsCorruptOrUnsupportedFiles(t *testing.T) {
	w := world(t)
	advance(t, w, 2)
	data := encoded(t, w)
	payload, _ := json.Marshal(w.Snapshot())

	corrupt := bytes.Replace(data, []byte(`"Seed": 42`), []byte(`"Seed": 43`), 1)
	if bytes.Equal(corrupt, data) {
		corrupt = bytes.Replace(data, []byte(`"Seed":42`), []byte(`"Seed":43`), 1)
	}
	if bytes.Equal(corrupt, data) {
		t.Fatal("test could not find the seed to corrupt")
	}
	withUnknown := bytes.Replace(payload, []byte(`{"Seed":`), []byte(`{"Extra":1,"Seed":`), 1)

	cases := map[string]struct {
		data []byte
		want error
	}{
		"payload edited without checksum": {corrupt, ErrMalformedSave},
		"trailing data":                   {append(append([]byte{}, data...), []byte(`{}`)...), ErrMalformedSave},
		"not JSON":                        {[]byte("save"), ErrMalformedSave},
		"empty":                           {nil, ErrMalformedSave},
		"unknown envelope field":          {bytes.Replace(data, []byte(`"Format"`), []byte(`"Extra": 1, "Format"`), 1), ErrMalformedSave},
		"unknown payload field":           {withPayload(t, Format, SchemaVersion, withUnknown), ErrMalformedSave},
		"other format":                    {withPayload(t, "other/save", SchemaVersion, payload), ErrUnsupportedSave},
		"future schema":                   {withPayload(t, Format, SchemaVersion+1, payload), ErrUnsupportedSave},
		"old schema":                      {withPayload(t, Format, 0, payload), ErrUnsupportedSave},
		"six attributes (schema 17)":      {withPayload(t, Format, 17, payload), ErrUnsupportedSave},
	}
	for name, c := range cases {
		if _, err := Decode(c.data); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestLoadRejectsInvalidStateAndVersions(t *testing.T) {
	dir := t.TempDir()
	w := world(t)
	advance(t, w, 2)
	cases := map[string]struct {
		mutate func(*app.WorldSnapshot)
		want   error
	}{
		"broken reference":  {func(s *app.WorldSnapshot) { s.Employment[0].Team = 99 }, app.ErrInvalidSave},
		"engine version":    {func(s *app.WorldSnapshot) { s.Versions.EngineVersion++ }, app.ErrIncompatibleSave},
		"allocator too low": {func(s *app.WorldSnapshot) { s.Competitions.LastFixture = 1 }, app.ErrInvalidSave},
	}
	for name, c := range cases {
		snap := w.Snapshot()
		c.mutate(&snap)
		payload, _ := json.Marshal(snap)
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, withPayload(t, Format, SchemaVersion, payload), 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if !errors.Is(err, c.want) || loaded != nil {
			t.Errorf("%s: world %v, err = %v, want %v", name, loaded != nil, err, c.want)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
}

func TestSaveLoadCycleFinishesSeasonIdentically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	straight := world(t)
	advance(t, straight, 20)

	w := world(t)
	advance(t, w, 7)
	if err := Save(path, w); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	advance(t, loaded, 20)
	if !reflect.DeepEqual(loaded.Snapshot(), straight.Snapshot()) {
		t.Fatal("season finished after a file save/load differs from an uninterrupted one")
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A failed save at any step leaves the previous save loadable and no
// temporary files behind.
func TestFailedSaveKeepsExistingSave(t *testing.T) {
	boom := errors.New("injected")
	failing := map[string]fileOps{
		"create temp": {createTemp: func(string, string) (*os.File, error) { return nil, boom }, write: osFS.write, sync: osFS.sync, rename: osFS.rename},
		"partial write": {createTemp: osFS.createTemp, write: func(f *os.File, d []byte) error {
			f.Write(d[:len(d)/2])
			return boom
		}, sync: osFS.sync, rename: osFS.rename},
		"sync":   {createTemp: osFS.createTemp, write: osFS.write, sync: func(*os.File) error { return boom }, rename: osFS.rename},
		"rename": {createTemp: osFS.createTemp, write: osFS.write, sync: osFS.sync, rename: func(string, string) error { return boom }},
	}
	for name, fs := range failing {
		dir := t.TempDir()
		path := filepath.Join(dir, "career.json")
		old := world(t)
		if err := Save(path, old); err != nil {
			t.Fatal(err)
		}
		newer := world(t)
		advance(t, newer, 3)
		if err := saveWith(path, newer, fs); !errors.Is(err, boom) {
			t.Fatalf("%s: err = %v", name, err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("%s: previous save unusable: %v", name, err)
		}
		if !reflect.DeepEqual(loaded.Snapshot(), old.Snapshot()) {
			t.Fatalf("%s: previous save was modified", name)
		}
		if names := dirEntries(t, dir); len(names) != 1 || names[0] != "career.json" {
			t.Fatalf("%s: directory contains %v", name, names)
		}
	}
}

func TestSaveKeepsOneVerifiedPreviousSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "career.json")
	old := world(t)
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PreviousPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first save unexpectedly has previous file: %v", err)
	}
	newer := world(t)
	advance(t, newer, 2)
	if err := Save(path, newer); err != nil {
		t.Fatal(err)
	}
	current, err := Load(path)
	if err != nil || !reflect.DeepEqual(current.Snapshot(), newer.Snapshot()) {
		t.Fatalf("current save did not advance: err=%v", err)
	}
	previous, err := Load(PreviousPath(path))
	if err != nil || !reflect.DeepEqual(previous.Snapshot(), old.Snapshot()) {
		t.Fatalf("previous save was not preserved: err=%v", err)
	}
}

func TestSaveRefusesDamagedOrIncompatibleCurrentWithoutChangingFiles(t *testing.T) {
	for name, current := range map[string][]byte{
		"damaged":      []byte("not a save"),
		"incompatible": withPayload(t, Format, SchemaVersion+1, mustJSON(t, world(t).Snapshot())),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "career.json")
			backup := PreviousPath(path)
			backupBytes := []byte("keep this file exactly")
			if err := os.WriteFile(path, current, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, backupBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			beforeCurrent, _ := os.ReadFile(path)
			beforePrevious, _ := os.ReadFile(backup)
			if err := Save(path, world(t)); err == nil {
				t.Fatal("save replaced a damaged or incompatible career")
			}
			afterCurrent, _ := os.ReadFile(path)
			afterPrevious, _ := os.ReadFile(backup)
			if !bytes.Equal(afterCurrent, beforeCurrent) || !bytes.Equal(afterPrevious, beforePrevious) {
				t.Fatal("refused save modified one of the career files")
			}
		})
	}
}

func TestRecoverPreviousExplicitlyRestoresSelectedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "career.json")
	old, newer := world(t), world(t)
	advance(t, newer, 2)
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, newer); err != nil {
		t.Fatal(err)
	}
	previousBefore, err := os.ReadFile(PreviousPath(path))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := RecoverPrevious(path)
	if err != nil || !reflect.DeepEqual(recovered.Snapshot(), old.Snapshot()) {
		t.Fatalf("recovery = %v, %v", recovered, err)
	}
	current, err := Load(path)
	if err != nil || !reflect.DeepEqual(current.Snapshot(), old.Snapshot()) {
		t.Fatalf("recovered selected path = %v, %v", current, err)
	}
	previousAfter, err := os.ReadFile(PreviousPath(path))
	if err != nil || !bytes.Equal(previousAfter, previousBefore) {
		t.Fatal("recovery changed its source file")
	}
}

func TestRecoverPreviousAfterNewestSaveIsDamaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "career.json")
	old, newer := world(t), world(t)
	advance(t, newer, 2)
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("truncated newest save"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrMalformedSave) {
		t.Fatalf("damaged newest save load error = %v", err)
	}
	recovered, err := RecoverPrevious(path)
	if err != nil || !reflect.DeepEqual(recovered.Snapshot(), old.Snapshot()) {
		t.Fatalf("recovery = %v, %v", recovered, err)
	}
	current, err := Load(path)
	if err != nil || !reflect.DeepEqual(current.Snapshot(), old.Snapshot()) {
		t.Fatalf("recovered selected path = %v, %v", current, err)
	}
}

func TestFailedCurrentReplacementKeepsCurrentAndPrevious(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "career.json")
	old, newer := world(t), world(t)
	advance(t, newer, 1)
	if err := Save(path, old); err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("injected target rename interruption")
	fs := osFS
	fs.rename = func(oldpath, newpath string) error {
		if newpath == path {
			return boom
		}
		return osFS.rename(oldpath, newpath)
	}
	if err := saveWith(path, newer, fs); !errors.Is(err, boom) {
		t.Fatalf("save error = %v", err)
	}
	for _, savePath := range []string{path, PreviousPath(path)} {
		got, err := os.ReadFile(savePath)
		if err != nil || !bytes.Equal(got, oldBytes) {
			t.Fatalf("%s after interruption differs from original: %v", savePath, err)
		}
		if _, err := Load(savePath); err != nil {
			t.Fatalf("%s is not loadable after interruption: %v", savePath, err)
		}
	}
	if names := dirEntries(t, dir); len(names) != 2 {
		t.Fatalf("temporary file left behind: %v", names)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSaveReplacesExistingFileAndReportsBadPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "career.json")
	w := world(t)
	if err := Save(path, w); err != nil {
		t.Fatal(err)
	}
	advance(t, w, 1)
	if err := Save(path, w); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Revision() != w.Revision() {
		t.Fatalf("replacement not loaded: %v", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if err := Save(filepath.Join(dir, "no-such-dir", "x.json"), w); err == nil || !strings.Contains(err.Error(), "no-such-dir") {
		t.Fatalf("save into missing directory: %v", err)
	}
}

// A managed career saved while a batch is pending keeps its club and the
// submitted lineup, and resolves identically after loading from a file.
func TestPendingLineupSurvivesSaveFile(t *testing.T) {
	cfg := app.DefaultConfig(random.Seed(42))
	cfg.UserClub = 3
	w, err := app.NewWorld(cfg)
	if err != nil {
		t.Fatal(err)
	}
	advance(t, w, 2)
	res, err := w.Continue(w.Schedules()[0].Rounds[13].Kickoff)
	if err != nil {
		t.Fatal(err)
	}
	ready, ok := res.(app.FixtureRoundReady)
	if !ok || len(ready.UserFixtures) != 1 {
		t.Fatalf("Continue = %#v, want one user fixture pending", res)
	}
	fixture := ready.UserFixtures[0]
	lineup, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	lineup.Tactics.Mentality = matches.Defensive
	if _, err := w.SubmitLineup(app.SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: lineup}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "career.json")
	if err := Save(path, w); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if club, _ := loaded.UserClub(); club != 3 {
		t.Fatalf("user club %d after load", club)
	}
	if got, ok := loaded.SubmittedLineup(fixture); !ok || !got.Equal(lineup) {
		t.Fatal("submitted lineup lost")
	}
	cmd := app.ResolveRounds{ID: w.NextCommandID(), ExpectedRevision: w.Revision()}
	for _, r := range ready.Rounds {
		cmd.Rounds = append(cmd.Rounds, r.Round)
	}
	want, err := w.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(loaded.Snapshot(), w.Snapshot()) {
		t.Fatal("loaded career resolved differently")
	}
}

// Read acknowledgements and retry records survive the actual save codec.
func TestInboxReadSaveRoundTrip(t *testing.T) {
	cfg := app.DefaultConfig(42)
	cfg.UserClub = 3
	w, err := app.NewWorld(cfg)
	if err != nil {
		t.Fatal(err)
	}
	advance(t, w, 2)
	cmd := app.MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: w.Inbox()[0].Event}
	result, err := w.MarkInboxRead(cmd)
	if err != nil {
		t.Fatal(err)
	}
	data := encoded(t, w)
	snap, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := app.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.Inbox(), loaded.Inbox()) || w.UnreadInboxCount() != loaded.UnreadInboxCount() {
		t.Fatal("codec lost read flags")
	}
	retry, err := loaded.MarkInboxRead(cmd)
	if err != nil || retry != result || !bytes.Equal(data, encoded(t, loaded)) {
		t.Fatal("loaded retry changed save")
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(withPayload(t, Format, 14, env.Payload)); !errors.Is(err, ErrUnsupportedSave) {
		t.Fatalf("schema 14 was not explicitly refused: %v", err)
	}
}
