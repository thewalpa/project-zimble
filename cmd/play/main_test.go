package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/storage"
)

// play runs the game with scripted input lines and returns its output.
func play(t *testing.T, args []string, lines ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	err := run(args, in, &out, &errOut, func() (uint64, error) { return 42, nil })
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return out.String()
}

func contains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("output missing %q:\n%s", w, out)
		}
	}
}

func TestChooseAClubAtThePrompt(t *testing.T) {
	out := play(t, nil, "99", "abc", "3", "status", "quit", "quit")
	contains(t, out, "New career (seed 42). Choose your club:", "Please enter one of the club IDs above.",
		"New career, seed 42 (start again with -seed 42 -club 3).",
		"You manage Quillford FC (QUI).", "Next: round 1 v Brackenmoor Town (home)",
		"You have unsaved progress.", "Goodbye.")
	if strings.Count(out, "Please enter one of the club IDs above.") != 2 {
		t.Fatal("both invalid club choices should be rejected")
	}
}

// Editing the lineup and playing submits exactly the edited lineup; the
// session is reproducible and the career saves and loads.
func TestEditedLineupIsPlayedAndSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	script := []string{"continue", "swap 57 58", "mentality attacking", "role 44 mf", "continue", "save " + path, "quit"}
	out := play(t, []string{"-seed", "42", "-club", "3"}, script...)
	again := play(t, []string{"-seed", "42", "-club", "3"}, script...)
	if out != again {
		t.Fatal("the same session printed different output")
	}
	contains(t, out, "MATCHDAY Sat 2025-08-09 15:00 UTC: round 1 v Brackenmoor Town (home).",
		"your changes (used when you continue)", "Mentality: attacking", "(out of position)",
		"FULL TIME  Quillford FC", "Other results:", "New in your inbox:", "Saved to "+path)
	if strings.Contains(out, "player ") {
		t.Fatal("a scorer was printed without a name")
	}

	w, err := storage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := w.SubmittedLineup(3) // round 1 fixture of club 3
	if !ok || l.Tactics.Mentality != matches.Attacking || l.Starters[10].Player != 58 || l.Starters[1].Player != 44 || l.Starters[1].Role != matches.Midfielder {
		t.Fatalf("submitted lineup %+v, %v", l, ok)
	}
	if _, ok := w.SubmittedLineup(8); ok {
		t.Fatal("a lineup was submitted for a match that was not edited")
	}

	loaded := play(t, []string{"-load", path}, "status", "fixtures", "quit")
	contains(t, loaded, "You manage Quillford FC (QUI).", "1 of 14 rounds played",
		"Next: round 2 v Hollowick Town (away), Sat 2025-08-16 15:00 UTC.", "R1  Sat 2025-08-09 15:00 UTC  F3   v Brackenmoor Town (home)", "Balance 2,")
	if strings.Contains(loaded, "unsaved") {
		t.Fatal("a loaded career with no changes is reported unsaved")
	}
}

// Playing without edits leaves the AI's selection: no lineup is submitted.
func TestUneditedMatchUsesTheAISelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	play(t, []string{"-seed", "42", "-club", "3"}, "c", "lineup", "c", "save "+path, "q")
	w, err := storage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.SubmittedLineup(3); ok {
		t.Fatal("viewing the lineup submitted it")
	}
}

func TestMistakesAreReportedAndChangeNothing(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"lineup", "swap 1 2", "dance", "continue",
		"swap 43 999", "swap 46 51", "swap 43", "role 44 gk", "role 46 df", "role 49 xx", "mentality reckless", "inbox 0", "lineup", "q", "q")
	contains(t, out,
		"! no match is waiting; type continue to go to your next matchday",
		`! unknown command "dance"; type help`,
		"! player 999 is not in your squad",
		"! neither player is in the lineup",
		"! usage: swap A B (player IDs)",
		"! selection: invalid lineup: 2 starting goalkeepers, want 1",
		"! player 46 is not in the starting XI",
		"! role must be GK, DF, MF or FW",
		"! mentality must be defensive, balanced or attacking",
		"! usage: inbox [N]",
		": AI suggestion\n")
}

