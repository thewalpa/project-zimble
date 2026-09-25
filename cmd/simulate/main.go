// Command simulate is the headless runner. It starts a world, runs one mode,
// and optionally saves the result.
//
// Start (one of):
//
//	-seed N      generate a new world from seed N
//	(nothing)    generate a new world from a random seed, reported on stderr
//	-load FILE   continue a saved career; its seed and configuration come from
//	             the file, so -seed is rejected rather than silently ignored
//
// Mode (at most one):
//
//	-season      resolve every remaining round and print results and tables
//	-rounds N    resolve at most N more fixture batches
//	(nothing)    new world: print the calendar and demonstrate Continue up to
//	             the first pending round; loaded world: print its status only
//
// Management (new worlds only for -club; a loaded career keeps its club):
//
//	-club N        manage club N; its fixtures are marked in the output
//	-mentality M   with -season or -rounds: before each of the club's
//	               matches, submit the suggested lineup with mentality M
//	               (defensive, balanced or attacking) instead of leaving the
//	               AI selection to play
//
// Then -save FILE writes the world atomically (only if the run succeeded).
//
//	go run ./cmd/simulate -seed 42 -rounds 7 -save career.json
//	go run ./cmd/simulate -load career.json -season
//	go run ./cmd/simulate -seed 42 -club 3 -mentality attacking -season
package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/storage"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, cryptoSeed); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "simulate:", err)
		}
		os.Exit(2)
	}
}

// cryptoSeed draws a seed from the operating system's random source. Only the
// CLI picks seeds; the simulation itself stays deterministic for a given seed.
func cryptoSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("draw random seed: %w", err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// run parses args and runs the simulation. newSeed supplies the seed when a
// new world is generated without -seed; the chosen seed is reported on stderr
// and in the world header so the run can be repeated with -seed.
func run(args []string, stdout, stderr io.Writer, newSeed func() (uint64, error)) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Uint64("seed", 0, "world seed for a new world (default: random, reported on stderr)")
	load := fs.String("load", "", "continue the career saved in `FILE` (keeps its seed and configuration)")
	save := fs.String("save", "", "after a successful run, save the world to `FILE` (replaced atomically)")
	season := fs.Bool("season", false, "resolve every remaining round and print the tables")
	rounds := fs.Int("rounds", 0, "resolve at most `N` more fixture batches")
	club := fs.Uint64("club", 0, "manage club `N` in a new world")
	mentalityName := fs.String("mentality", "", "submit the suggested lineup with mentality `M` for each of the managed club's matches")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	switch {
	case set["load"] && set["seed"]:
		return errors.New("-seed cannot be used with -load: a loaded career keeps its saved seed and configuration")
	case set["season"] && set["rounds"]:
		return errors.New("-season and -rounds cannot be combined")
	case set["rounds"] && *rounds < 1:
		return errors.New("-rounds must be at least 1")
	case set["load"] && *load == "":
		return errors.New("-load needs a file name")
	case set["save"] && *save == "":
		return errors.New("-save needs a file name")
	case set["load"] && set["club"]:
		return errors.New("-club cannot be used with -load: a loaded career keeps its saved club")
	case set["club"] && *club == 0:
		return errors.New("-club must name a club ID (1 or more)")
	case set["mentality"] && !*season && !set["rounds"]:
		return errors.New("-mentality needs -season or -rounds")
	}
	var mentality matches.Mentality
	if set["mentality"] {
		m, err := parseMentality(*mentalityName)
		if err != nil {
			return err
		}
		mentality = m
	}

	var w *app.World
	if set["load"] {
		loaded, err := storage.Load(*load)
		if err != nil {
			return fmt.Errorf("load %s: %w", *load, err)
		}
		w = loaded
	} else {
		if !set["seed"] {
			drawn, err := newSeed()
			if err != nil {
				return err
			}
			*seed = drawn
			fmt.Fprintf(stderr, "simulate: using random seed %d (rerun with -seed %d to reproduce)\n", drawn, drawn)
		}
		cfg := app.DefaultConfig(random.Seed(*seed))
		cfg.UserClub = ids.ClubID(*club)
		created, err := app.NewWorld(cfg)
		if err != nil {
			return err
		}
		w = created
	}
	if _, managed := w.UserClub(); mentality != 0 && !managed {
		return errors.New("-mentality needs a managed club: use -club, or load a career that has one")
	}

	if err := printSummary(stdout, w.Summary()); err != nil {
		return err
	}
	printManager(stdout, w)
	var err error
	switch {
	case *season:
		err = playRounds(stdout, w, -1, mentality)
	case set["rounds"]:
		err = playRounds(stdout, w, *rounds, mentality)
	case set["load"]:
		err = printStatus(stdout, w)
	default:
		if err = printSchedule(stdout, w.Calendar(), w.Schedules()[0]); err == nil {
			err = demoContinue(stdout, w)
		}
	}
	if err != nil {
		return err
	}

	if set["save"] {
		if err := storage.Save(*save, w); err != nil {
			return fmt.Errorf("save %s: %w", *save, err)
		}
		fmt.Fprintf(stdout, "\nsaved %s: revision %d, now %s\n", *save, w.Revision(), w.Calendar().Format(w.Now()))
	}
	return nil
}

