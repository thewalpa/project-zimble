package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/storage"
	"github.com/thewalpa/project-zimble/internal/transfers"
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
	script := []string{"continue", "lineup available", "swap 57 58", "mentality attacking", "role 44 mf", "continue", "save " + path, "quit"}
	out := play(t, []string{"-seed", "42", "-club", "3"}, script...)
	again := play(t, []string{"-seed", "42", "-club", "3"}, script...)
	if out != again {
		t.Fatal("the same session printed different output")
	}
	contains(t, out, "MATCHDAY Sat 2025-08-09 15:00 UTC: round 1 v Brackenmoor Town (home).",
		"Squad availability (20 selectable; use lineup available to hide unavailable players):",
		"your changes (used when you continue)", "Mentality: attacking", "(out of position)",
		"FULL TIME  Quillford FC", "45'  Half time", "90'  Full time", "Other results:", "New in your inbox:", "Saved to "+path)
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

func TestTeamPlanCanBeEditedBetweenMatchesAndIsUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"lineup", "swap 57 58", "mentality attacking", "save "+path, "quit")
	contains(t, out, "Team plan: starting point; each edit saves automatically", "Team plan saved.", "Team plan: saved team plan")
	w, err := storage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := w.TeamPlan()
	if err != nil || !plan.Saved || plan.Lineup.Tactics.Mentality != matches.Attacking {
		t.Fatalf("team plan %+v, %v", plan, err)
	}
	if plan.Lineup.Starters[9].Player != 58 && plan.Lineup.Starters[10].Player != 58 {
		t.Fatalf("starter 58 was not saved: %+v", plan.Lineup.Starters)
	}
	ready := play(t, []string{"-load", path}, "continue", "lineup", "quit", "quit")
	contains(t, ready, "Lineup: your saved team plan", "Mentality: attacking")
}

func TestMistakesAreReportedAndChangeNothing(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"lineup", "swap 1 2", "dance", "continue",
		"swap 43 999", "swap 46 51", "swap 43", "role 44 gk", "role 46 df", "role 49 xx", "mentality reckless", "inbox 0", "lineup", "q", "q")
	contains(t, out,
		"! player 1 is not in your squad",
		`! unknown command "dance"; type help`,
		"! player 999 is not in your squad",
		"! neither player is in the lineup",
		"! usage: swap A B (player IDs)",
		"! selection: invalid lineup: 2 starting goalkeepers, want 1",
		"! player 46 is not in the starting XI",
		"! role must be GK, DF, MF or FW",
		"! mentality must be defensive, balanced or attacking",
		"! usage: inbox [N]",
		": the assistant's suggestion\n")
}

// season plays the rest of the season; continue then crosses into the next,
// through the cup run (three matches, to the final), stopping before the
// contract-year end and at the transfer window.
func TestSeasonAndNextSeason(t *testing.T) {
	script := []string{"season"}
	for range 9 {
		script = append(script, "continue")
	}
	out := play(t, []string{"-seed", "42", "-club", "3"}, append(script, "status", "q", "q")...)
	contains(t, out, "Founders League season 1 (14/14 rounds)", "Season finished. Type continue for the next season.",
		"Tue 2026-06-30 00:00 UTC: 7 of your players' contracts end tomorrow.",
		"Wed 2026-07-01 00:00 UTC: The transfer window is open until Wed 2026-07-29 00:00 UTC; clubs answer bids made before Tue 2026-07-28 00:00 UTC.",
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
		"45'  SUB   QUI  Rhys Aldridge on for Pieter Haugen", "45'  TACT  QUI  now attacking",
		"\n70'  Quillford FC", "FULL TIME\nType continue to confirm", "\nFULL TIME  Quillford FC", "Other results:")

	first := play(t, args, "c", "watch", "sub 57 58", "save "+path, "q")
	contains(t, first, "Saved to "+path)
	resumed := play(t, []string{"-load", path}, "status", "lineup", "mentality attacking", "watch 70", "watch", "continue", "q", "q")
	contains(t, resumed, "LIVE 45'  Quillford FC", "2 substitutions left", "  58  Rhys Aldridge ",
		"on the pitch", "Taken off: 57 Pieter Haugen (45')")
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

// Before the contract year (after the player year): renew one player,
// reject bad offers; after it: the others have left, the inbox says so, and
// a free agent can be signed while the window is open (the AI clubs take
// most of the pool at the first runs; once a matchday is waiting, squads
// are locked).
func TestContracts(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"season", "continue", "continue", "continue", "continue", "continue", "continue", "continue", // the cup run, to the final
		"contracts", "renew 56", "renew 45 9", "renew 48 2 1", "renew 44", "renew 999", "sign 45",
		"continue", "free", "sign 44", "sign 168 1", "squad", "inbox 12", "continue", "bid 300", "q", "q")
	contains(t, out,
		"Contracts: 7 end on Wed 2026-07-01 00:00 UTC unless renewed (type contracts).",
		"  56  MF  Kieran Walsh              27    76     2,660.00     2026     2,570.00  <- final year",
		"Kieran Walsh signed a new contract until 1 July 2028 at 2,570.00 a week.",
		"! app: contract offer rejected: 9 years, allowed 1..4",
		"! app: contract offer rejected: weekly wage 1.00, player accepts 2,120.00..4,240.00",
		"! app: the contract is not in its final year: player 44",
		"! player 999 is not in your squad",
		"! player 45 is not a free agent; type free for the list",
		"development: 8 of your players improved and 11 declined over the year (type squad)",
		"contract: Kieran Walsh renewed until 1 July 2028 at 2,570.00 a week",
		"contract: Aaron Morrow left the club as a free agent",
		"signing: Rasmus Brandt joined until 1 July 2029 at 1,340.00 a week",
		" 168  DF  Lars Kessler             Eastmarch   23    46 100%               940.00",
		"Free agents retire on the eve of 1 July once they are 31.",
		"! player 44 is not a free agent; type free for the list",
		"! app: squads cannot change while rounds await results",
		"  56  MF  Kieran Walsh             Westmark    27    76 100%             2,570.00  to 2028",
		"Lars Kessler joined until 1 July 2027 at 940.00 a week.",
	)
	if strings.Count(out, "Lars Kessler joined until 1 July 2027 at 940.00 a week.") != 1 {
		t.Fatal("signed a player on a matchday, or twice")
	}
}