// season plays the rest of the season; continue then crosses into the next.
func TestSeasonAndNextSeason(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "season", "continue", "status", "q", "q")
	contains(t, out, "Founders League season 1 (14/14 rounds)", "Season finished. Type continue for the next season.",
		"Founders League season 1 ended: champion ", "; you finished ",
		"Founders League season 2 scheduled: first kickoff Sat 2026-08-08 15:00 UTC",
		"MATCHDAY Sat 2026-08-08 15:00 UTC: round 1 v ", "Founders League season 2, 0 of 14 rounds played")
	if n := strings.Count(out, "\n  R"); n != 14 {
		t.Fatalf("season printed %d results, want 14", n)
	}
}

func TestEndOfInputAndBadArguments(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "c")
	contains(t, out, "End of input: unsaved progress was discarded.")

	var buf bytes.Buffer
	noSeed := func() (uint64, error) { return 0, errors.New("unused") }
	for _, args := range [][]string{
		{"-load", "x.json", "-club", "3"},
		{"-load", filepath.Join(t.TempDir(), "missing.json")},
		{"-seed", "42", "-club", "99"},
		{"extra"},
	} {
		if err := run(args, strings.NewReader(""), &buf, &buf, noSeed); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	// A save without a managed club cannot be played.
	path := filepath.Join(t.TempDir(), "plain.json")
	w := mustPlainWorld(t)
	if err := storage.Save(path, w); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-load", path}, strings.NewReader(""), &buf, &buf, noSeed); err == nil || !strings.Contains(err.Error(), "no managed club") {
		t.Fatalf("plain save: err = %v", err)
	}
}

func mustPlainWorld(t *testing.T) *app.World {
	t.Helper()
	w, err := app.NewWorld(app.DefaultConfig(42))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Watching live: half time stops for decisions, substitutions and mentality
// changes show as events, and a save at half time resumes identically.
func TestWatchLiveWithHalfTimeChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.json")
	args := []string{"-seed", "42", "-club", "3"}
	straight := play(t, args, "c", "watch", "sub 57 58", "mentality attacking", "watch 70", "watch", "continue", "q", "q")
	contains(t, straight, "KICKOFF  Quillford FC v Brackenmoor Town", "HALF TIME\nMake changes",
		"45'  SUB   QUI  Wes Lindqvist on for Quentin Tanaka", "45'  TACT  QUI  now attacking",
		"\n70'  Quillford FC", "FULL TIME\nType continue to confirm", "\nFULL TIME  Quillford FC", "Other results:")

	first := play(t, args, "c", "watch", "sub 57 58", "save "+path, "q")
	contains(t, first, "Saved to "+path)
	resumed := play(t, []string{"-load", path}, "status", "lineup", "mentality attacking", "watch 70", "watch", "continue", "q", "q")
	contains(t, resumed, "LIVE 45'  Quillford FC", "2 substitutions left", "  58  Wes Lindqvist ",
		"on the pitch", "Taken off: 57 Quentin Tanaka (45')")
	cut := func(out string) string { return out[strings.Index(out, "\n70'  Quillford"):] }
	if cut(resumed) != cut(straight) {
		t.Fatalf("the match differs after resuming at half time:\n%s\n---\n%s", cut(resumed), cut(straight))
	}
	w, err := storage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := w.LiveMatch(); !ok || l.Position.Minute != 45 || l.View.SubstitutionsUsed[l.Side.Index()] != 1 {
		t.Fatalf("saved live match %+v, %v", l.Position, ok)
	}
}

func TestLiveMistakes(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"watch", "c", "sub 57 58", "watch", "swap 57 58", "watch 30", "sub 58 57", "sub 41 58", "watch 90", "watch", "sub 57 58", "q", "q")
	contains(t, out,
		"! no match is waiting; type continue to go to your next matchday",
		"! your match has not kicked off; type watch to play it live",
		"! the match has kicked off: use sub OUT IN or mentality M",
		"! app: invalid command: play to minute 30 from 45",
		"! app: invalid match decision: ",
		"! full time: type continue to finish the round",
	)
}

func TestMoneyViews(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "squad", "finances", "season", "finances 3", "finances x", "q", "q")
	contains(t, out, "Balance 2,000,000.00 | weekly wages ", "WAGE/WEEK CONTRACT", "Contracts end on 1 July of the year shown.",
		"| 1 ledger entries", "opening balance", "gate receipts F54", "wages ", "! usage: finances [N]")
	if strings.Count(out, "  to 20") != 20 {
		t.Fatal("squad does not show 20 contract ends")
	}
}
