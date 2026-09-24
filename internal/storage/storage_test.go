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
