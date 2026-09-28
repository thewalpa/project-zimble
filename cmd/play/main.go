// Command play is an interactive terminal career. You manage one club: look
// at your squad, the table and your inbox, pick your lineup and mentality
// for each match, and continue through the season. Everything goes through
// the same commands and queries as any other client; the lineup you edit is
// a local draft until the match is played.
//
//	go run ./cmd/play                 # new career: random seed, choose a club
//	go run ./cmd/play -seed 42 -club 3
//	go run ./cmd/play -load career.json
//
// Type help at the prompt for the commands.
package main

import (
	"bufio"
	"cmp"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
	"github.com/thewalpa/project-zimble/internal/storage"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cryptoSeed); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "play:", err)
		}
		os.Exit(2)
	}
}

func cryptoSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("draw random seed: %w", err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// run starts or loads a career, then reads commands from in until quit or
// end of input.
func run(args []string, in io.Reader, out, errOut io.Writer, newSeed func() (uint64, error)) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.SetOutput(errOut)
	seed := fs.Uint64("seed", 0, "world seed for a new career (default: random)")
	club := fs.Uint64("club", 0, "club `ID` to manage in a new career (default: choose at the prompt)")
	load := fs.String("load", "", "continue the career saved in `FILE`")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["load"] && (set["seed"] || set["club"]) {
		return errors.New("-seed and -club cannot be used with -load: a saved career keeps its own")
	}

	s := &session{in: bufio.NewScanner(in), out: out, savePath: "career.json"}
	if set["load"] {
		w, err := storage.Load(*load)
		if err != nil {
			return fmt.Errorf("load %s: %w", *load, err)
		}
		if _, ok := w.UserClub(); !ok {
			return fmt.Errorf("%s has no managed club; start a new career instead", *load)
		}
		s.w, s.savePath = w, *load
	} else {
		if !set["seed"] {
			drawn, err := newSeed()
			if err != nil {
				return err
			}
			*seed = drawn
		}
		w, err := s.newCareer(random.Seed(*seed), ids.ClubID(*club))
		if err != nil {
			return err
		}
		s.w = w
	}
	s.saved, s.savedRevision = set["load"], s.w.Revision()
	s.markInboxRead()
	s.welcome()
	s.loop()
	return nil
}

// session is the interactive client state: the world, the input, the save
// file, the lineup draft for the pending match and the newest inbox message
// already shown.
type session struct {
	w             *app.World
	in            *bufio.Scanner
	out           io.Writer
	savePath      string
	saved         bool         // the career exists on disk...
	savedRevision app.Revision // ...at this revision
	draft         *draft
	lastSeen      events.ID
	liveShown     int // live match events already printed
	quitWarned    bool
	warnedYearEnd sim.GameInstant // contract-year end already warned about
}

// draft is the lineup being prepared for a pending user fixture. It becomes
// a SubmitLineup command only when the match is played, and only if edited.
type draft struct {
	fixture ids.FixtureID
	lineup  selection.Lineup
	edited  bool
}

func (s *session) printf(format string, args ...any) { fmt.Fprintf(s.out, format, args...) }

// prompt prints a prompt and reads one line; ok is false at end of input.
func (s *session) prompt(p string) (string, bool) {
	s.printf("%s", p)
	if !s.in.Scan() {
		s.printf("\n")
		return "", false
	}
	return strings.TrimSpace(s.in.Text()), true
}

func (s *session) unsaved() bool { return !s.saved || s.w.Revision() != s.savedRevision }

// newCareer creates a world managing club; with club 0 the player chooses
// from the generated clubs.
func (s *session) newCareer(seed random.Seed, club ids.ClubID) (*app.World, error) {
	cfg := app.DefaultConfig(seed)
	if club == 0 {
		preview, err := app.NewWorld(cfg)
		if err != nil {
			return nil, err
		}
		s.printf("New career (seed %d). Choose your club:\n\n", seed)
		s.printf("%4s  %-3s  %-22s %5s\n", "ID", "ABB", "CLUB", "OVR")
		for _, c := range preview.Summary().ClubRows {
			s.printf("%4d  %-3s  %-22s %5d\n", c.ID, c.ShortName, c.Name, c.AverageOverall)
		}
		for club == 0 {
			line, ok := s.prompt("\nclub ID> ")
			if !ok {
				return nil, errors.New("no club chosen")
			}
			n, err := strconv.ParseUint(line, 10, 64)
			if _, known := preview.Squad(ids.ClubID(n)); err != nil || !known {
				s.printf("Please enter one of the club IDs above.\n")
				continue
			}
			club = ids.ClubID(n)
		}
	}
	cfg.UserClub = club
	w, err := app.NewWorld(cfg)
	if err == nil {
		s.printf("New career, seed %d (start again with -seed %d -club %d).\n", seed, seed, club)
	}
	return w, err
}

func (s *session) welcome() {
	label := s.clubLabel()
	s.printf("\nYou manage %s (%s). Type help for commands.\n", label.ClubName, label.ShortName)
	s.status()
}