// The yearly player step reaches the manager: a development summary, a
// retirement and the youth player who replaces him.
func TestPlayerYearMessages(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "season", "continue", "continue", "season", "continue", "season", "continue", "squad", "q", "q")
	contains(t, out,
		"Wed 2027-06-30 00:00 UTC  development: 4 of your players improved and 8 declined over the year (type squad)",
		"Wed 2027-06-30 00:00 UTC  retirement: Liam Aldridge retired at 36",
		"Wed 2027-06-30 00:00 UTC  youth: Ben Nolan joined from the youth ranks until 1 July 2030 at 510.00 a week",
		"Every year on the eve of that date, young players improve, older ones decline, and some retire.",
	)
	if strings.Contains(out[strings.LastIndex(out, "AGE"):], "Liam Aldridge") {
		t.Fatal("the retired goalkeeper is still in the squad")
	}
}

// The cup is drawn when the leagues end and shown by cup; its results reach
// the inbox, penalties included.
func TestCup(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "cup", "season",
		"continue", "continue", "continue", "continue", "continue", "continue", "continue", // the manager's cup run, and on
		"cup", "q", "q")
	contains(t, out,
		"No cup has been drawn yet",
		"Continental Cup 1 drawn: first kickoff Sat 2025-11-22 15:00 UTC (type cup)",
		"Continental Cup 1 won by Quillford FC: your club won it!",
		"Quarter-finals, Sat 2025-11-22 15:00 UTC",
		"  Brackenmoor Town         1-1 Ironbridge Wanderers (2-4 on penalties)",
		"Final, Sat 2025-12-06 15:00 UTC",
		"Winner: Quillford FC",
	)
}

// A managed club in the cup: its matchday is named after the round, the
// result can be decided on penalties, and the inbox says how far it went.
func TestManagedCupRun(t *testing.T) {
	script := []string{"season", "continue", "continue", "continue", "continue", "continue", "continue", "continue", "inbox 20", "q", "q"}
	// Brackenmoor Town (club 4) goes out on penalties in the quarter-final.
	out := play(t, []string{"-seed", "42", "-club", "4"}, script...)
	contains(t, out,
		"MATCHDAY Sat 2025-11-22 15:00 UTC: Continental Cup quarter-final v Ironbridge Wanderers (home).",
		"matchday: Continental Cup quarter-final v Ironbridge Wanderers (home)",
		"FULL TIME  Brackenmoor Town 1-1 Ironbridge Wanderers (2-4 on penalties)  (L)",
		"result: 1-1 (2-4 on penalties) v Ironbridge Wanderers (home)",
		"Continental Cup 1 won by Saltmere Athletic; you went out in the quarter-final",
	)
	// Hollowick Athletic (club 6 of seed 1) wins it.
	out = play(t, []string{"-seed", "1", "-club", "6"}, script...)
	contains(t, out,
		"matchday: Continental Cup final v Osterholm Athletic (home)",
		"FULL TIME  Hollowick Athletic 4-0 Osterholm Athletic  (W)",
		"Continental Cup 1 won by Hollowick Athletic: your club won it!",
	)
}

