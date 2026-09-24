package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut, func() (uint64, error) {
		t.Fatal("random seed drawn although -seed was given")
		return 0, nil
	})
	return out.String(), err
}

func TestRunWithoutSeedUsesAndReportsRandomSeed(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run(nil, &out, &errOut, func() (uint64, error) { return 42, nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "using random seed 42 (rerun with -seed 42 to reproduce)") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	// A random run is reproducible from the reported seed.
	explicit, err := runCLI(t, "-seed", "42")
	if err != nil || out.String() != explicit {
		t.Fatalf("random-seed run differs from -seed 42 (err %v)", err)
	}

	out.Reset()
	if err := run(nil, &out, &errOut, func() (uint64, error) { return 0, errors.New("no entropy") }); err == nil {
		t.Fatal("seed source failure ignored")
	}
	if out.Len() != 0 {
		t.Fatal("output written despite seed failure")
	}
}

func TestCryptoSeedVaries(t *testing.T) {
	a, errA := cryptoSeed()
	b, errB := cryptoSeed()
	if errA != nil || errB != nil || a == b {
		t.Fatalf("cryptoSeed = %d, %d (%v, %v)", a, b, errA, errB)
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	if _, err := runCLI(t, "-seed", "42", "extra"); err == nil {
		t.Fatal("extra arguments accepted")
	}
	if _, err := runCLI(t, "-seed", "-1"); err == nil {
		t.Fatal("negative seed accepted")
	}
}

func TestRunOutputIsDeterministic(t *testing.T) {
	a, err := runCLI(t, "-seed", "42")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := runCLI(t, "-seed", "42")
	if a != b {
		t.Fatal("same seed printed different output")
	}
	c, _ := runCLI(t, "-seed", "43")
	if a == c {
		t.Fatal("different seeds printed identical output")
	}

	lines := strings.Split(strings.TrimSpace(a), "\n")
	if !strings.HasPrefix(lines[0], "world seed=42 ") || lines[2] != "clubs=8 teams=8 players=160" {
		t.Fatalf("unexpected header:\n%s", a)
	}
	rows := lines[5:13]
	if lines[13] != "" {
		t.Fatalf("expected 8 club rows then a blank line:\n%s", a)
	}
	for i, row := range rows {
		if !strings.HasPrefix(strings.TrimSpace(row), string(rune('1'+i))+" ") {
			t.Fatalf("row %d not in club ID order: %q", i, row)
		}
	}
}

func TestRunPrintsFixturesGroupedByRound(t *testing.T) {
	out, err := runCLI(t, "-seed", "42")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(competition 1, season 1, schedule=v1): 8 teams, 14 rounds, 56 fixtures") {
		t.Fatalf("missing schedule header:\n%s", out)
	}
	round, fixtures, perRound := 0, 0, map[int]int{}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "Round "):
			round++
			if !strings.HasPrefix(line, fmt.Sprintf("Round %-2d ", round)) ||
				!strings.Contains(line, " UTC  t=") || !strings.HasSuffix(line, "  scheduled") {
				t.Fatalf("rounds out of order or malformed: %q, want Round %d", line, round)
			}
		case strings.HasPrefix(line, "  F"):
			fixtures++
			perRound[round]++
		}
	}
	if round != 14 || fixtures != 56 {
		t.Fatalf("printed %d rounds and %d fixtures, want 14 and 56", round, fixtures)
	}
	for r := 1; r <= 14; r++ {
		if perRound[r] != 4 {
			t.Fatalf("round %d printed %d fixtures, want 4", r, perRound[r])
		}
	}
}

func TestRunPrintsCalendarAndContinueDemo(t *testing.T) {
	out, err := runCLI(t, "-seed", "42")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"career epoch Tue 2025-07-01 00:00 UTC (t=0)",
		"Round 1  Sat 2025-08-09 15:00 UTC  t=57060  scheduled",
		"Round 14 Sat 2025-11-08 15:00 UTC  t=188100  scheduled",
		"Continue (now Tue 2025-07-01 00:00 UTC)",
		"  to Fri 2025-08-08 15:00 UTC: reached target",
		"  to Sat 2025-09-06 15:00 UTC: fixture round ready, now Sat 2025-08-09 15:00 UTC",
		"    competition 1 season 1 round 1 kicked off Sat 2025-08-09 15:00 UTC, 4 fixtures awaiting results",
		"calendar now: 1 awaiting results, 13 scheduled",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// The repeated Continue reports the same pending round.
	if n := strings.Count(out, "fixture round ready, now Sat 2025-08-09 15:00 UTC"); n != 2 {
		t.Errorf("pending round reported %d times, want 2", n)
	}
}

