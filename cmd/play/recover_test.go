package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/storage"
)

// damagedPair leaves path damaged and path.previous holding the earlier
// save, taken before the first matchday.
func damagedPair(t *testing.T) (path string, previous []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "career.json")
	play(t, []string{"-seed", "42", "-club", "3"}, "save "+path, "continue", "save "+path, "quit")
	previous, err := os.ReadFile(storage.PreviousPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, previous
}

func files(t *testing.T, path string) [2]string {
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

func runScript(args []string, lines ...string) (string, error) {
	var out, errOut bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	err := run(args, in, &out, &errOut, func() (uint64, error) { return 42, nil })
	return out.String(), err
}

func TestDecliningRecoveryAtStartupChangesNothing(t *testing.T) {
	path, _ := damagedPair(t)
	before := files(t, path)
	for _, answer := range []string{"no", "", "maybe"} {
		out, err := runScript([]string{"-load", path}, answer)
		if err == nil || !strings.Contains(err.Error(), "malformed save") {
			t.Fatalf("answer %q: err = %v", answer, err)
		}
		contains(t, out, "has a previous save", "Recovering replaces "+path+" with it and loads it.", "Recover the previous save? (yes/no)", "Nothing was changed.")
		if files(t, path) != before {
			t.Fatalf("answer %q changed a file", answer)
		}
	}
	// End of input is not a choice either.
	if _, err := runScript([]string{"-load", path}); err == nil || files(t, path) != before {
		t.Fatalf("end of input: err = %v", err)
	}
}

func TestChoosingRecoveryAtStartupRestoresThePreviousCareer(t *testing.T) {
	path, previous := damagedPair(t)
	out, err := runScript([]string{"-load", path}, "yes", "status", "quit")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	contains(t, out, "Recovered "+path, "You manage Quillford FC (QUI).", "0 of 14 rounds played")
	after := files(t, path)
	if after[0] != string(previous) || after[1] != string(previous) {
		t.Fatal("the selected file was not restored from the previous save")
	}
	if _, err := storage.Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestFailedRecoveryChangesNothing(t *testing.T) {
	path, _ := damagedPair(t)
	if err := os.WriteFile(storage.PreviousPath(path), []byte("{also damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := files(t, path)
	if _, err := runScript([]string{"-load", path}, "yes"); err == nil || !strings.Contains(err.Error(), "no file was changed") {
		t.Fatalf("err = %v", err)
	}
	if files(t, path) != before {
		t.Fatal("a failed recovery changed a file")
	}
}

func TestRefusedSaveOffersRecoveryInSession(t *testing.T) {
	path, previous := damagedPair(t)
	before := files(t, path)
	out, err := runScript([]string{"-seed", "42", "-club", "3"},
		"continue", "save "+path, "recover", "recover extra args here", "status")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	contains(t, out, "was not changed and the game was not saved.", "Type recover "+path+" to replace it with the previous save",
		"Recovering replaces "+path, "whose unsaved progress is lost", "Type recover "+path+" yes to do it. Nothing has changed.",
		"usage: recover [FILE] [yes]")
	if files(t, path) != before {
		t.Fatal("refusing a save or explaining recovery changed a file")
	}

	out, err = runScript([]string{"-seed", "42", "-club", "3"}, "continue", "save "+path, "recover yes", "status", "quit")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	contains(t, out, "Recovered "+path, "0 of 14 rounds played")
	if after := files(t, path); after[0] != string(previous) {
		t.Fatal("the selected file was not restored")
	}
	if strings.Contains(out, "unsaved progress") {
		t.Fatalf("a recovered career reads as unsaved:\n%s", out)
	}
	out, _ = runScript([]string{"-seed", "42", "-club", "3"}, "recover "+filepath.Join(t.TempDir(), "none.json"))
	contains(t, out, "has no previous save to recover")
}
