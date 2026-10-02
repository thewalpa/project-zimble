package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/storage"
)

// --- home ---------------------------------------------------------------------

type fixtureView struct {
	Name         string // e.g. "Round 3", "Continental Cup semi-final"
	Round        int
	Opponent     string
	When         string
	LineupSource string
	DroppedNotes []string
	kickoff      sim.GameInstant
}

// agendaRow is one thing the manager has ahead, linked to where to act on it.
type agendaRow struct {
	Text string
	Now  bool
	Href string
}

// agenda turns app.Agenda into rows. The wording comes from app; the client
// only picks the page each kind is handled on.
func (s *server) agenda() []agendaRow {
	var rows []agendaRow
	for _, it := range s.w.Agenda() {
		href := "/transfers"
		switch it.Kind {
		case app.AgendaMatchday:
			href = "/lineup"
		case app.AgendaFixture:
			href = "/fixtures"
		case app.AgendaContract:
			href = fmt.Sprintf("/player?id=%d", it.Player)
		}
		rows = append(rows, agendaRow{Text: it.Text, Now: it.Now, Href: href})
	}
	return rows
}

type otherResult struct {
	Fixture ids.FixtureID
	Title   string
}

type matchReport struct {
	Fixture ids.FixtureID
	Title   string
	Outcome string
	Goals   []string
	Others  []otherResult
}

type homeView struct {
	Season      string
	Played      int
	Rounds      int
	Position    int
	WeeklyWage  money.Money
	Expiring    int
	ContractEnd string
	Review      bool   // the league season has every result: its review is open
	Window      string // while a transfer window is open
	Bids        int    // bids for the club's players awaiting an answer
	FreeAgents  int    // players without a club, while a transfer window is open
	Live        string
	Agenda      []agendaRow
	Matchday    *fixtureView
	Next        *fixtureView
	Report      *matchReport
	Inbox       []messageView
	SaveName    string
	Engine      string // the career's match engine and its version
	OtherSaves  []saveInfo
	Damaged     *saveInfo // the active file, when it cannot be loaded but has a previous save
}

type chooseClubRow struct {
	app.ClubSummary
	League      string
	leagueOrder ids.CompetitionID
}

type chooseView struct {
	Seed    uint64
	Engine  string   // match engine the new career will be played on
	Engines []string // the choices, the default first
	Clubs   []chooseClubRow
	Sort    SortState
	Saves   []saveInfo
}