func TestRefusedAcceptedOfferWording(t *testing.T) {
	got := refusedOfferText(app.OfferView{PlayerName: "Elias Gallo", BuyerName: "Eldhaven United"})
	contains(t, got, "Accepted, but Elias Gallo refused to join Eldhaven United.")
}

func TestRefusedOfferInboxWording(t *testing.T) {
	if got := outcomeName(uint8(transfers.StatusRefused)); !strings.Contains(got, "the player refused to join") {
		t.Fatalf("outcomeName(StatusRefused) = %q", got)
	}
}

// In the transfer window: the manager bids (at and below the asking
// price), the answers arrive at the next run, the manager lists a player,
// an AI club bids for him, and the manager answers it before and after
// saving.
func TestTransfers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career.json")
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"season", "continue", "continue", "continue", "continue", "continue", "continue", "continue", "continue", // the cup run
		"market fw", "market", "bid 344", "bid 547", "bid 617", "bid 238 500000", "bid 238",
		"bid 44", "list 44", "continue", "list 44", "list", "continue", "transfers", "status", "free", "save "+path, "accept 99", "q", "q")
	contains(t, out, "9 free agents wait for a club (type free).",
		"Until Wed 2026-07-15 00:00 UTC, only you may sign free agents; AI clubs can sign them from then.",
		"Wed 2026-07-01 00:00 UTC: The transfer window is open until Wed 2026-07-29 00:00 UTC",
		"  37  GRY Westmark    Ben Mercer               Westmark     32    74     2027     410,000.00  (won't join a weaker club)",
		"! usage: market GK|DF|MF|FW",
		"You bid 400,000.00 for Rhys Underwood (offer 55), offering 1 year at 2,000.00 a week. The club answers on Thu 2026-07-02 00:00 UTC.",
		"! app: your club has already bid for the player in this window: player 238",
		"! player 44 is not at another club; type market POS for the list",
		"! app: the squad would fall below its minimum at that position: 5 DF, minimum 5", // before the bought defenders arrive
		"Callum Doyle is on the transfer list at 1,200,000.00 until the window closes; clubs that need a defender may bid.",
		"  44  Quillford FC           Callum Doyle             Westmark   DF   18    75   1,200,000.00",
		"transfer: Rhys Underwood joined from Northwick Albion for 400,000.00, until 1 July 2027 at 2,000.00 a week",
		"transfer: your bid of 500,000.00 for Leif Dekker of Ironbridge Wanderers was rejected",
		"bid: Eldhaven United bid 1,200,000.00 for Callum Doyle (offer 84); answer before Mon 2026-07-06 00:00 UTC (accept/reject)",
		"Bids for your players (accept OFFER or reject OFFER):",
		"Your players on the transfer list (unlist ID takes one off):",
		"* Thu 2026-07-02 00:00 UTC   Rhys Underwood           Northwick Albion       -> Quillford FC",
		"1 bids for your players await your answer. (type transfers)",
		"! app: no open offer for one of your players has that ID: offer 99")

	accepted := play(t, []string{"-load", path}, "accept 84", "finances 2", "list", "q", "q")
	contains(t, accepted, "Accepted: the transfer is complete.",
		"transfer: Callum Doyle left for Eldhaven United for 1,200,000.00", "transfer fee O84")
	if strings.Contains(accepted, "  44  Quillford FC") {
		t.Fatal("a sold player is still listed")
	}
	rejected := play(t, []string{"-load", path}, "reject 84", "unlist 44", "unlist 44", "continue", "q", "q")
	contains(t, rejected, "Rejected.", "Callum Doyle is off the transfer list.", "! app: the player is not on the transfer list: player 44",
		"transfer: the bid of 1,200,000.00 from Eldhaven United for Callum Doyle was rejected")
	if strings.Contains(rejected, "left for Dunmarrow") {
		t.Fatal("a rejected bid moved the player")
	}
}