func (s *session) loop() {
	for {
		line, ok := s.prompt("\n> ")
		if !ok {
			if s.unsaved() {
				s.printf("End of input: unsaved progress was discarded.\n")
			}
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cmd, args := strings.ToLower(fields[0]), fields[1:]
		if cmd != "quit" && cmd != "exit" && cmd != "q" {
			s.quitWarned = false
		}
		var err error
		switch cmd {
		case "help", "h", "?":
			s.help()
		case "status", "s":
			s.status()
		case "squad":
			err = s.squad(args)
		case "table", "t":
			err = s.table(args)
		case "cup":
			err = s.cup(args)
		case "fixtures", "f":
			err = s.fixtures(args)
		case "finances", "money":
			err = s.finances(args)
		case "contracts":
			err = s.contracts(args)
		case "renew":
			err = s.renew(args)
		case "free", "agents":
			err = s.freeAgents(args)
		case "sign":
			err = s.sign(args)
		case "release":
			err = s.release(args)
		case "transfers":
			err = s.transfers(args)
		case "market":
			err = s.market(args)
		case "list":
			err = s.list(args)
		case "unlist":
			err = s.unlist(args)
		case "bid":
			err = s.bid(args)
		case "accept", "reject":
			err = s.answer(args, cmd == "accept")
		case "inbox", "i":
			err = s.inbox(args)
		case "lineup", "l":
			if _, live := s.w.LiveMatch(); live {
				err = s.showLive()
			} else {
				err = s.showLineup()
			}
		case "swap", "role", "reset":
			if _, live := s.w.LiveMatch(); live {
				err = errors.New("the match has kicked off: use sub OUT IN or mentality M")
			} else if cmd == "swap" {
				err = s.swap(args)
			} else if cmd == "role" {
				err = s.role(args)
			} else {
				err = s.reset()
			}
		case "mentality", "m":
			if _, live := s.w.LiveMatch(); live {
				err = s.liveMentality(args)
			} else {
				err = s.mentality(args)
			}
		case "watch", "w":
			err = s.watch(args)
		case "sub":
			err = s.sub(args)
		case "continue", "c":
			err = s.next()
		case "season":
			err = s.season()
		case "save":
			err = s.save(args)
		case "quit", "exit", "q":
			if s.unsaved() && !s.quitWarned {
				s.quitWarned = true
				s.printf("You have unsaved progress. Type save, or quit again to leave without saving.\n")
				continue
			}
			s.printf("Goodbye.\n")
			return
		default:
			err = fmt.Errorf("unknown command %q; type help", cmd)
		}
		if err != nil {
			s.printf("! %v\n", err)
		}
	}
}

func (s *session) help() {
	s.printf(`Commands:
  status (s)            date, season progress and your next match
  squad                 your players: ID, position, rating, condition
  table (t)             the league table
  cup                   the Continental Cup: this edition's bracket and results
  fixtures (f)          your club's fixtures and results this season
  inbox (i) [N]         the latest N inbox messages (default 10)
  finances [N]          your balance, weekly wage bill and latest N ledger entries
  contracts             your players' contracts, soonest end first, and what they ask for
  renew ID [YEARS [WAGE]]  offer a new contract to a player in the final year (default: usual terms)
  free                  free agents: players without a club
  sign ID [YEARS [WAGE]]   sign a free agent (default: usual terms; not on a matchday)
  release ID [yes]      release one of your players, paying the rest of his contract (not on a matchday)
  transfers             the transfer window, bids for your players, your bids, this window's transfers
  market GK|DF|MF|FW    other clubs' players at a position, best first, with asking prices
  list                  the transfer list: players clubs have put up for sale, with asking prices
  list ID [PRICE]       put one of your players on the transfer list (default: his value) until the window closes
  unlist ID             take one of your players off the transfer list
  bid ID [FEE [YEARS [WAGE]]]  bid for another club's player (default: the asking price and usual terms)
  accept OFFER, reject OFFER   answer a bid for one of your players
  lineup (l)            your lineup for the match waiting to be played
  swap A B              swap two players (IDs) between XI, bench and squad
  role P GK|DF|MF|FW    play starter P in another role
  mentality (m) M       defensive, balanced or attacking (during your match: a live change)
  reset                 go back to the AI's suggested lineup
  watch (w) [MIN]       play your match live to MIN (default: half time, then full time)
  sub OUT IN            during your match: substitute player OUT with IN (IDs)
  continue (c)          play the waiting match, or go to your next matchday (while the
                        transfer window is open: to the next transfer news)
  season                play the rest of the season (edited lineup used once)
  save [FILE]           save the career (default: %s)
  quit (q)              leave
`, s.savePath)
}

// --- views ---------------------------------------------------------------

func (s *session) club() ids.ClubID {
	c, _ := s.w.UserClub()
	return c
}

func (s *session) clubLabel() app.TeamLabel {
	for _, sc := range s.w.Schedules() {
		for _, r := range sc.Rounds {
			for _, f := range r.Fixtures {
				if f.Home.Club == s.club() {
					return f.Home
				}
				if f.Away.Club == s.club() {
					return f.Away
				}
			}
		}
	}
	return app.TeamLabel{Club: s.club()}
}

// userSchedule returns the current season schedule of the league the club
// plays in.
func (s *session) userSchedule() (app.Schedule, bool) {
	for _, sc := range s.w.Schedules() {
		for _, r := range sc.Rounds {
			for _, f := range r.Fixtures {
				if f.Home.Club == s.club() || f.Away.Club == s.club() {
					return sc, true
				}
			}
		}
	}
	return app.Schedule{}, false
}

// fixtureInfo finds a fixture of any competition.
func (s *session) fixtureInfo(id ids.FixtureID) app.FixtureInfo {
	info, _ := s.w.FixtureInfo(id)
	return info
}

// matchName names a fixture's round: "round 3" in the league, "Continental
// Cup quarter-final" in a cup.
func matchName(info app.FixtureInfo) string {
	if info.Cup {
		return info.CompetitionName + " " + info.RoundName
	}
	return info.RoundName
}

// nextFixture is the club's next scheduled fixture in its league or a cup.
func (s *session) nextFixture() (app.FixtureInfo, bool) {
	var best app.FixtureInfo
	found := false
	consider := func(id ids.FixtureID, home, away ids.ClubID, played bool) {
		if played || (home != s.club() && away != s.club()) {
			return
		}
		if info := s.fixtureInfo(id); !found || info.Kickoff < best.Kickoff {
			best, found = info, true
		}
	}
	if sc, ok := s.userSchedule(); ok {
		for _, r := range sc.Rounds {
			for _, f := range r.Fixtures {
				consider(f.ID, f.Home.Club, f.Away.Club, f.Played)
			}
		}
	}
	for _, c := range s.w.Cups() {
		for _, r := range c.Rounds {
			for _, f := range r.Ties {
				consider(f.ID, f.Home.Club, f.Away.Club, f.Played)
			}
		}
	}
	return best, found
}

// describe says who the club plays in a fixture and where.
func (s *session) describe(f app.FixtureLine) string {
	if f.Home.Club == s.club() {
		return fmt.Sprintf("%s (home)", f.Away.ClubName)
	}
	return fmt.Sprintf("%s (away)", f.Home.ClubName)
}

func (s *session) status() {
	cal := s.w.Calendar()
	sc, ok := s.userSchedule()
	s.printf("\n%s", cal.Format(s.w.Now()))
	if !ok {
		s.printf("\n")
		return
	}
	played := 0
	for _, r := range sc.Rounds {
		if r.Status == competitions.RoundCompleted {
			played++
		}
	}
	s.printf(" | %s season %d, %d of %d rounds played\n", sc.CompetitionName, sc.Season, played, len(sc.Rounds))
	if fin, ok := s.w.Finances(s.club()); ok {
		s.printf("Balance %s | weekly wages %s\n", fin.Balance, fin.WeeklyWage)
	}
	if n := len(s.expiring()); n > 0 {
		s.printf("Contracts: %d end on %s unless renewed (type contracts).\n", n, cal.Format(s.w.ContractYearEnd()))
	}
	if s.w.TransferWindow().Open {
		s.printf("%s", s.windowLine())
		if n := len(s.bidsReceived()); n > 0 {
			s.printf(" %d bids for your players await your answer.", n)
		}
		s.printf(" (type transfers)\n")
	}
	if l, live := s.w.LiveMatch(); live {
		s.printf("LIVE %d'  %s %d-%d %s. watch to play on, sub/mentality to change, continue to finish.\n",
			l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
		return
	}
	if fixture, ok := s.pendingFixture(); ok {
		info := s.fixtureInfo(fixture)
		s.printf("MATCHDAY: %s v %s is waiting. Check lineup, then watch it live or continue to play.\n", matchName(info), s.describe(info.FixtureLine))
		return
	}
	if info, ok := s.nextFixture(); ok {
		s.printf("Next: %s v %s, %s. Type continue to go there.\n", matchName(info), s.describe(info.FixtureLine), cal.Format(info.Kickoff))
		return
	}
	s.printf("Season finished. Type continue for the next season.\n")
}

func (s *session) squad(args []string) error {
	col := "id"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "ovr", "cond", "wage":
			desc = true
		case "pos", "name", "age", "contract", "ends", "id", "pick":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose pos, name, age, ovr, cond, wage, contract or id", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: squad [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: squad [COLUMN [asc|desc]]")
		}
	}
	players, _ := s.w.Squad(s.club())
	marks := map[ids.PlayerID]string{}
	markOrder := map[ids.PlayerID]int{}
	if d, err := s.currentDraft(); err == nil {
		for i, sl := range d.lineup.Starters {
			marks[sl.Player] = fmt.Sprintf("XI %-2d", i+1)
			markOrder[sl.Player] = i + 1
		}
		for i, p := range d.lineup.Bench {
			marks[p] = "bench"
			markOrder[p] = 100 + i
		}
	}
	slices.SortStableFunc(players, func(a, b app.SquadPlayer) int {
		var diff int
		switch col {
		case "pos":
			diff = cmp.Compare(a.Position, b.Position)
		case "name":
			diff = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "age":
			diff = cmp.Compare(a.Age, b.Age)
		case "ovr":
			diff = cmp.Compare(a.Overall, b.Overall)
		case "cond":
			diff = cmp.Compare(a.Condition, b.Condition)
		case "wage":
			diff = cmp.Compare(a.Contract.WeeklyWage, b.Contract.WeeklyWage)
		case "contract", "ends":
			diff = cmp.Compare(a.Contract.Expires, b.Contract.Expires)
		case "pick":
			oa, ob := markOrder[a.Player], markOrder[b.Player]
			if oa == 0 {
				oa = 999
			}
			if ob == 0 {
				ob = 999
			}
			diff = cmp.Compare(oa, ob)
		default:
			diff = cmp.Compare(a.Player, b.Player)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Player, b.Player))
	})
	cal := s.w.Calendar()
	s.printf("\n%4s  %-3s %-24s %3s %5s %5s %12s %8s  %s\n", "ID", "POS", "NAME", "AGE", "OVR", "COND", "WAGE/WEEK", "CONTRACT", "PICK")
	for _, p := range players {
		ends, _ := cal.Civil(p.Contract.Expires)
		s.printf("%4d  %-3s %-24s %3d %5d %5s %12s %8s  %s\n", p.Player, p.Position, p.Name, p.Age,
			p.Overall, condition(p.Condition), p.Contract.WeeklyWage, fmt.Sprintf("to %d", ends.Year), marks[p.Player])
	}
	s.printf("Ratings are 1-100. COND is fitness (100%% = fully fit). Contracts end on %s of the year shown.\n",
		monthDay(cal.Epoch()))
	s.printf("Every year on the eve of that date, young players improve, older ones decline, and some retire.\n")
	defs := s.w.Content()
	var mins []string
	for _, q := range defs.Roster {
		mins = append(mins, fmt.Sprintf("%d %s", q.Min, q.Position))
	}
	s.printf("You have %d players; a squad holds at most %d, and at least %s.\n", len(players), defs.SquadLimit, strings.Join(mins, ", "))
	s.printf("Lineup shows each player's attributes.\n")
	return nil
}

