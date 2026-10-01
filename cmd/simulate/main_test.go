package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/content"
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
	if !strings.HasPrefix(lines[0], "world seed=42 ") || lines[2] != "clubs=32 teams=32 players=640 free agents=0 retired=0" {
		t.Fatalf("unexpected header:\n%s", a)
	}
	rows := lines[5:37]
	if lines[37] != "" {
		t.Fatalf("expected 32 club rows then a blank line:\n%s", a)
	}
	for i, row := range rows {
		if !strings.HasPrefix(strings.TrimSpace(row), strconv.Itoa(i+1)+" ") {
			t.Fatalf("row %d not in club ID order: %q", i, row)
		}
	}
}

func TestRunPrintsFixturesGroupedByRound(t *testing.T) {
	out, err := runCLI(t, "-seed", "42")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(competition 1, season 1, schedule=v3): 8 teams, 14 rounds, 56 fixtures") {
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
		"  to Sat 2025-09-06 15:00 UTC: reached target",
		"    resolved Sat 2025-08-09 15:00 UTC: 4 rounds, 16 matches",
		"    resolved Sat 2025-09-06 15:00 UTC: 4 rounds, 16 matches",
		"calendar now: 5 completed, 0 awaiting results, 9 scheduled",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// The five user-less batches resolve on the way to the target; the
	// repeated Continue resolves nothing more and holds there.
	if n := strings.Count(out, "    resolved "); n != 5 {
		t.Errorf("resolved batches reported %d times, want 5", n)
	}
	if n := strings.Count(out, "  to Sat 2025-09-06 15:00 UTC: reached target"); n != 2 {
		t.Errorf("repeated target reported %d times, want 2", n)
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
	// The four leagues' 14 rounds (one header each), then the play-offs' 4
	// ties and the cup's 4+2+1 (named sections, not "Round" headers).
	if rounds != 56 || matchLines != 224+4+7 {
		t.Fatalf("printed %d rounds and %d matches, want 56 and 235", rounds, matchLines)
	}
	for _, want := range []string{"Promotion Play-off (Founders League / Founders Second Division)  Sat 2025-11-15 15:00 UTC", "  playoff\n", "Promotion Play-off (Harbour League / Harbour Second Division) season 1: playing to ", "Continental Cup quarter-final  Sat 2025-11-22 15:00 UTC", "Continental Cup final  Sat 2025-12-06 15:00 UTC", "Continental Cup 1 winner: "} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
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
	// A managed demo stops at the club's first matchday, which rests pending.
	if _, err := runCLI(t, "-seed", "42", "-club", "3", "-save", path); err != nil {
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
	fresh, _ := runCLI(t, "-seed", "42", "-club", "3", "-rounds", "1")
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

// matchLines returns the printed match lines (those starting "  F" after
// the Playing header), each without any manager note.
func matchLines(t *testing.T, out string) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(section(t, out, "Playing to"), "\n") {
		if strings.HasPrefix(line, "  F") {
			line, _, _ = strings.Cut(line, "  <- ")
			lines = append(lines, line)
		}
	}
	return lines
}

func TestManagedClubWithoutLineupsPlaysTheAISeason(t *testing.T) {
	plain, err := runCLI(t, "-seed", "42", "-season")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := runCLI(t, "-seed", "42", "-club", "3", "-season")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(managed, "\nmanaging club 3: ") || strings.Contains(plain, "managing club") {
		t.Fatal("manager line missing, or printed without -club")
	}
	// 14 league matches and a cup quarter-final (the play-off zone changed
	// with the manager's trades, so the club missed the play-off).
	if strings.Count(managed, "  <- AI lineup") != 15 || strings.Contains(managed, "your lineup") {
		t.Fatal("want 15 managed fixtures marked as AI lineups")
	}
	// The AI's lineup is the suggested one. (The season differs from the
	// unmanaged one: AI clubs trade among themselves in the first window,
	// and bid for a manager's players only when he lists them.)
	suggested, err := runCLI(t, "-seed", "42", "-club", "3", "-mentality", "balanced", "-season")
	if err != nil {
		t.Fatal(err)
	}
	// The cup results after the final table carry the lineup markers too.
	unmarked := func(out string) string {
		lines := strings.Split(section(t, out, "Final table"), "\n")
		for i, line := range lines {
			lines[i], _, _ = strings.Cut(line, "  <- ")
		}
		return strings.Join(lines, "\n")
	}
	if !slices.Equal(matchLines(t, managed), matchLines(t, suggested)) || unmarked(managed) != unmarked(suggested) {
		t.Fatal("managing a club without lineups changed results")
	}
}

func TestMentalityChangesOnlyTheManagedClubsMatches(t *testing.T) {
	plain, _ := runCLI(t, "-seed", "42", "-club", "3", "-season") // the same squads, AI lineups
	out, err := runCLI(t, "-seed", "42", "-club", "3", "-mentality", "attacking", "-season")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := runCLI(t, "-seed", "42", "-club", "3", "-mentality", "attacking", "-season")
	if out != again {
		t.Fatal("managed season is not reproducible")
	}
	if strings.Count(out, "  <- your lineup, attacking") != 17 { // 14 league matches and a cup run to the final
		t.Fatal("want 17 submitted attacking lineups")
	}
	// The leagues' matches; the play-offs' and the cup's depend on who
	// qualified.
	got, want := matchLines(t, out), matchLines(t, plain)
	if len(got) != 235 || len(want) != 235 {
		t.Fatalf("%d and %d match lines", len(got), len(want))
	}
	got, want = got[:224], want[:224]
	changed := 0
	for i := range got {
		if !strings.Contains(got[i], "QUI") {
			if got[i] != want[i] {
				t.Fatalf("unmanaged match changed:\n%s\n%s", got[i], want[i])
			}
		} else if got[i] != want[i] {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("attacking lineups changed none of the managed club's scores")
	}
}

func TestManagedCareerSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	straight, err := runCLI(t, "-seed", "42", "-club", "3", "-mentality", "attacking", "-season")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "-seed", "42", "-club", "3", "-mentality", "attacking", "-rounds", "7", "-save", path); err != nil {
		t.Fatal(err)
	}
	status, err := runCLI(t, "-load", path)
	if err != nil || !strings.Contains(status, "\nmanaging club 3: ") {
		t.Fatalf("status of a managed save (err %v):\n%s", err, status)
	}
	// The inbox shows the latest of the manager's messages.
	inboxSection := section(t, status, "Inbox (latest 10 of ")
	if !strings.Contains(inboxSection, "result: F") || !strings.Contains(inboxSection, "matchday: Founders League round 7,") ||
		strings.Count(inboxSection, "\n  ") != 10 {
		t.Fatalf("inbox section:\n%s", inboxSection)
	}
	// The status lists the squad with condition; some starters are tired.
	var rows, tired int
	for _, line := range strings.Split(section(t, status, "Squad (condition"), "\n")[3:] {
		if strings.HasSuffix(line, "%") {
			rows++
			if !strings.HasSuffix(line, " 100%") {
				tired++
			}
		}
	}
	if rows != 20 || tired == 0 || tired == 20 {
		t.Fatalf("squad: %d rows, %d tired:\n%s", rows, tired, status)
	}
	rest, err := runCLI(t, "-load", path, "-mentality", "attacking", "-season")
	if err != nil {
		t.Fatal(err)
	}
	if between(t, rest, "Round 8 ", "Final table") != between(t, straight, "Round 8 ", "Final table") ||
		section(t, rest, "Final table") != section(t, straight, "Final table") {
		t.Fatal("managed career differs after save/load")
	}
}

func TestManagementOptionsAreValidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	if _, err := runCLI(t, "-seed", "42", "-save", path); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"-club cannot be used with -load":       {"-load", path, "-club", "3"},
		"-engine cannot be used with -load":     {"-load", path, "-engine", "tick"},
		"-engine must be one of simple, tick":   {"-seed", "42", "-engine", "turbo"},
		"-club must name a club":                {"-seed", "42", "-club", "0"},
		"unknown club":                          {"-seed", "42", "-club", "99"},
		"-mentality needs -season or -rounds":   {"-seed", "42", "-club", "3", "-mentality", "attacking"},
		"want defensive, balanced or attacking": {"-seed", "42", "-club", "3", "-mentality", "reckless", "-season"},
		"-mentality needs a managed club":       {"-seed", "42", "-mentality", "attacking", "-season"},
	}
	cases["-mentality needs a managed club: use -club, or load"] = []string{"-load", path, "-mentality", "defensive", "-rounds", "1"}
	for want, args := range cases {
		if _, err := runCLI(t, args...); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want %q", args, err, want)
		}
	}
}