func TestSeasonModePlaysEveryRound(t *testing.T) {
	out, err := runCLI(t, "-seed", "42", "-season")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := runCLI(t, "-seed", "42", "-season")
	if out != again {
		t.Fatal("season output is not reproducible")
	}
	rounds, matchLines := 0, 0
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "Round "):
			rounds++
		case strings.HasPrefix(line, "  F"):
			matchLines++
		}
	}
	if rounds != 14 || matchLines != 56 {
		t.Fatalf("printed %d rounds and %d matches, want 14 and 56", rounds, matchLines)
	}
	if !strings.Contains(out, "Final table: Founders League season 1 (14/14 rounds)") || !strings.Contains(out, "\nChampion: ") {
		t.Fatalf("missing final table or champion:\n%s", out)
	}
	if strings.Contains(out, "Continue (now") {
		t.Fatal("season mode printed the Continue demo")
	}
}

// section returns out from the first line starting with prefix to the end.
func section(t *testing.T, out, prefix string) string {
	t.Helper()
	i := strings.Index(out, "\n"+prefix)
	if i < 0 {
		t.Fatalf("output has no %q section:\n%s", prefix, out)
	}
	return out[i:]
}

// between returns the text from the line starting with from up to the line
// starting with to.
func between(t *testing.T, out, from, to string) string {
	t.Helper()
	rest := section(t, out, from)
	end := strings.Index(rest, "\n"+to)
	if end < 0 {
		t.Fatalf("no %q after %q", to, from)
	}
	return rest[:end]
}

func TestSaveLoadCycleMatchesUninterruptedSeason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	straight, err := runCLI(t, "-seed", "42", "-season")
	if err != nil {
		t.Fatal(err)
	}
	first, err := runCLI(t, "-seed", "42", "-rounds", "7", "-save", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "Table: Founders League season 1 (7/14 rounds)") || !strings.Contains(first, "\nsaved "+path+": revision ") {
		t.Fatalf("partial run output:\n%s", first)
	}
	rest, err := runCLI(t, "-load", path, "-season")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rest, "\nRound 7 ") || !strings.Contains(rest, "\nRound 8 ") {
		t.Fatalf("loaded run did not continue from round 8:\n%s", rest)
	}
	if section(t, rest, "Final table") != section(t, straight, "Final table") {
		t.Fatal("final table after save/load differs from an uninterrupted season")
	}
	// Rounds 8-14 print exactly as in the uninterrupted run.
	if between(t, rest, "Round 8 ", "Final table") != between(t, straight, "Round 8 ", "Final table") {
		t.Fatal("round 8-14 results differ after save/load")
	}
}

func TestLoadWithoutModePrintsStatusAndChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	if _, err := runCLI(t, "-seed", "42", "-save", path); err != nil { // demo leaves round 1 pending
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	out, err := runCLI(t, "-load", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status: now Sat 2025-08-09 15:00 UTC", "pending: competition 1 season 1 round 1", "Table: Founders League season 1 (0/14 rounds)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("loading without -save modified the file")
	}
	// Resolving the saved pending batch gives the same round 1 as a fresh run.
	loaded, err := runCLI(t, "-load", path, "-rounds", "1")
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := runCLI(t, "-seed", "42", "-rounds", "1")
	if section(t, loaded, "Round 1 ") != section(t, fresh, "Round 1 ") {
		t.Fatal("pending batch resolved differently after load")
	}
}

func TestIncompatibleOptionsAreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	if _, err := runCLI(t, "-seed", "42", "-save", path); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"-seed cannot be used with -load": {"-load", path, "-seed", "42"},
		"-season and -rounds":             {"-seed", "42", "-season", "-rounds", "2"},
		"-rounds must be at least 1":      {"-seed", "42", "-rounds", "0"},
		"-load needs a file name":         {"-load", ""},
		"-save needs a file name":         {"-seed", "42", "-save", ""},
	}
	for want, args := range cases {
		if _, err := runCLI(t, args...); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want %q", args, err, want)
		}
	}
}

func TestLoadAndSaveErrorsAreClear(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.json")
	os.WriteFile(garbage, []byte(`{"Format": "project-zimble/save", "Sch`), 0o644)
	for args, want := range map[string]string{
		"-load " + filepath.Join(dir, "missing.json"): "load " + filepath.Join(dir, "missing.json"),
		"-load " + garbage: "malformed save",
		"-seed 42 -save " + filepath.Join(dir, "nodir", "x.json"): "save " + filepath.Join(dir, "nodir", "x.json"),
	} {
		_, err := runCLI(t, strings.Fields(args)...)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", args, err, want)
		}
	}
}