func parseMentality(name string) (matches.Mentality, error) {
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if m.String() == name {
			return m, nil
		}
	}
	return 0, fmt.Errorf("-mentality %q: want defensive, balanced or attacking", name)
}

// printManager names the managed club, if any.
func printManager(out io.Writer, w *app.World) {
	id, ok := w.UserClub()
	if !ok {
		return
	}
	for _, c := range w.Summary().ClubRows {
		if c.ID == id {
			fmt.Fprintf(out, "\nmanaging club %d: %s (%s), team %d\n", c.ID, c.Name, c.ShortName, c.SeniorTeam)
		}
	}
}

// lineupNote describes who picked the managed club's lineup for fixture.
func lineupNote(w *app.World, fixture ids.FixtureID) string {
	if l, ok := w.SubmittedLineup(fixture); ok {
		return fmt.Sprintf("your lineup, %s", l.Tactics.Mentality)
	}
	return "AI lineup"
}

// printStatus shows a world's position without advancing it.
func printStatus(out io.Writer, w *app.World) error {
	cal := w.Calendar()
	fmt.Fprintf(out, "\nStatus: now %s (t=%d), revision %d\n", cal.Format(w.Now()), w.Now(), w.Revision())
	if r, ok := w.Pending(); ok {
		for _, rr := range r.Rounds {
			fmt.Fprintf(out, "pending: competition %d season %d round %d, kicked off %s, %d fixtures awaiting results\n",
				rr.Round.Season.Competition, rr.Round.Season.Season, rr.Round.Round, cal.Format(rr.Kickoff), len(rr.Fixtures))
		}
		for _, f := range r.UserFixtures {
			fmt.Fprintf(out, "your fixture: F%d, %s\n", f, lineupNote(w, f))
		}
	} else {
		fmt.Fprintln(out, "pending: none")
	}
	for _, h := range w.History() {
		if h.Champion != nil {
			fmt.Fprintf(out, "champion: %s season %d: %s (%s)\n", h.CompetitionName, h.Season.Season, h.Champion.ClubName, h.Champion.ShortName)
		}
	}
	for _, t := range w.Tables() {
		if err := printTable(out, t); err != nil {
			return err
		}
	}
	if err := printSquad(out, w); err != nil {
		return err
	}
	return printInbox(out, w, 10)
}