// -engine picks the new world's match engine; the header names it, and a
// saved career keeps it.
func TestEngineChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	out, err := runCLI(t, "-seed", "42", "-engine", "tick", "-rounds", "1", "-save", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, " engine=tick/v") {
		t.Fatalf("header does not name the tick engine:\n%s", out)
	}
	loaded, err := runCLI(t, "-load", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loaded, " engine=tick/v") {
		t.Fatalf("loaded career lost its engine:\n%s", loaded)
	}
	if plain, _ := runCLI(t, "-seed", "42"); !strings.Contains(plain, " engine=simple/v") {
		t.Fatalf("default engine is not simple:\n%s", plain)
	}
}

// -season plays only the current season; the saved career then continues
// with the next season, a year later, with new fixture IDs.
func TestSecondSeasonAfterSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	first, err := runCLI(t, "-seed", "42", "-season", "-save", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "Final table: Founders League season 1 (14/14 rounds)") || strings.Contains(first, "season 2") {
		t.Fatalf("first run:\n%s", first)
	}
	status, err := runCLI(t, "-load", path)
	// The four league ends scrolled out of the inbox's latest 10 once the
	// play-offs and the cup added their messages; the champions block and
	// the play-off, cup and next-season messages remain.
	if err != nil || !strings.Contains(status, "\nchampion: Founders League season 1: ") ||
		!strings.Contains(status, "Promotion Play-off (Harbour League / Harbour Second Division) season 1 decided\n") ||
		!strings.Contains(status, "Continental Cup season 1 ended: champion ") ||
		!strings.Contains(status, "Founders League season 2 scheduled: first kickoff Sat 2026-08-08 15:00 UTC") ||
		!strings.Contains(status, "Table: Founders League season 2 (0/14 rounds)") {
		t.Fatalf("off-season status (err %v):\n%s", err, status)
	}
	second, err := runCLI(t, "-load", path, "-season")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\nRound 1  Sat 2026-08-08 15:00 UTC", "\n  F229 ", "\n  F456 ", "Continental Cup 2 winner: ",
		"Final table: Founders League season 2 (14/14 rounds)", "\nChampion: ",
	} {
		if !strings.Contains(second, want) {
			t.Fatalf("second season output missing %q:\n%s", want, second)
		}
	}
	// Season 1's play-offs took IDs 225..228 (created at its season ends)
	// and its cup 341..344 and 457..459 (drawn at the play-off end, its
	// later rounds created as it went). Season 2's leagues fill the rest of
	// 229..456.
	if strings.Contains(second, "\n  F224 ") || strings.Contains(second, "\n  F225 ") ||
		strings.Contains(second, "\n  F341 ") || strings.Contains(second, "\n  F457 ") || strings.Contains(second, "\n  F459 ") {
		t.Fatal("second season printed another season's fixtures")
	}
	if strings.Count(second, "\nRound ") != 56 {
		t.Fatal("second run did not play exactly 14 rounds of each league")
	}
	// -rounds never crosses into the next season.
	capped, err := runCLI(t, "-seed", "42", "-rounds", "20")
	if err != nil || strings.Count(capped, "\nRound ") != 56 || !strings.Contains(capped, "Final table: Founders League season 1") || strings.Contains(capped, "Continental Cup") {
		t.Fatalf("-rounds 20 (err %v):\n%s", err, capped)
	}
}