func (s *server) home(r *http.Request) (string, any, error) {
	if s.w == nil {
		preview, err := app.NewWorld(app.DefaultConfig(s.seed))
		if err != nil {
			return "", nil, err
		}
		sortState := newSortState(r, "league", "asc")
		leagueOf := map[ids.ClubID]app.Table{}
		for _, t := range preview.Tables() {
			for _, row := range t.Rows {
				leagueOf[row.Label.Club] = t
			}
		}
		var clubs []chooseClubRow
		for _, c := range preview.Summary().ClubRows {
			t := leagueOf[c.ID]
			clubs = append(clubs, chooseClubRow{ClubSummary: c, League: t.CompetitionName, leagueOrder: t.Competition})
		}
		sortChooseRows(clubs, sortState.Col, sortState.Dir)
		engine := s.engineID()
		if e := r.URL.Query().Get("engine"); slices.Contains(app.Engines(), e) {
			engine = e
		}
		return "choose", chooseView{
			Seed:    uint64(s.seed),
			Engine:  engine,
			Engines: app.Engines(),
			Clubs:   clubs,
			Sort:    sortState,
			Saves:   s.listSaves(),
		}, nil
	}
	cal := s.w.Calendar()
	v := homeView{Report: s.report, Agenda: s.agenda(), ContractEnd: cal.Format(s.w.ContractYearEnd()), Expiring: len(s.expiring())}
	_, v.Review, _ = s.w.SeasonReview()
	if s.w.TransferWindow().Open {
		v.Window, v.Bids, v.FreeAgents = s.windowText(), s.openBidsForUs(), len(s.observedFreeAgents())
	}
	if fin, ok := s.w.Finances(s.club()); ok {
		v.WeeklyWage = fin.WeeklyWage
	}
	if sc, ok := s.userSchedule(); ok {
		v.Season, v.Rounds = fmt.Sprintf("%s season %d", sc.CompetitionName, sc.Season), len(sc.Rounds)
		for _, r := range sc.Rounds {
			if r.Status == competitions.RoundCompleted {
				v.Played++
			}
		}
		if t, ok := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season}); ok && t.RoundsCompleted > 0 {
			for _, row := range t.Rows {
				if row.Label.Club == s.club() {
					v.Position = row.Rank
				}
			}
		}
	}
	v.Matchday, v.Next = s.upcoming()
	if v.Matchday != nil {
		if fix, ok := s.pendingFixture(); ok {
			if ml, err := s.w.MatchdayLineup(fix); err == nil {
				v.Matchday.LineupSource = s.w.LineupSourceLabel(ml)
				v.Matchday.DroppedNotes = s.w.LineupDroppedMessages(ml)
			}
		}
	}
	v.Live = s.liveLine()
	msgs := s.messages()
	v.Inbox = msgs[max(len(msgs)-8, 0):]
	slices.Reverse(v.Inbox)
	v.SaveName = filepath.Base(s.savePath)
	e := s.w.MatchEngine()
	v.Engine = fmt.Sprintf("%s v%d", e.ID, e.Version)
	for _, sv := range s.listSaves() {
		switch {
		case filepath.Clean(sv.Path) != filepath.Clean(s.savePath):
			v.OtherSaves = append(v.OtherSaves, sv)
		case sv.Recover != nil:
			v.Damaged = &sv
		}
	}
	return "home", v, nil
}

// upcoming finds the club's matchday waiting for its result and its next
// scheduled match, in its league, a cup or a play-off. A cup tie or a
// play-off may come before the next league match.
func (s *server) upcoming() (matchday, next *fixtureView) {
	cal := s.w.Calendar()
	consider := func(name string, round int, status competitions.RoundStatus, kickoff sim.GameInstant, f app.FixtureLine) {
		if f.Home.Club != s.club() && f.Away.Club != s.club() {
			return
		}
		fv := &fixtureView{Name: name, Round: round, Opponent: s.opponent(f), When: cal.Format(kickoff), kickoff: kickoff}
		switch {
		case status == competitions.RoundAwaitingResults && matchday == nil:
			matchday = fv
		case status == competitions.RoundScheduled && (next == nil || kickoff < next.kickoff):
			next = fv
		}
	}
	if sc, ok := s.userSchedule(); ok {
		for _, r := range sc.Rounds {
			for _, f := range r.Fixtures {
				consider(fmt.Sprintf("Round %d", r.Round), int(r.Round), r.Status, r.Kickoff, f)
			}
		}
	}
	for _, c := range s.w.Cups() {
		for _, r := range c.Rounds {
			for _, f := range r.Ties {
				consider(c.Name+" "+r.Name, int(r.Round), r.Status, r.Kickoff, f)
			}
		}
	}
	for _, p := range s.w.Playoffs() {
		for _, r := range p.Rounds {
			for _, f := range r.Ties {
				consider(p.Name, int(r.Round), r.Status, r.Kickoff, f)
			}
		}
	}
	return matchday, next
}

