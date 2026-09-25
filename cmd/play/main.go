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
			s.squad()
		case "table", "t":
			s.table()
		case "fixtures", "f":
			s.fixtures()
		case "finances", "money":
			err = s.finances(args)
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
  fixtures (f)          your club's fixtures and results this season
  inbox (i) [N]         the latest N inbox messages (default 10)
  finances [N]          your balance, weekly wage bill and latest N ledger entries
  lineup (l)            your lineup for the match waiting to be played
  swap A B              swap two players (IDs) between XI, bench and squad
  role P GK|DF|MF|FW    play starter P in another role
  mentality (m) M       defensive, balanced or attacking (during your match: a live change)
  reset                 go back to the AI's suggested lineup
  watch (w) [MIN]       play your match live to MIN (default: half time, then full time)
  sub OUT IN            during your match: substitute player OUT with IN (IDs)
  continue (c)          play the waiting match, or go to your next matchday
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

// fixtureInfo finds a fixture of the club's current season.
func (s *session) fixtureInfo(id ids.FixtureID) (app.FixtureLine, app.RoundSchedule, bool) {
	sc, _ := s.userSchedule()
	for _, r := range sc.Rounds {
		for _, f := range r.Fixtures {
			if f.ID == id {
				return f, r, true
			}
		}
	}
	return app.FixtureLine{}, app.RoundSchedule{}, false
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
	if l, live := s.w.LiveMatch(); live {
		s.printf("LIVE %d'  %s %d-%d %s. watch to play on, sub/mentality to change, continue to finish.\n",
			l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
		return
	}
	if fixture, ok := s.pendingFixture(); ok {
		f, r, _ := s.fixtureInfo(fixture)
		s.printf("MATCHDAY: round %d v %s is waiting. Check lineup, then watch it live or continue to play.\n", r.Round, s.describe(f))
		return
	}
	for _, r := range sc.Rounds {
		for _, f := range r.Fixtures {
			if !f.Played && (f.Home.Club == s.club() || f.Away.Club == s.club()) {
				s.printf("Next: round %d v %s, %s. Type continue to go there.\n", r.Round, s.describe(f), cal.Format(r.Kickoff))
				return
			}
		}
	}
	s.printf("Season finished. Type continue for the next season.\n")
}

func (s *session) squad() {
	players, _ := s.w.Squad(s.club())
	marks := map[ids.PlayerID]string{}
	if d, err := s.currentDraft(); err == nil {
		for i, sl := range d.lineup.Starters {
			marks[sl.Player] = fmt.Sprintf("XI %-2d", i+1)
		}
		for _, p := range d.lineup.Bench {
			marks[p] = "bench"
		}
	}
	cal := s.w.Calendar()
	s.printf("\n%4s  %-3s %-24s %5s %5s %12s %8s  %s\n", "ID", "POS", "NAME", "OVR", "COND", "WAGE/WEEK", "CONTRACT", "PICK")
	for _, p := range players {
		ends, _ := cal.Civil(p.Contract.Expires)
		s.printf("%4d  %-3s %-24s %5d %5s %12s %8s  %s\n", p.Player, p.Position, p.Name,
			p.Overall, condition(p.Condition), p.Contract.WeeklyWage, fmt.Sprintf("to %d", ends.Year), marks[p.Player])
	}
	s.printf("Ratings are 1-100. COND is fitness (100%% = fully fit). Contracts end on %s of the year shown.\n",
		monthDay(cal.Epoch()))
	s.printf("Lineup shows each player's attributes.\n")
}

func condition(c uint8) string { return fmt.Sprintf("%d%%", c) }

func (s *session) table() {
	sc, ok := s.userSchedule()
	if !ok {
		return
	}
	t, _ := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season})
	s.printf("\n%s season %d (%d/%d rounds)\n", t.CompetitionName, t.Season, t.RoundsCompleted, t.Rounds)
	s.printf("%3s  %-3s  %-22s %3s %3s %3s %3s %4s %4s %4s %4s\n", "POS", "ABB", "CLUB", "P", "W", "D", "L", "GF", "GA", "GD", "PTS")
	for _, r := range t.Rows {
		mark := " "
		if r.Label.Club == s.club() {
			mark = "*"
		}
		s.printf("%3d%s %-3s  %-22s %3d %3d %3d %3d %4d %4d %+4d %4d\n", r.Rank, mark, r.Label.ShortName, r.Label.ClubName,
			r.Played, r.Won, r.Drawn, r.Lost, r.GoalsFor, r.GoalsAgainst, r.GoalDifference(), r.Points)
	}
}