func condition(c uint8) string { return fmt.Sprintf("%d%%", c) }

func (s *session) table(args []string) error {
	col := "rank"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "pos", "rank":
			desc = false
		case "pts", "points", "w", "won", "d", "drawn", "gf", "gd", "p", "played":
			desc = true
		case "l", "lost", "ga", "club":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose pts, gd, gf, ga, w, d, l, played, club or rank", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: table [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: table [COLUMN [asc|desc]]")
		}
	}
	sc, ok := s.userSchedule()
	if !ok {
		return errors.New("your club has no season")
	}
	t, _ := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season})
	if t.RoundsCompleted == 0 && t.Season > 1 {
		// The off-season: last season's final table.
		t, _ = s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season - 1})
	}
	rows := append([]app.TableRow(nil), t.Rows...)
	slices.SortStableFunc(rows, func(a, b app.TableRow) int {
		var diff int
		switch col {
		case "pos", "rank":
			diff = cmp.Compare(a.Rank, b.Rank)
		case "club":
			diff = strings.Compare(strings.ToLower(a.Label.ClubName), strings.ToLower(b.Label.ClubName))
		case "p", "played":
			diff = cmp.Compare(a.Played, b.Played)
		case "w", "won":
			diff = cmp.Compare(a.Won, b.Won)
		case "d", "drawn":
			diff = cmp.Compare(a.Drawn, b.Drawn)
		case "l", "lost":
			diff = cmp.Compare(a.Lost, b.Lost)
		case "gf":
			diff = cmp.Compare(a.GoalsFor, b.GoalsFor)
		case "ga":
			diff = cmp.Compare(a.GoalsAgainst, b.GoalsAgainst)
		case "gd":
			diff = cmp.Compare(a.GoalDifference(), b.GoalDifference())
		case "pts", "points":
			diff = cmp.Compare(a.Points, b.Points)
		default:
			diff = cmp.Compare(a.Rank, b.Rank)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.Rank, b.Rank))
	})
	s.printf("\n%s season %d (%d/%d rounds)\n", t.CompetitionName, t.Season, t.RoundsCompleted, t.Rounds)
	s.printf("%3s  %-3s  %-22s %3s %3s %3s %3s %4s %4s %4s %4s\n", "POS", "ABB", "CLUB", "P", "W", "D", "L", "GF", "GA", "GD", "PTS")
	for _, r := range rows {
		mark := " "
		if r.Label.Club == s.club() {
			mark = "*"
		}
		s.printf("%3d%s %-3s  %-22s %3d %3d %3d %3d %4d %4d %+4d %4d\n", r.Rank, mark, r.Label.ShortName, r.Label.ClubName,
			r.Played, r.Won, r.Drawn, r.Lost, r.GoalsFor, r.GoalsAgainst, r.GoalDifference(), r.Points)
	}
	return nil
}