// liveLine is the manager's match in progress, e.g. "34'  Quillford FC 1-0
// Brackenmoor Town", or "" when none is.
func (s *server) liveLine() string {
	l, live := s.w.LiveMatch()
	if !live {
		return ""
	}
	return fmt.Sprintf("%d'  %s %d-%d %s", l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
}

// liveTeams names the manager's match in progress without its score.
func (s *server) liveTeams() string {
	l, live := s.w.LiveMatch()
	if !live {
		return ""
	}
	return fmt.Sprintf("%s v %s", l.Home.ClubName, l.Away.ClubName)
}

// reportOf summarizes a resolved matchday: the club's match with its
// scorers, then the other results.
func (s *server) reportOf(res app.RoundsResolved) *matchReport {
	r := &matchReport{}
	for _, m := range res.Matches {
		line := fmt.Sprintf("%s %d-%d %s%s", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, present.Penalties(m.Shootout))
		if m.Home.Club != s.club() && m.Away.Club != s.club() {
			r.Others = append(r.Others, otherResult{Fixture: m.Fixture, Title: line})
			continue
		}
		r.Fixture = m.Fixture
		r.Title, r.Outcome = line, present.Outcome(m.Score, m.Shootout, m.Home.Club == s.club())
		for _, g := range m.Goals {
			team := m.Home.ShortName
			if g.Side == matches.Away {
				team = m.Away.ShortName
			}
			pName, _ := s.w.PlayerName(g.Scorer)
			if pName == "" {
				pName = s.name(g.Scorer)
			}
			r.Goals = append(r.Goals, fmt.Sprintf("%d'  %s %s", g.Minute, team, pName))
		}
	}
	if r.Title == "" {
		return nil
	}
	return r
}

type saveInfo struct {
	Name     string
	Path     string
	Club     string
	Date     string
	Season   string
	Modified string
	ModTime  time.Time
	Damaged  string        // why the file cannot be loaded, when a previous save can replace it
	Recover  *recoverOffer // the choice to do so
}

// listSaves inspects available .json save files in savesDir, career.json in root,
// and s.savePath, returning metadata sorted newest first.
func (s *server) listSaves() []saveInfo {
	seen := map[string]bool{}
	var paths []string

	if entries, err := os.ReadDir(s.savesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				p := filepath.Join(s.savesDir, e.Name())
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
		}
	}

	if !seen["career.json"] {
		if fi, err := os.Stat("career.json"); err == nil && !fi.IsDir() {
			seen["career.json"] = true
			paths = append(paths, "career.json")
		}
	}

	if s.savePath != "" && !seen[s.savePath] {
		if fi, err := os.Stat(s.savePath); err == nil && !fi.IsDir() {
			seen[s.savePath] = true
			paths = append(paths, s.savePath)
		}
	}

	var saves []saveInfo
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		w, err := storage.Load(p)
		if err != nil {
			if o := s.offerFor(p); o != nil {
				saves = append(saves, saveInfo{Name: filepath.Base(p), Path: p, Modified: fi.ModTime().Format("2006-01-02 15:04"), ModTime: fi.ModTime(), Damaged: err.Error(), Recover: o})
			}
			continue
		}
		clubID, ok := w.UserClub()
		if !ok {
			continue
		}
		clubName := fmt.Sprintf("Club %d", clubID)
		for _, cr := range w.Summary().ClubRows {
			if cr.ID == clubID {
				clubName = cr.Name
				break
			}
		}
		date := w.Calendar().Format(w.Now())
		season := "Season 1"
		for _, sc := range w.Schedules() {
			found := false
			for _, r := range sc.Rounds {
				for _, f := range r.Fixtures {
					if f.Home.Club == clubID || f.Away.Club == clubID {
						season = fmt.Sprintf("%s season %d", sc.CompetitionName, sc.Season)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}
		saves = append(saves, saveInfo{
			Name:     filepath.Base(p),
			Path:     p,
			Club:     clubName,
			Date:     date,
			Season:   season,
			Modified: fi.ModTime().Format("2006-01-02 15:04"),
			ModTime:  fi.ModTime(),
		})
	}

	slices.SortFunc(saves, func(a, b saveInfo) int {
		return b.ModTime.Compare(a.ModTime)
	})
	return saves
}