func (s *session) fixtures() {
	sc, _ := s.userSchedule()
	cal := s.w.Calendar()
	s.printf("\n%s season %d\n", sc.CompetitionName, sc.Season)
	for _, r := range sc.Rounds {
		for _, f := range r.Fixtures {
			if f.Home.Club != s.club() && f.Away.Club != s.club() {
				continue
			}
			result := r.Status.String()
			if f.Played {
				result = fmt.Sprintf("%d-%d %s", f.Score[0], f.Score[1], outcome(f, s.club()))
			}
			s.printf("  R%-2d %s  F%-3d v %-30s %s\n", r.Round, cal.Format(r.Kickoff), f.ID, s.describe(f), result)
		}
	}
}

// outcome is W, D or L from the club's point of view.
func outcome(f app.FixtureLine, club ids.ClubID) string {
	us, them := f.Score[0], f.Score[1]
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
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 {
			return errors.New("usage: inbox [N]")
		}
		n = v
	}
	items := s.w.Inbox()
	if len(items) == 0 {
		s.printf("Your inbox is empty.\n")
		return nil
	}
	s.printf("\nInbox (latest %d of %d)\n", min(n, len(items)), len(items))
	for _, m := range items[max(len(items)-n, 0):] {
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
		s.printf("matchday: round %d v %s (%s)\n", m.Round, m.OpponentLabel.ClubName, venue)
	case inbox.KindResult:
		s.printf("result: %d-%d v %s (%s)\n", m.Goals[0], m.Goals[1], m.OpponentLabel.ClubName, venue)
	case inbox.KindSeasonEnded:
		s.printf("%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
		if m.Position > 0 {
			s.printf("; you finished %s", ordinal(m.Position))
		}
		s.printf("\n")
	case inbox.KindSeasonStarted:
		s.printf("%s season %d scheduled: first kickoff %s\n", m.CompetitionName, m.Season, cal.Format(m.Kickoff))
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
	f, r, _ := s.fixtureInfo(d.fixture)
	squad := s.squadByID()
	state := "AI suggestion"
	if _, ok := s.w.SubmittedLineup(d.fixture); ok {
		state = "submitted"
	}
	if d.edited {
		state = "your changes (used when you continue)"
	}
	s.printf("\nRound %d v %s, %s: %s\n", r.Round, s.describe(f), s.w.Calendar().Format(r.Kickoff), state)
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

// advance continues until the club's next matchday.
func (s *session) advance() error {
	for {
		target := s.w.Now() + 400*sim.GameInstant(sim.Day)
		res, err := s.w.Continue(target)
		if err != nil {
			return err
		}
		ready, ok := res.(app.FixtureRoundReady)
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
		f, r, _ := s.fixtureInfo(ready.UserFixtures[0])
		s.printf("\nMATCHDAY %s: round %d v %s.\n", s.w.Calendar().Format(r.Kickoff), r.Round, s.describe(f))
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
			f, _, _ := s.fixtureInfo(m.Fixture)
			f.Score, f.Played = m.Score, true
			s.printf("\nFULL TIME  %s %d-%d %s  (%s)\n", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, outcome(f, s.club()))
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
			s.printf("  %-22s %d-%d %s\n", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName)
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
					f, r, _ := s.fixtureInfo(m.Fixture)
					f.Score = m.Score
					s.printf("  R%-2d %-22s %d-%d %-22s %s\n", r.Round, m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, outcome(f, s.club()))
				}
			}
			continue
		}
		if t, _ := s.w.Table(ref); t.Complete {
			break
		}
		if err := s.advanceQuietly(); err != nil {
			return err
		}
	}
	s.table()
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
		f, _, _ := s.fixtureInfo(fixture)
		s.printf("\nKICKOFF  %s v %s\n", f.Home.ClubName, f.Away.ClubName)
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
		s.printf("  FULL TIME\nType continue to confirm the result and see the round.\n")
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
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 {
			return errors.New("usage: finances [N]")
		}
		n = v
	}
	fin, ok := s.w.Finances(s.club())
	if !ok {
		return errors.New("your club has no account")
	}
	cal := s.w.Calendar()
	s.printf("\nBalance %s | weekly wages %s | %d ledger entries\n", fin.Balance, fin.WeeklyWage, len(fin.Entries))
	var running money.Money
	balances := make([]money.Money, len(fin.Entries))
	for i, e := range fin.Entries {
		running += e.Amount
		balances[i] = running
	}
	start := max(len(fin.Entries)-n, 0)
	s.printf("\n%-26s %-16s %14s %16s\n", "DATE", "WHAT", "AMOUNT", "BALANCE")
	for i := start; i < len(fin.Entries); i++ {
		e := fin.Entries[i]
		what := e.Kind.String()
		if e.Fixture != 0 {
			what = fmt.Sprintf("%s F%d", what, e.Fixture)
		}
		s.printf("%-26s %-16s %14s %16s\n", cal.Format(e.At), what, e.Amount, balances[i])
	}
	return nil
}