func TestSortableLists(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"squad ovr desc",
		"squad name asc",
		"squad bogus",
		"table club",
		"table gd",
		"table bogus",
		"fixtures opp",
		"contracts wage",
		"contracts bogus",
		"finances what",
		"finances 5 amount desc",
		"market fw price desc",
		"market fw name asc",
		"market fw bogus",
		"inbox 5 oldest",
		"cup home",
		"cup bogus",
		"q", "q",
	)
	contains(t, out,
		"POS", "NAME", "OVR",
		"ABB", "CLUB",
		"WAGE/WEEK",
		"ASKING PRICE",
		`! unknown sort "bogus": choose pos, name, age, ovr, cond, wage, contract or id`,
		`! unknown sort "bogus": choose pts, gd, gf, ga, w, d, l, played, club or rank`,
		`! unknown sort "bogus": choose ends, wage, ovr, age, name, pos or asks`,
		`! unknown sort "bogus": choose ovr, price, id, club, name, age or ends`,
		`! unknown sort "bogus": choose home, away, score or id`,
	)
}

// Releasing a player shows the payoff first and needs yes; the payoff
// reaches the ledger and the inbox. Releases are refused at the minimum
// and on a matchday, and the squad may grow beyond the roster count.
func TestRelease(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"release 60", "release 60 yes", "release 43 yes", "release 42 yes", "release 999", "release 60 no",
		"finances 2", "inbox 2", "squad", "bid 159", "continue", "continue", "release 41 yes", "q", "q")
	contains(t, out,
		"Releasing Rhys McAllister costs 179,400.00: his wages until his contract ends on 1 July 2028. He becomes a free agent.",
		"Type release 60 yes to release him.",
		"Rhys McAllister was released and is now a free agent. You paid 179,400.00.",
		"Callum McAllister was released and is now a free agent. You paid 113,360.00.",
		"! app: the squad would fall below its minimum at that position: 2 GK, minimum 2",
		"! player 999 is not in your squad",
		"! usage: release ID [yes]",
		"contract payoff P60    -179,400.00",
		"release: Callum McAllister left the club as a free agent; you paid 113,360.00",
		"You have 18 players; a squad holds at most 25, and at least 2 GK, 5 DF, 5 MF, 3 FW.",
		"You bid 860,000.00 for Jamie Draper (offer 1)",
		"transfer: Jamie Draper joined from Dunmarrow Albion",
		"! app: squads cannot change while rounds await results",
	)
	if strings.Count(out, "was released") != 2 {
		t.Fatal("a refused release was reported as done")
	}
}

func TestLineupCarriesOverToNextMatchday(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"},
		"continue", // to R1 matchday
		"swap 57 58", "mentality attacking",
		"continue",       // play R1
		"release 44 yes", // release starter 44 between rounds
		"continue",       // to R2 matchday
		"lineup",
		"continue", // play R2 without submitting
		"quit",
	)
	contains(t, out,
		"carried over from the last match (vs Brackenmoor Town)",
		"Callum Doyle has left the club;",
		"takes his place",
		"Mentality: attacking",
		"Lineup: your lineup",
	)
}

func TestPlayerProfileCommand(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "player 56", "player 1", "player 99999", "player", "quit", "quit")
	contains(t, out, "Kieran Walsh (player 56)", "Contract:", "GK DEF PAS FIN PAC STA", "Club:", "Career:", "before ", "current club",
		"no player 99999", "usage: player ID")
}

func TestComparePlayersCommand(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "compare 56 1", `compare "Kieran Walsh" 1`, "compare 56 99999", "compare 56", "quit", "quit")
	contains(t, out, "Kieran Walsh (#56)", "Weekly wage", "Asking price", "Wage demand", "DEF", "no player 99999", "usage: compare PLAYER PLAYER")
	if strings.Count(out, "Kieran Walsh (#56)") < 2 {
		t.Fatalf("compare should resolve a managed squad player by name:\n%s", out)
	}
}

// tables lists every league with a heading and marks the manager's club.
func TestTablesShowsEveryLeague(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "tables", "tables extra", "quit", "quit")
	contains(t, out,
		"Founders League season 1 (0/14 rounds)",
		"Harbour League season 1 (0/14 rounds)",
		"Founders Second Division season 1 (0/14 rounds)",
		"Harbour Second Division season 1 (0/14 rounds)",
		"top 4 qualify for Continental Cup",
		"usage: tables",
	)
	if strings.Count(out, "POS  ABB  CLUB") != 4 {
		t.Fatalf("tables should print four league headers:\n%s", out)
	}
	if !strings.Contains(out, "* QUI") {
		t.Fatalf("managed club was not marked in its table:\n%s", out)
	}
	if strings.Count(out, "top 4 qualify for Continental Cup") != 2 {
		t.Fatalf("cup places should be shown only for the qualifying first divisions:\n%s", out)
	}
}