func (s *session) fixtures(args []string) error {
	col := "round"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "round":
			desc = false
		case "date", "when", "opp", "opponent", "result":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose round, date, opp or result", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: fixtures [round|date|opp|result [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: fixtures [round|date|opp|result [asc|desc]]")
		}
	}
	sc, ok := s.userSchedule()
	if !ok {
		return errors.New("your club has no season")
	}
	type fixLine struct {
		round   competitions.Round
		kickoff sim.GameInstant
		fixture app.FixtureLine
		result  string
	}
	var lines []fixLine
	for _, r := range sc.Rounds {
		for _, f := range r.Fixtures {
			if f.Home.Club != s.club() && f.Away.Club != s.club() {
				continue
			}
			result := r.Status.String()
			if f.Played {
				result = fmt.Sprintf("%d-%d %s", f.Score[0], f.Score[1], outcome(f, s.club()))
			}
			lines = append(lines, fixLine{round: r.Round, kickoff: r.Kickoff, fixture: f, result: result})
		}
	}
	slices.SortStableFunc(lines, func(a, b fixLine) int {
		var diff int
		switch col {
		case "round":
			diff = cmp.Compare(a.round, b.round)
		case "date", "when":
			diff = cmp.Compare(a.kickoff, b.kickoff)
		case "opp", "opponent":
			diff = strings.Compare(strings.ToLower(s.describe(a.fixture)), strings.ToLower(s.describe(b.fixture)))
		case "result":
			diff = strings.Compare(a.result, b.result)
		default:
			diff = cmp.Compare(a.round, b.round)
		}
		if desc {
			diff = -diff
		}
		return cmp.Or(diff, cmp.Compare(a.round, b.round), cmp.Compare(a.fixture.ID, b.fixture.ID))
	})
	cal := s.w.Calendar()
	s.printf("\n%s season %d\n", sc.CompetitionName, sc.Season)
	for _, l := range lines {
		s.printf("  R%-2d %s  F%-3d v %-30s %s\n", l.round, cal.Format(l.kickoff), l.fixture.ID, s.describe(l.fixture), l.result)
	}
	return nil
}

// outcome is W, D or L from the club's point of view.
// outcome is W, D or L for the club in a played fixture; a shootout
// decides a level knockout match.
func outcome(f app.FixtureLine, club ids.ClubID) string {
	us, them := f.Score[0], f.Score[1]
	if us == them {
		us, them = f.Shootout[0], f.Shootout[1]
	}
	if f.Away.Club == club {
		us, them = them, us
	}
	switch {
	case us > them:
		return "W"
	case us < them:
		return "L"
	}
	return "D"
}

func (s *session) inbox(args []string) error {
	n := 10
	order := "latest"
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err == nil {
			if v < 1 {
				return errors.New("usage: inbox [N]")
			}
			n = v
			if len(args) > 1 {
				switch strings.ToLower(args[1]) {
				case "oldest", "asc":
					order = "oldest"
				case "latest", "newest", "desc":
					order = "latest"
				default:
					return errors.New("usage: inbox [N] [latest|oldest]")
				}
			}
		} else {
			switch strings.ToLower(args[0]) {
			case "oldest", "asc":
				order = "oldest"
			case "latest", "newest", "desc":
				order = "latest"
			default:
				return errors.New("usage: inbox [N]")
			}
			if len(args) > 1 {
				if v, err := strconv.Atoi(args[1]); err == nil && v >= 1 {
					n = v
				}
			}
		}
	}
	items := s.w.Inbox()
	if len(items) == 0 {
		s.printf("Your inbox is empty.\n")
		return nil
	}
	var showing []app.InboxItem
	if order == "oldest" {
		showing = append([]app.InboxItem(nil), items[:min(n, len(items))]...)
	} else {
		showing = append([]app.InboxItem(nil), items[max(len(items)-n, 0):]...)
	}
	s.printf("\nInbox (%s %d of %d)\n", order, len(showing), len(items))
	for _, m := range showing {
		s.printMessage(m)
	}
	s.markInboxRead()
	return nil
}