// printInbox lists the newest n inbox messages, oldest first.
func printInbox(out io.Writer, w *app.World, n int) error {
	items := w.Inbox()
	if len(items) == 0 {
		return nil
	}
	cal := w.Calendar()
	fmt.Fprintf(out, "\nInbox (latest %d of %d)\n", min(n, len(items)), len(items))
	for _, m := range items[max(len(items)-n, 0):] {
		venue := "away"
		if m.Home {
			venue = "home"
		}
		fmt.Fprintf(out, "  %s  ", cal.Format(m.At))
		switch m.Kind {
		case inbox.KindMatchday:
			fmt.Fprintf(out, "matchday: %s round %d, F%d v %s (%s)\n",
				m.CompetitionName, m.Round, m.Fixture, m.OpponentLabel.ClubName, venue)
		case inbox.KindResult:
			fmt.Fprintf(out, "result: F%d %d-%d v %s (%s)\n", m.Fixture, m.Goals[0], m.Goals[1], m.OpponentLabel.ClubName, venue)
		case inbox.KindSeasonEnded:
			fmt.Fprintf(out, "%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
			if m.Position > 0 {
				fmt.Fprintf(out, "; your position: %d", m.Position)
			}
			fmt.Fprintln(out)
		case inbox.KindSeasonStarted:
			fmt.Fprintf(out, "%s season %d scheduled: first kickoff %s\n", m.CompetitionName, m.Season, cal.Format(m.Kickoff))
		}
	}
	return nil
}

// printSquad lists the managed club's squad with condition, if any.
func printSquad(out io.Writer, w *app.World) error {
	club, ok := w.UserClub()
	if !ok {
		return nil
	}
	squad, _ := w.Squad(club)
	fmt.Fprintf(out, "\nSquad (condition: 100%% is fully fit)\n")
	fmt.Fprintf(out, "%4s  %-3s %-24s %5s %6s\n", "ID", "POS", "NAME", "OVR", "COND")
	for _, p := range squad {
		_, err := fmt.Fprintf(out, "%4d  %-3s %-24s %5d %5d%%\n", p.Player, p.Position, p.Name,
			p.Overall, p.Condition)
		if err != nil {
			return err
		}
	}
	return nil
}

func printSummary(out io.Writer, s app.Summary) error {
	fmt.Fprintf(out, "world seed=%d generator=v%d random=v%d content=v%d\n",
		s.Seed, s.GeneratorVersion, s.RandomVersion, s.ContentVersion)
	fmt.Fprintf(out, "fingerprint %s\n", s.Fingerprint)
	fmt.Fprintf(out, "clubs=%d teams=%d players=%d\n\n", s.Clubs, s.Teams, s.Players)

	fmt.Fprintf(out, "%2s  %-3s  %-22s %4s %7s", "ID", "ABB", "CLUB", "TEAM", "PLAYERS")
	for _, p := range players.Positions() {
		fmt.Fprintf(out, " %3s", p)
	}
	fmt.Fprintf(out, " %5s\n", "OVR")
	for _, c := range s.ClubRows {
		fmt.Fprintf(out, "%2d  %-3s  %-22s %4d %7d", c.ID, c.ShortName, c.Name, c.SeniorTeam, c.Players)
		for _, pc := range c.Positions {
			fmt.Fprintf(out, " %3d", pc.Count)
		}
		_, err := fmt.Fprintf(out, " %5d\n", c.AverageOverall)
		if err != nil {
			return err
		}
	}
	return nil
}

func printSchedule(out io.Writer, cal sim.Calendar, s app.Schedule) error {
	fixtures := 0
	for _, r := range s.Rounds {
		fixtures += len(r.Fixtures)
	}
	fmt.Fprintf(out, "\n%s (competition %d, season %d, schedule=v%d): %d teams, %d rounds, %d fixtures\n",
		s.CompetitionName, s.Competition, s.Season, s.ScheduleVersion, s.Entrants, len(s.Rounds), fixtures)
	fmt.Fprintf(out, "career epoch %s (t=0); times are logical minutes since the epoch\n", cal.Format(0))
	for _, r := range s.Rounds {
		fmt.Fprintf(out, "\nRound %-2d %s  t=%d  %s\n", r.Round, cal.Format(r.Kickoff), r.Kickoff, r.Status)
		for _, f := range r.Fixtures {
			_, err := fmt.Fprintf(out, "  F%-3d %-3s v %-3s  %-22s v %s\n",
				f.ID, f.Home.ShortName, f.Away.ShortName, f.Home.ClubName, f.Away.ClubName)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// demoContinue advances to just before the first kickoff, then past it, then
// repeats the call to show that a pending round holds the world in place.
func demoContinue(out io.Writer, w *app.World) error {
	cal := w.Calendar()
	rounds := w.Schedules()[0].Rounds
	if len(rounds) == 0 {
		return errors.New("no rounds scheduled")
	}
	kickoff := rounds[0].Kickoff
	fmt.Fprintf(out, "\nContinue (now %s)\n", cal.Format(w.Now()))
	for _, target := range []sim.GameInstant{
		kickoff - sim.GameInstant(sim.Day),
		kickoff + sim.GameInstant(4*sim.Week),
		kickoff + sim.GameInstant(4*sim.Week),
	} {
		res, err := w.Continue(target)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "  to %s: ", cal.Format(target))
		switch r := res.(type) {
		case app.ReachedTarget:
			fmt.Fprintf(out, "reached target\n")
		case app.FixtureRoundReady:
			fmt.Fprintf(out, "fixture round ready, now %s\n", cal.Format(r.At))
			for _, rr := range r.Rounds {
				fmt.Fprintf(out, "    competition %d season %d round %d kicked off %s, %d fixtures awaiting results\n",
					rr.Round.Season.Competition, rr.Round.Season.Season, rr.Round.Round, cal.Format(rr.Kickoff), len(rr.Fixtures))
			}
		default:
			return fmt.Errorf("unexpected continue result %T", res)
		}
	}
	counts := map[string]int{}
	for _, r := range w.Schedules()[0].Rounds {
		counts[r.Status.String()]++
	}
	_, err := fmt.Fprintf(out, "calendar now: %d awaiting results, %d scheduled\n",
		counts["awaiting results"], counts["scheduled"])
	return err
}

// playRounds alternates Continue and ResolveRounds within the leagues'
// current seasons, resolving at most limit batches (all remaining if
// limit < 0), printing each, then prints those seasons' tables. It stops a
// day after their last kickoff, past the season end that creates the next
// seasons, so a later run plays the next season. With a mentality, it first submits the suggested lineup with that
// mentality for each of the managed club's fixtures. Command IDs continue
// after any recorded in the world, so a loaded career never reuses one.
func playRounds(out io.Writer, w *app.World, limit int, mentality matches.Mentality) error {
	cal := w.Calendar()
	end := w.Now()
	var seasons []competitions.SeasonRef
	for _, s := range w.Schedules() {
		end = max(end, s.Rounds[len(s.Rounds)-1].Kickoff+sim.GameInstant(sim.Day))
		seasons = append(seasons, competitions.SeasonRef{Competition: s.Competition, Season: s.Season})
	}
	fmt.Fprintf(out, "\nPlaying to %s\n", cal.Format(end))
	for played := 0; limit < 0 || played < limit; played++ {
		res, err := w.Continue(end)
		if err != nil {
			return err
		}
		ready, ok := res.(app.FixtureRoundReady)
		if !ok {
			break
		}
		if mentality != 0 {
			for _, f := range ready.UserFixtures {
				l, err := w.SuggestLineup(f)
				if err != nil {
					return err
				}
				l.Tactics.Mentality = mentality
				sub := app.SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: f, Lineup: l}
				if _, err := w.SubmitLineup(sub); err != nil {
					return err
				}
			}
		}
		cmd := app.ResolveRounds{ID: w.NextCommandID(), ExpectedRevision: w.Revision()}
		for _, r := range ready.Rounds {
			cmd.Rounds = append(cmd.Rounds, r.Round)
		}
		resolved, err := w.ResolveRounds(cmd)
		if err != nil {
			return err
		}
		for _, r := range resolved.Rounds {
			fmt.Fprintf(out, "\nRound %d  %s  (competition %d)\n", r.Round, cal.Format(resolved.At), r.Season.Competition)
			for _, m := range resolved.Matches {
				if m.Round != r {
					continue
				}
				fmt.Fprintf(out, "  F%-3d %-3s %d-%d %-3s  %s %d-%d %s", m.Fixture,
					m.Home.ShortName, m.Score[0], m.Score[1], m.Away.ShortName,
					m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName)
				if club, ok := w.UserClub(); ok && (m.Home.Club == club || m.Away.Club == club) {
					fmt.Fprintf(out, "  <- %s", lineupNote(w, m.Fixture))
				}
				fmt.Fprintln(out)
			}
		}
	}
	for _, ref := range seasons {
		t, _ := w.Table(ref)
		if err := printTable(out, t); err != nil {
			return err
		}
	}
	return nil
}

func printTable(out io.Writer, t app.Table) error {
	title := "Table"
	if t.Complete {
		title = "Final table"
	}
	fmt.Fprintf(out, "\n%s: %s season %d (%d/%d rounds)\n", title, t.CompetitionName, t.Season, t.RoundsCompleted, t.Rounds)
	fmt.Fprintf(out, "%3s  %-3s  %-22s %3s %3s %3s %3s %4s %4s %4s %4s\n", "POS", "ABB", "CLUB", "P", "W", "D", "L", "GF", "GA", "GD", "PTS")
	for _, r := range t.Rows {
		fmt.Fprintf(out, "%3d  %-3s  %-22s %3d %3d %3d %3d %4d %4d %+4d %4d\n", r.Rank, r.Label.ShortName, r.Label.ClubName,
			r.Played, r.Won, r.Drawn, r.Lost, r.GoalsFor, r.GoalsAgainst, r.GoalDifference(), r.Points)
	}
	if t.Complete && len(t.Rows) > 0 {
		fmt.Fprintf(out, "Champion: %s (%s)\n", t.Rows[0].Label.ClubName, t.Rows[0].Label.ShortName)
	}
	return nil
}