func TestContentModeWithoutACareer(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"-content"}, &out, &errOut, func() (uint64, error) {
		t.Fatal("content report requested a world seed")
		return 0, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"content version ", "clubs 32, senior squad 20", "nations\n", "roster (overall", "leagues\n", "cups\n", "weekly wages", "no problems\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("content report missing %q:\n%s", want, out.String())
		}
	}
	if errOut.Len() != 0 || strings.Contains(out.String(), "world seed=") {
		t.Fatalf("content inspection started a career: stdout %q, stderr %q", out.String(), errOut.String())
	}
	again, err := runCLI(t, "-content")
	if err != nil || again != out.String() {
		t.Fatalf("content report changed between runs: %v", err)
	}
}

func TestContentModeRejectsCareerFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-seed", "42"}, {"-load", "missing.json"}, {"-save", filepath.Join(t.TempDir(), "career.json")},
		{"-season"}, {"-rounds", "1"}, {"-club", "3"}, {"-mentality", "attacking"}, {"-engine", "tick"},
	} {
		t.Run(args[0], func(t *testing.T) {
			out, err := runCLI(t, append([]string{"-content"}, args...)...)
			if err == nil || !strings.Contains(err.Error(), "-content cannot be combined") || out != "" {
				t.Fatalf("-content %v: output %q, error %v", args, out, err)
			}
		})
	}
	if out, err := runCLI(t, "-content", "extra"); err == nil || out != "" {
		t.Fatalf("content mode accepted positional argument: output %q, error %v", out, err)
	}
}

func TestContentProblemsArePrintedAndFail(t *testing.T) {
	definitions := content.DefaultSet()
	definitions.Leagues = definitions.Leagues[:3]
	var out bytes.Buffer
	err := printContent(&out, definitions)
	if err == nil || !strings.Contains(err.Error(), "content report contains problems") {
		t.Fatalf("invalid content succeeded: %v", err)
	}
	if !strings.Contains(out.String(), "problem: content: leagues take 24 entrants but the nations generate 32 clubs") || strings.Contains(out.String(), "no problems") {
		t.Fatalf("invalid content diagnostics missing:\n%s", out.String())
	}
}

type failingContentWriter struct{ err error }

func (w failingContentWriter) Write([]byte) (int, error) { return 0, w.err }

func TestContentModeReportsWriteErrors(t *testing.T) {
	writeError := errors.New("output unavailable")
	err := run([]string{"-content"}, failingContentWriter{writeError}, io.Discard, func() (uint64, error) {
		t.Fatal("content report requested a world seed")
		return 0, nil
	})
	if !errors.Is(err, writeError) {
		t.Fatalf("write failure lost: %v", err)
	}
}
