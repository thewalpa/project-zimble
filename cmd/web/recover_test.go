package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/storage"
)

// twoSaves writes a career twice, so path holds the second save and
// path.previous the first, then damages the selected file.
func twoSaves(t *testing.T) (path string, previous []byte) {
	t.Helper()
	c := career(t)
	path = c.s.savePath
	c.post("/save", nil)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c.post("/continue", nil)
	c.post("/save", nil)
	if got, err := os.ReadFile(storage.PreviousPath(path)); err != nil || string(got) != string(first) {
		t.Fatalf("previous save not retained: %v", err)
	}
	if err := os.WriteFile(path, []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, first
}

func snapshotFiles(t *testing.T, path string) [2]string {
	t.Helper()
	var out [2]string
	for i, p := range []string{path, storage.PreviousPath(path)} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = string(b)
	}
	return out
}

func TestRecoverFromDamagedSaveAtStartup(t *testing.T) {
	path, previous := twoSaves(t)
	before := snapshotFiles(t, path)

	c := newClient(t, config{loadPath: path, savePath: path})
	page := c.get("/")
	contains(t, page, "Cannot be loaded", "New career",
		"Recovering replaces <b>career.json</b> with the previous save (<b>career.json.previous</b>)",
		"Nothing changes unless you choose it.")
	if c.s.w != nil {
		t.Fatal("a career was loaded from a damaged save")
	}
	// Declining is simply not choosing: browsing on changes nothing.
	c.get("/")
	if got := snapshotFiles(t, path); got != before {
		t.Fatal("a file changed without the player choosing recovery")
	}

	page = c.post("/recover", url.Values{"file": {path}})
	contains(t, page, "Recovered career.json from the previous save career.json.previous", "Round 1 v Brackenmoor Town (home)")
	after := snapshotFiles(t, path)
	if after[0] != string(previous) || after[1] != string(previous) {
		t.Fatal("recovery did not restore the selected file and retain the previous one")
	}
	if _, err := storage.Load(path); err != nil {
		t.Fatalf("recovered file: %v", err)
	}
	if page = c.post("/save", nil); strings.Contains(page, "refusing") {
		t.Fatal("saving is still refused after recovery")
	}
}

func TestFailedLoadOffersRecoveryAndChangesNothing(t *testing.T) {
	path, _ := twoSaves(t)
	before := snapshotFiles(t, path)

	c := newClient(t, config{seed: 42, club: 3, savePath: filepath.Join(t.TempDir(), "other.json"), savesDir: filepath.Dir(path)})
	page := c.post("/load", url.Values{"file": {filepath.Base(path)}})
	contains(t, page, "load career.json:", "Replace career.json with the previous save", "Cannot be loaded")
	if got := snapshotFiles(t, path); got != before {
		t.Fatal("a failed load changed a file")
	}
	// The choice is not an automatic action; the career stays as it was.
	if c.s.w == nil || c.s.savePath == path {
		t.Fatal("the career changed without the player choosing recovery")
	}
	if page = c.post("/recover", url.Values{"file": {filepath.Base(path)}}); !strings.Contains(page, "Recovered career.json") {
		t.Fatalf("recovery refused:\n%s", notesOf(page))
	}
}

func TestFailedSaveKeepsDamagedFileAndOffersRecovery(t *testing.T) {
	c := career(t)
	path := c.s.savePath
	c.post("/save", nil)
	c.post("/continue", nil)
	c.post("/save", nil)
	if err := os.WriteFile(path, []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotFiles(t, path)
	c.post("/continue", nil)

	page := c.post("/save", nil)
	contains(t, page, "refusing to replace", "career.json was not changed and the game was not saved",
		"unsaved changes that will be lost", "Replace career.json with the previous save")
	if got := snapshotFiles(t, path); got != before {
		t.Fatal("a refused save changed a file")
	}
	// The home page keeps the choice available.
	contains(t, c.get("/"), "career.json cannot be loaded", "Replace career.json with the previous save")

	page = c.post("/recover", url.Values{"file": {path}})
	contains(t, page, "Recovered career.json")
}

func TestFailedRecoveryChangesNothing(t *testing.T) {
	path, _ := twoSaves(t)
	if err := os.WriteFile(storage.PreviousPath(path), []byte("{also damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotFiles(t, path)

	c := newClient(t, config{loadPath: path, savePath: path})
	c.get("/")
	page := c.post("/recover", url.Values{"file": {path}})
	contains(t, page, "no file was changed")
	if got := snapshotFiles(t, path); got != before {
		t.Fatal("a failed recovery changed a file")
	}
	if c.s.w != nil {
		t.Fatal("a career was loaded by a failed recovery")
	}

	// Without a previous save there is nothing to recover, and nothing is offered.
	other := filepath.Join(t.TempDir(), "lonely.json")
	if err := os.WriteFile(other, []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newServer(config{loadPath: other}); err == nil {
		t.Fatal("a damaged save without a previous one started a server")
	}
	page = c.post("/recover", url.Values{"file": {"../other.json"}})
	contains(t, page, "invalid save file path")
}