// history lists every season's champion and shows a past season's final
// table or bracket.
func TestHistory(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "history", "season", "continue", "continue", "continue", "continue", "continue", "continue", "continue", "history", "history 1 1", "history 3 1", "history 99 1", "history x", "q", "q")
	contains(t, out,
		"Founders League", "in progress",
		"Quillford FC *",                         // the manager's club is marked where it is champion
		"Continental Cup   ", "Brackenmoor Town", // the league champion
		"Founders League season 1 (14/14 rounds)",
		"Winner: Quillford FC",
		"no season 1 of competition 99",
		"usage: history [COMPETITION SEASON]",
	)
}

// The inbox marks unread messages, listing it acknowledges nothing, read
// does, and the state survives a save.
func TestInboxReadState(t *testing.T) {
	dir := t.TempDir()
	start, path := filepath.Join(dir, "start.json"), filepath.Join(dir, "career.json")
	play(t, []string{"-seed", "42", "-club", "3"}, "continue", "save "+start, "quit")
	w, err := storage.Load(start)
	if err != nil {
		t.Fatal(err)
	}
	// Time passes outside the client, as when a web session saved the career.
	for i := 0; w.UnreadInboxCount() == 0; i++ {
		if i > 20 {
			t.Fatal("no message ever arrived")
		}
		if ready, ok := w.Pending(); ok {
			cmd := app.ResolveRounds{ID: w.NextCommandID(), ExpectedRevision: w.Revision()}
			for _, r := range ready.Rounds {
				cmd.Rounds = append(cmd.Rounds, r.Round)
			}
			if _, err := w.ResolveRounds(cmd); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := w.Continue(w.Now() + 7*24*60); err != nil {
			t.Fatal(err)
		}
	}
	n := w.UnreadInboxCount()
	if err := storage.Save(path, w); err != nil {
		t.Fatal(err)
	}

	out := play(t, []string{"-load", path}, "inbox 3", "inbox 3", "read", "inbox 3", "save", "quit")
	contains(t, out, fmt.Sprintf("%d unread; * marks unread", n), fmt.Sprintf("Marked %d message(s) read.", n), "0 unread; * marks unread")
	if strings.Count(out, "\n* ") == 0 {
		t.Fatalf("no unread marker:\n%s", out)
	}
	w, err = storage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.UnreadInboxCount(); got != 0 {
		t.Fatalf("%d unread after read and save", got)
	}
}

// The club chooser lists the clubs by league with their nations; the squad,
// lineup and player pages show nationalities and all eleven attributes.
func TestNationalitiesAndAttributes(t *testing.T) {
	out := play(t, []string{"-seed", "42"}, "3", "squad", "continue", "lineup", "player 56", "quit", "quit")
	contains(t, out, "Founders League\n  ID  ABB  CLUB", "Harbour Second Division\n", "NATION", "Westmark",
		"POS NAME                     NATION", "GK DEF PAS FIN PAC STA DRI HEA STR ACC PSN",
		"Kieran Walsh (player 56), MF, age 26, Westmark", "dribbling, heading, strength, acceleration, positioning")
	first, second := strings.Index(out, "Founders League\n"), strings.Index(out, "Harbour Second Division\n")
	if first < 0 || second < first {
		t.Fatal("the leagues are not listed in order")
	}
}

// Tables mark the promotion and relegation places; the history's name column
// fits the longest competition name.
func TestPromotionMarksAndHistory(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "3"}, "table", "season", "history", "quit", "quit")
	contains(t, out, "down: the bottom 2 are relegated to the division below.")
	if strings.Count(out, "  down\n") < 2 {
		t.Fatalf("want two relegation places marked:\n%s", out)
	}
	contains(t, out, "down: the bottom 2 were relegated to the division below.")
	contains(t, out, fmt.Sprintf("COMP %-24s SEASON  CHAMPION", "NAME"), "Harbour Second Division")
}

// Matches hurt players: the inbox and squad say who is out, and the client
// prints the recovery days.
func TestInjuriesInTheTerminal(t *testing.T) {
	script := []string{"season"}
	for range 12 {
		script = append(script, "continue")
	}
	script = append(script, "squad", "inbox 40", "quit", "quit")
	out := play(t, []string{"-seed", "42", "-club", "3"}, script...)
	contains(t, out, "injury: ", " is out for ", "out ")
	if !strings.Contains(out, "d  ") && !strings.Contains(out, "d\n") {
		t.Fatalf("no recovery days shown:\n%s", out)
	}
}

// The season-end message says when the club goes up or down a division.
func TestSeasonEndSaysRelegation(t *testing.T) {
	out := play(t, []string{"-seed", "42", "-club", "8"}, "season", "continue", "continue", "inbox 20", "quit", "quit")
	contains(t, out, "you finished 8th: relegated to the division below")
}