func (s *session) printMessage(m app.InboxItem) {
	cal := s.w.Calendar()
	venue := "away"
	if m.Home {
		venue = "home"
	}
	s.printf("  %s  ", cal.Format(m.At))
	switch m.Kind {
	case inbox.KindMatchday:
		s.printf("matchday: %s v %s (%s)\n", itemMatchName(m), m.OpponentLabel.ClubName, venue)
	case inbox.KindResult:
		s.printf("result: %d-%d%s v %s (%s)\n", m.Goals[0], m.Goals[1], penalties(m.Shootout), m.OpponentLabel.ClubName, venue)
	case inbox.KindSeasonEnded:
		if m.Cup {
			s.printf("%s %d won by %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
			switch m.Stage {
			case "":
			case "winner":
				s.printf(": your club won it!")
			default:
				s.printf("; you went out in the %s", m.Stage)
			}
			s.printf("\n")
			break
		}
		s.printf("%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
		if m.Position > 0 {
			s.printf("; you finished %s", ordinal(m.Position))
		}
		s.printf("\n")
	case inbox.KindSeasonStarted:
		if m.Cup {
			s.printf("%s %d drawn: first kickoff %s (type cup)\n", m.CompetitionName, m.Season, cal.Format(m.Kickoff))
			break
		}
		s.printf("%s season %d scheduled: first kickoff %s\n", m.CompetitionName, m.Season, cal.Format(m.Kickoff))
	case inbox.KindRenewed:
		s.printf("contract: %s renewed until %s at %s a week\n", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindPlayerLeft:
		s.printf("contract: %s left the club as a free agent\n", m.PlayerName)
	case inbox.KindPlayerJoined:
		s.printf("signing: %s joined until %s at %s a week\n", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindRetired:
		s.printf("retirement: %s retired at %d\n", m.PlayerName, m.Age)
	case inbox.KindYouthJoined:
		s.printf("youth: %s joined from the youth ranks until %s at %s a week\n", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindDeveloped:
		s.printf("development: %d of your players improved and %d declined over the year (type squad)\n", m.Improved, m.Declined)
	case inbox.KindReleased:
		s.printf("release: %s left the club as a free agent; you paid %s\n", m.PlayerName, m.Compensation)
	default:
		s.printTransferMessage(m)
	}
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// markInboxRead remembers the newest message as shown.
func (s *session) markInboxRead() {
	if items := s.w.Inbox(); len(items) > 0 {
		s.lastSeen = items[len(items)-1].Event
	}
}

// transferNews reports whether an unshown inbox message is about a
// transfer.
func (s *session) transferNews() bool {
	for _, m := range s.w.Inbox() {
		if m.Event > s.lastSeen && m.Kind >= inbox.KindBidReceived {
			return true
		}
	}
	return false
}

// newMessages prints inbox messages not shown yet.
func (s *session) newMessages() {
	var fresh []app.InboxItem
	for _, m := range s.w.Inbox() {
		if m.Event > s.lastSeen {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 0 {
		return
	}
	s.printf("\nNew in your inbox:\n")
	for _, m := range fresh {
		s.printMessage(m)
	}
	s.markInboxRead()
}

// --- lineup --------------------------------------------------------------

// pendingFixture is the club's fixture in the batch waiting to be played.
func (s *session) pendingFixture() (ids.FixtureID, bool) {
	ready, ok := s.w.Pending()
	if !ok || len(ready.UserFixtures) == 0 {
		return 0, false
	}
	return ready.UserFixtures[0], true
}

// currentDraft returns the draft for the pending fixture, starting it from
// the submitted lineup, if any, or else the AI's suggestion.
func (s *session) currentDraft() (*draft, error) {
	fixture, ok := s.pendingFixture()
	if !ok {
		return nil, errors.New("no match is waiting; type continue to go to your next matchday")
	}
	if s.draft != nil && s.draft.fixture == fixture {
		return s.draft, nil
	}
	l, ok := s.w.SubmittedLineup(fixture)
	if !ok {
		var err error
		if l, err = s.w.SuggestLineup(fixture); err != nil {
			return nil, err
		}
	}
	s.draft = &draft{fixture: fixture, lineup: l}
	return s.draft, nil
}

var roleNames = map[string]matches.Role{"gk": matches.Goalkeeper, "df": matches.Defender, "mf": matches.Midfielder, "fw": matches.Forward}

func roleName(r matches.Role) string {
	for name, role := range roleNames {
		if role == r {
			return strings.ToUpper(name)
		}
	}
	return "?"
}

func naturalRole(p players.Position) matches.Role {
	switch p {
	case players.Goalkeeper:
		return matches.Goalkeeper
	case players.Defender:
		return matches.Defender
	case players.Midfielder:
		return matches.Midfielder
	case players.Forward:
		return matches.Forward
	}
	return 0
}

func (s *session) showLineup() error {
	d, err := s.currentDraft()
	if err != nil {
		return err
	}
	info := s.fixtureInfo(d.fixture)
	squad := s.squadByID()
	state := "AI suggestion"
	if _, ok := s.w.SubmittedLineup(d.fixture); ok {
		state = "submitted"
	}
	if d.edited {
		state = "your changes (used when you continue)"
	}
	s.printf("\n%s v %s, %s: %s\n", capitalize(matchName(info)), s.describe(info.FixtureLine), s.w.Calendar().Format(info.Kickoff), state)
	s.printf("Mentality: %s\n\n", d.lineup.Tactics.Mentality)
	s.printf("%3s  %-4s %4s  %-24s %-3s %5s %5s  %s\n", "#", "ROLE", "ID", "NAME", "POS", "OVR", "COND", " GK DEF PAS FIN PAC STA")
	for i, sl := range d.lineup.Starters {
		p := squad[sl.Player]
		note := ""
		if naturalRole(p.Position) != sl.Role {
			note = "  (out of position)"
		}
		s.printf("%3d  %-4s %4d  %-24s %-3s %5d %5s  %s%s\n", i+1, roleName(sl.Role), p.Player, p.Name, p.Position,
			p.Overall, condition(p.Condition), ratings(p.Attributes), note)
	}
	s.printf("\nBench:\n")
	for _, id := range d.lineup.Bench {
		p := squad[id]
		s.printf("     %-4s %4d  %-24s %-3s %5d %5s  %s\n", "", p.Player, p.Name, p.Position,
			p.Overall, condition(p.Condition), ratings(p.Attributes))
	}
	s.printf("\nRatings are 1-100: goalkeeping, defending, passing, finishing, pace, stamina.\n")
	s.printf("Edit with swap/role/mentality; continue plays the match.\n")
	return nil
}

// ratings formats attributes (1..100) in the column order GK DEF PAS FIN PAC STA.
func ratings(r players.Attributes) string {
	return fmt.Sprintf("%3d %3d %3d %3d %3d %3d", r[players.Goalkeeping], r[players.Defending], r[players.Passing],
		r[players.Finishing], r[players.Pace], r[players.Stamina])
}

func (s *session) squadByID() map[ids.PlayerID]app.SquadPlayer {
	players, _ := s.w.Squad(s.club())
	out := map[ids.PlayerID]app.SquadPlayer{}
	for _, p := range players {
		out[p.Player] = p
	}
	return out
}

func parsePlayer(arg string) (ids.PlayerID, error) {
	n, err := strconv.ParseUint(arg, 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%q is not a player ID (see squad)", arg)
	}
	return ids.PlayerID(n), nil
}

// edit applies change to a copy of the draft and keeps it only if the
// lineup is still valid.
func (s *session) edit(change func(l *selection.Lineup) error) error {
	d, err := s.currentDraft()
	if err != nil {
		return err
	}
	l := d.lineup.Clone()
	if err := change(&l); err != nil {
		return err
	}
	if err := l.Validate(); err != nil {
		return err
	}
	d.lineup, d.edited = l, true
	return s.showLineup()
}

// swap exchanges two players' places. Each is a starter (keeping the slot's
// role), on the bench, or in the squad but unselected.
func (s *session) swap(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: swap A B (player IDs)")
	}
	a, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	b, err := parsePlayer(args[1])
	if err != nil {
		return err
	}
	squad := s.squadByID()
	for _, id := range []ids.PlayerID{a, b} {
		if _, ok := squad[id]; !ok {
			return fmt.Errorf("player %d is not in your squad", id)
		}
	}
	return s.edit(func(l *selection.Lineup) error {
		find := func(id ids.PlayerID) *ids.PlayerID {
			for i := range l.Starters {
				if l.Starters[i].Player == id {
					return &l.Starters[i].Player
				}
			}
			for i := range l.Bench {
				if l.Bench[i] == id {
					return &l.Bench[i]
				}
			}
			return nil
		}
		pa, pb := find(a), find(b)
		switch {
		case pa == nil && pb == nil:
			return errors.New("neither player is in the lineup")
		case pa == nil:
			*pb = a
		case pb == nil:
			*pa = b
		default:
			*pa, *pb = b, a
		}
		return nil
	})
}

func (s *session) role(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: role P GK|DF|MF|FW")
	}
	p, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	role, ok := roleNames[strings.ToLower(args[1])]
	if !ok {
		return errors.New("role must be GK, DF, MF or FW")
	}
	return s.edit(func(l *selection.Lineup) error {
		for i := range l.Starters {
			if l.Starters[i].Player == p {
				l.Starters[i].Role = role
				return nil
			}
		}
		return fmt.Errorf("player %d is not in the starting XI", p)
	})
}

func (s *session) mentality(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: mentality defensive|balanced|attacking")
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if m.String() == strings.ToLower(args[0]) {
			return s.edit(func(l *selection.Lineup) error { l.Tactics.Mentality = m; return nil })
		}
	}
	return errors.New("mentality must be defensive, balanced or attacking")
}

func (s *session) reset() error {
	fixture, ok := s.pendingFixture()
	if !ok {
		return errors.New("no match is waiting")
	}
	l, err := s.w.SuggestLineup(fixture)
	if err != nil {
		return err
	}
	s.draft = &draft{fixture: fixture, lineup: l}
	s.printf("Lineup reset to the AI's suggestion.\n")
	return nil
}

// --- progression ---------------------------------------------------------

// next plays the waiting batch, or advances to the next batch that includes
// the club, resolving any batch without it on the way.
func (s *session) next() error {
	if _, ok := s.w.Pending(); ok {
		return s.play()
	}
	return s.advance()
}

// advance continues until the club's next matchday. It stops at the opening
// of a transfer window, and while a window is open it goes one transfer run
// at a time and stops at transfer news for the club.
func (s *session) advance() error {
	for {
		target, warn := s.warnBeforeContractYear(s.w.Now() + 400*sim.GameInstant(sim.Day))
		win, opening, stepping := s.w.TransferWindow(), false, false
		switch {
		case !win.Open && win.Opens < target:
			target, warn, opening = win.Opens, false, true
		case win.NextRun < target:
			target, warn, stepping = win.NextRun, false, true
		}
		res, err := s.w.Continue(target)
		if err != nil {
			return err
		}
		ready, ok := res.(app.FixtureRoundReady)
		if !ok && opening {
			s.newMessages()
			s.printf("\n%s: %s\nType transfers, market POS and bid ID to buy players; continue goes on to the next transfer news.\n",
				s.w.Calendar().Format(s.w.Now()), s.windowLine())
			return nil
		}
		if !ok && stepping {
			if s.transferNews() {
				s.newMessages()
				s.printf("\n%s. Type transfers to review, or continue to go on.\n", s.w.Calendar().Format(s.w.Now()))
				return nil
			}
			continue
		}
		if !ok && warn {
			s.newMessages()
			s.warnedYearEnd = s.w.ContractYearEnd()
			s.printf("\n%s: %d of your players' contracts end tomorrow. Type contracts to review them;\n", s.w.Calendar().Format(s.w.Now()), len(s.expiring()))
			s.printf("players you do not renew leave as free agents. Type continue to go on.\n")
			return nil
		}
		if !ok {
			s.newMessages()
			s.printf("Nothing is scheduled before %s.\n", s.w.Calendar().Format(target))
			return nil
		}
		if len(ready.UserFixtures) == 0 {
			if _, err := s.resolve(false); err != nil {
				return err
			}
			continue
		}
		s.newMessages()
		info := s.fixtureInfo(ready.UserFixtures[0])
		s.printf("\nMATCHDAY %s: %s v %s.\n", s.w.Calendar().Format(info.Kickoff), matchName(info), s.describe(info.FixtureLine))
		s.printf("Type lineup to check your team, or continue to play with it.\n")
		return nil
	}
}

// play submits an edited draft, plays the waiting batch and reports it.
func (s *session) play() error {
	res, err := s.resolve(true)
	if err != nil {
		return err
	}
	for _, m := range res.Matches {
		if m.Home.Club == s.club() || m.Away.Club == s.club() {
			f := app.FixtureLine{Home: m.Home, Away: m.Away, Played: true, Score: m.Score, Shootout: m.Shootout}
			s.printf("\nFULL TIME  %s %d-%d %s%s  (%s)\n", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, penalties(m.Shootout), outcome(f, s.club()))
			names := map[ids.PlayerID]string{}
			for _, c := range []ids.ClubID{m.Home.Club, m.Away.Club} {
				squad, _ := s.w.Squad(c)
				for _, p := range squad {
					names[p.Player] = p.Name
				}
			}
			for _, g := range m.Goals {
				who := names[g.Scorer]
				team := m.Home.ShortName
				if g.Side == matches.Away {
					team = m.Away.ShortName
				}
				s.printf("  %2d'  %s %s\n", g.Minute, team, who)
			}
		}
	}
	s.printf("\nOther results:\n")
	for _, m := range res.Matches {
		if m.Home.Club != s.club() && m.Away.Club != s.club() {
			s.printf("  %-22s %d-%d %s%s\n", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, penalties(m.Shootout))
		}
	}
	s.newMessages()
	s.printf("\nType table for the standings, or continue for your next matchday.\n")
	return nil
}

// resolve resolves the waiting batch; with submit, an edited draft for the
// club's fixture is submitted first.
func (s *session) resolve(submit bool) (app.RoundsResolved, error) {
	ready, ok := s.w.Pending()
	if !ok {
		return app.RoundsResolved{}, errors.New("no match is waiting")
	}
	if submit {
		if err := s.submitDraft(ready); err != nil {
			return app.RoundsResolved{}, err
		}
	}
	cmd := app.ResolveRounds{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision()}
	for _, r := range ready.Rounds {
		cmd.Rounds = append(cmd.Rounds, r.Round)
	}
	res, err := s.w.ResolveRounds(cmd)
	if err != nil {
		return app.RoundsResolved{}, err
	}
	s.draft = nil
	return res, nil
}

// submitDraft submits an edited lineup draft for a pending fixture that has
// not kicked off live.
func (s *session) submitDraft(ready app.FixtureRoundReady) error {
	if s.draft == nil || !s.draft.edited || !slices.Contains(ready.UserFixtures, s.draft.fixture) {
		return nil
	}
	if _, live := s.w.LiveMatch(); live {
		return nil
	}
	_, err := s.w.SubmitLineup(app.SubmitLineup{
		ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: s.draft.fixture, Lineup: s.draft.lineup,
	})
	return err
}

// season plays the rest of the current season. An edited draft for the
// waiting match is used for that match; later matches use the AI's picks.
func (s *session) season() error {
	sc, ok := s.userSchedule()
	if !ok {
		return errors.New("your club has no season")
	}
	ref := competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season}
	s.printf("\nPlaying the rest of %s season %d:\n", sc.CompetitionName, sc.Season)
	for {
		if _, ok := s.w.Pending(); ok {
			res, err := s.resolve(true)
			if err != nil {
				return err
			}
			for _, m := range res.Matches {
				if m.Home.Club == s.club() || m.Away.Club == s.club() {
					f := app.FixtureLine{Home: m.Home, Away: m.Away, Played: true, Score: m.Score, Shootout: m.Shootout}
					s.printf("  R%-2d %-22s %d-%d %-22s %s\n", m.Round.Round, m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, outcome(f, s.club()))
				}
			}
			continue
		}
		if t, _ := s.w.Table(ref); t.Complete {
			// Run the season end due now, which schedules the next
			// season and draws any cup it qualifies teams for.
			if _, err := s.w.Continue(s.w.Now()); err != nil {
				return err
			}
			break
		}
		if err := s.advanceQuietly(); err != nil {
			return err
		}
	}
	s.table(nil)
	s.printf("\nSeason finished. Type continue for the next season.\n")
	return nil
}

// advanceQuietly continues to the next batch of any club without reporting.
func (s *session) advanceQuietly() error {
	res, err := s.w.Continue(s.w.Now() + 400*sim.GameInstant(sim.Day))
	if err != nil {
		return err
	}
	if _, ok := res.(app.FixtureRoundReady); !ok {
		return errors.New("no more matches are scheduled")
	}
	return nil
}

func (s *session) save(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: save [FILE]")
	}
	if len(args) == 1 {
		s.savePath = args[0]
	}
	if err := storage.Save(s.savePath, s.w); err != nil {
		return err
	}
	s.saved, s.savedRevision = true, s.w.Revision()
	s.printf("Saved to %s. Resume with: go run ./cmd/play -load %s\n", s.savePath, s.savePath)
	if s.draft != nil && s.draft.edited {
		s.printf("(Your lineup edits are not saved until the match is played.)\n")
	}
	return nil
}

// --- live match ----------------------------------------------------------

// watch plays the club's waiting match live to a minute: by default to half
// time, then to full time. An edited lineup is submitted at kickoff.
func (s *session) watch(args []string) error {
	fixture, ok := s.pendingFixture()
	if !ok {
		return errors.New("no match is waiting; type continue to go to your next matchday")
	}
	current := uint16(0)
	l, live := s.w.LiveMatch()
	if live {
		current = l.Position.Minute
	} else {
		ready, _ := s.w.Pending()
		if err := s.submitDraft(ready); err != nil {
			return err
		}
		s.draft, s.liveShown = nil, 0
		info := s.fixtureInfo(fixture)
		s.printf("\nKICKOFF  %s v %s (%s)\n", info.Home.ClubName, info.Away.ClubName, matchName(info))
	}
	target := uint16(matches.HalfTimeMinute)
	if current >= matches.HalfTimeMinute {
		target = matches.RegulationMinutes
	}
	if len(args) > 0 {
		n, err := strconv.ParseUint(args[0], 10, 16)
		if err != nil {
			return errors.New("usage: watch [MINUTE]")
		}
		target = uint16(n)
	}
	if current == matches.RegulationMinutes {
		return errors.New("full time: type continue to finish the round")
	}
	res, err := s.w.PlayMatch(app.PlayMatch{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, ToMinute: target})
	if err != nil {
		return err
	}
	s.liveEvents(res.Live)
	l = res.Live
	s.printf("\n%d'  %s %d-%d %s", l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
	switch l.Status {
	case matches.MatchDecisionRequired:
		s.printf("  HALF TIME\nMake changes (sub OUT IN, mentality M, lineup), then watch or continue.\n")
	case matches.MatchFinished:
		s.printf("%s  FULL TIME\nType continue to confirm the result and see the round.\n", penalties(l.View.Shootout))
	default:
		s.printf("\nwatch to play on, sub/mentality to make changes, continue to finish the match.\n")
	}
	return nil
}

// playerNames maps both sides' selected players to names.
func (s *session) playerNames(l app.LiveMatch) map[ids.PlayerID]string {
	names := map[ids.PlayerID]string{}
	for _, club := range []ids.ClubID{l.Home.Club, l.Away.Club} {
		squad, _ := s.w.Squad(club)
		for _, p := range squad {
			names[p.Player] = p.Name
		}
	}
	return names
}

// liveEvents prints match events not printed yet.
func (s *session) liveEvents(l app.LiveMatch) {
	names := s.playerNames(l)
	short := func(side matches.Side) string {
		if side == matches.Away {
			return l.Away.ShortName
		}
		return l.Home.ShortName
	}
	var score [2]uint16
	for i, e := range l.Events {
		if e.Kind == matches.EventGoal {
			score[e.Side.Index()]++
		}
		if i < s.liveShown {
			continue
		}
		switch e.Kind {
		case matches.EventGoal:
			s.printf("  %2d'  GOAL  %-3s  %s  (%d-%d)\n", e.Minute, short(e.Side), names[e.Player], score[0], score[1])
		case matches.EventSubstitution:
			s.printf("  %2d'  SUB   %-3s  %s on for %s\n", e.Minute, short(e.Side), names[e.Player], names[e.Other])
		case matches.EventMentalityChange:
			s.printf("  %2d'  TACT  %-3s  now %s\n", e.Minute, short(e.Side), e.Mentality)
		case matches.EventPeriodEnd:
			if e.Period == matches.FirstHalf {
				s.printf("  %2d'  half time\n", e.Minute)
			} else {
				s.printf("  %2d'  full time\n", e.Minute)
			}
		}
	}
	s.liveShown = len(l.Events)
}

// decide submits a decision for the club's side of the live match.
func (s *session) decide(c matches.MatchCommand) error {
	l, live := s.w.LiveMatch()
	if !live {
		return errors.New("your match has not kicked off; type watch to play it live")
	}
	c.Side = l.Side
	res, err := s.w.MatchDecision(app.MatchDecision{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: l.Fixture, Command: c})
	if err != nil {
		return err
	}
	s.liveEvents(res.Live)
	return nil
}

func (s *session) sub(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: sub OUT IN (player IDs)")
	}
	out, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	in, err := parsePlayer(args[1])
	if err != nil {
		return err
	}
	return s.decide(matches.MatchCommand{Kind: matches.CommandSubstitute, Out: out, In: in})
}

func (s *session) liveMentality(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: mentality defensive|balanced|attacking")
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if m.String() == strings.ToLower(args[0]) {
			return s.decide(matches.MatchCommand{Kind: matches.CommandSetMentality, Mentality: m})
		}
	}
	return errors.New("mentality must be defensive, balanced or attacking")
}

// showLive lists the club's players on the pitch and the bench during the
// live match.
func (s *session) showLive() error {
	l, _ := s.w.LiveMatch()
	side := l.Side.Index()
	team := l.Teams[side]
	squad := s.squadByID()
	role := map[ids.PlayerID]matches.Role{}
	for _, p := range append(slices.Clone(team.Starters), team.Bench...) {
		role[p.Player] = p.Role
	}
	subs := int(l.Rules.MaxSubstitutions) - int(l.View.SubstitutionsUsed[side])
	s.printf("\n%d'  %s %d-%d %s | mentality %s | %d substitutions left\n", l.Position.Minute, l.Home.ClubName,
		l.View.Score[0], l.View.Score[1], l.Away.ClubName, l.View.Mentality[side], subs)
	s.printf("\nOn the pitch:\n")
	onPitch := map[ids.PlayerID]bool{}
	for _, id := range l.View.OnPitch[side] {
		onPitch[id] = true
		p := squad[id]
		s.printf("  %-4s %4d  %-24s %-3s %5d  %s\n", roleName(role[id]), id, p.Name, p.Position, p.Overall, ratings(p.Attributes))
	}
	cameOn := map[ids.PlayerID]bool{}
	var off []string
	for _, e := range l.Events {
		if e.Kind == matches.EventSubstitution && e.Side == l.Side {
			cameOn[e.Player] = true
			off = append(off, fmt.Sprintf("%d %s (%d')", e.Other, squad[e.Other].Name, e.Minute))
		}
	}
	s.printf("\nBench:\n")
	for _, b := range team.Bench {
		state := "available"
		switch {
		case onPitch[b.Player]:
			state = "on the pitch"
		case cameOn[b.Player]:
			state = "came on, then off"
		}
		p := squad[b.Player]
		s.printf("  %-4s %4d  %-24s %-3s %5d  %s\n", roleName(b.Role), b.Player, p.Name, p.Position, p.Overall, state)
	}
	if len(off) > 0 {
		s.printf("\nTaken off: %s\n", strings.Join(off, ", "))
	}
	return nil
}

// monthDay names the contract end day, e.g. "1 July".
func monthDay(epoch sim.CivilTime) string {
	months := [...]string{"January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
	return fmt.Sprintf("1 %s", months[epoch.Month-1])
}

// finances shows the club's balance, wage bill and latest ledger entries.
func (s *session) finances(args []string) error {
	n := 10
	col := "date"
	desc := false
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err == nil {
			if v < 1 {
				return errors.New("usage: finances [N]")
			}
			n = v
			if len(args) > 1 {
				col = strings.ToLower(args[1])
				switch col {
				case "date", "when", "what":
					desc = false
				case "amount", "balance":
					desc = true
				default:
					return fmt.Errorf("unknown sort %q: choose date, what, amount or balance", args[1])
				}
				if len(args) > 2 {
					switch strings.ToLower(args[2]) {
					case "asc":
						desc = false
					case "desc":
						desc = true
					default:
						return errors.New("usage: finances [N [COLUMN [asc|desc]]]")
					}
				}
			}
		} else {
			switch strings.ToLower(args[0]) {
			case "date", "when", "what":
				col = strings.ToLower(args[0])
				desc = false
			case "amount", "balance":
				col = strings.ToLower(args[0])
				desc = true
			default:
				return errors.New("usage: finances [N]")
			}
			if len(args) > 1 {
				switch strings.ToLower(args[1]) {
				case "asc":
					desc = false
				case "desc":
					desc = true
				default:
					return errors.New("usage: finances [COLUMN [asc|desc]]")
				}
			}
		}
	}
	fin, ok := s.w.Finances(s.club())
	if !ok {
		return errors.New("your club has no account")
	}
	cal := s.w.Calendar()
	s.printf("\nBalance %s | weekly wages %s | %d ledger entries\n", fin.Balance, fin.WeeklyWage, len(fin.Entries))
	var running money.Money
	type row struct {
		at      sim.GameInstant
		what    string
		amount  money.Money
		balance money.Money
	}
	allRows := make([]row, len(fin.Entries))
	for i, e := range fin.Entries {
		running += e.Amount
		what := e.Kind.String()
		if e.Fixture != 0 {
			what = fmt.Sprintf("%s F%d", what, e.Fixture)
		}
		if e.Offer != 0 {
			what = fmt.Sprintf("%s O%d", what, e.Offer)
		}
		if e.Player != 0 {
			what = fmt.Sprintf("%s P%d", what, e.Player)
		}
		allRows[i] = row{at: e.At, what: what, amount: e.Amount, balance: running}
	}
	start := max(len(allRows)-n, 0)
	rows := append([]row(nil), allRows[start:]...)
	if col != "date" || desc {
		slices.SortStableFunc(rows, func(a, b row) int {
			var diff int
			switch col {
			case "date", "when":
				diff = cmp.Compare(a.at, b.at)
			case "what":
				diff = strings.Compare(strings.ToLower(a.what), strings.ToLower(b.what))
			case "amount":
				diff = cmp.Compare(a.amount, b.amount)
			case "balance":
				diff = cmp.Compare(a.balance, b.balance)
			}
			if desc {
				diff = -diff
			}
			return diff
		})
	}
	s.printf("\n%-26s %-16s %14s %16s\n", "DATE", "WHAT", "AMOUNT", "BALANCE")
	for _, r := range rows {
		s.printf("%-26s %-16s %14s %16s\n", cal.Format(r.at), r.what, r.amount, r.balance)
	}
	return nil
}

// --- cups ------------------------------------------------------------------

// penalties renders a shootout, e.g. " (4-3 on penalties)", or nothing.
func penalties(p [2]uint16) string {
	if p == [2]uint16{} {
		return ""
	}
	return fmt.Sprintf(" (%d-%d on penalties)", p[0], p[1])
}

// itemMatchName names an inbox message's round like matchName.
func itemMatchName(m app.InboxItem) string {
	if m.Cup {
		return m.CompetitionName + " " + m.RoundName
	}
	return m.RoundName
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// cup shows every cup's latest edition: its rounds, ties and results.
func (s *session) cup(args []string) error {
	col := "id"
	desc := false
	if len(args) > 0 {
		col = strings.ToLower(args[0])
		switch col {
		case "score", "result":
			desc = true
		case "home", "away", "id":
			desc = false
		default:
			return fmt.Errorf("unknown sort %q: choose home, away, score or id", args[0])
		}
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "asc":
				desc = false
			case "desc":
				desc = true
			default:
				return errors.New("usage: cup [COLUMN [asc|desc]]")
			}
		}
		if len(args) > 2 {
			return errors.New("usage: cup [COLUMN [asc|desc]]")
		}
	}
	cups := s.w.Cups()
	if len(cups) == 0 {
		s.printf("No cup has been drawn yet: the Continental Cup starts when the league seasons end, with the top four of each league.\n")
		return nil
	}
	cal := s.w.Calendar()
	for _, c := range cups {
		s.printf("\n%s %d\n", c.Name, c.Edition)
		for _, r := range c.Rounds {
			title := capitalize(r.Name)
			if len(r.Ties) != 1 && r.Name != "final" {
				title += "s"
			}
			s.printf("\n%s, %s\n", title, cal.Format(r.Kickoff))
			if len(r.Ties) == 0 {
				s.printf("  to be decided\n")
			}
			ties := slices.Clone(r.Ties)
			if len(args) > 0 {
				slices.SortStableFunc(ties, func(a, b app.FixtureLine) int {
					var diff int
					switch col {
					case "home":
						diff = strings.Compare(strings.ToLower(a.Home.ClubName), strings.ToLower(b.Home.ClubName))
					case "away":
						diff = strings.Compare(strings.ToLower(a.Away.ClubName), strings.ToLower(b.Away.ClubName))
					case "score", "result":
						diff = cmp.Compare(a.Score[0]+a.Score[1], b.Score[0]+b.Score[1])
					default:
						diff = cmp.Compare(a.ID, b.ID)
					}
					if desc {
						diff = -diff
					}
					return cmp.Or(diff, cmp.Compare(a.ID, b.ID))
				})
			}
			for _, f := range ties {
				result := "v"
				if f.Played {
					result = fmt.Sprintf("%d-%d", f.Score[0], f.Score[1])
				}
				mark := "  "
				if f.Home.Club == s.club() || f.Away.Club == s.club() {
					mark = "* "
				}
				s.printf("%s%-22s %5s %s%s\n", mark, f.Home.ClubName, result, f.Away.ClubName, penalties(f.Shootout))
			}
		}
		if c.Champion != nil {
			s.printf("\nWinner: %s\n", c.Champion.ClubName)
		}
	}
	return nil
}
