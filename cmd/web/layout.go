package main

import (
	"fmt"
	"html/template"
	"path/filepath"
	"slices"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/players"
)

var funcs = template.FuncMap{
	// units renders money as whole units for a form field.
	"units": func(m money.Money) int64 { return int64(m) / money.MinorPerUnit },
	// sortHeader renders a sortable column header link with indicator.
	"sortHeader": func(s SortState, col, label, defaultDir string) template.HTML {
		return s.Header(col, label, defaultDir)
	},
	// ratings returns every attribute, in the order of the tables' columns.
	"ratings": func(a players.Attributes) []players.Rating { return a[:] },
}

// layout is what every page gets: the career header, notes and the page's
// own data.
type layout struct {
	Page      string
	Career    bool
	Club      app.TeamLabel
	Date      string
	Balance   money.Money
	Rev       app.Revision
	Unsaved   bool
	Pending   bool // a matchday of the club is waiting
	Notes     []note
	Automatic []app.AutomaticBatch
	Offers    []recoverOffer
	Data      any
	SaveName  string
	SavesDir  string
	Unread    int // unread inbox messages
}

func (s *server) layout(page string, data any) layout {
	l := layout{
		Page:      page,
		Notes:     s.notes,
		Automatic: s.automatic,
		Offers:    s.offers,
		Data:      data,
		SaveName:  filepath.Base(s.savePath),
		SavesDir:  filepath.Clean(s.savesDir),
	}
	if s.w == nil {
		return l
	}
	l.Career, l.Club, l.Date, l.Rev = true, s.clubLabel(), s.w.Calendar().Format(s.w.Now()), s.w.Revision()
	l.Unsaved = !s.saved || s.w.Revision() != s.savedRevision
	_, l.Pending = s.pendingFixture()
	l.Unread = s.w.UnreadInboxCount()
	if fin, ok := s.w.Finances(s.club()); ok {
		l.Balance = fin.Balance
	}
	return l
}

// --- queries shared by pages and actions -------------------------------------

func (s *server) clubLabel() app.TeamLabel {
	if sc, ok := s.userSchedule(); ok {
		for _, f := range sc.Rounds[0].Fixtures {
			if f.Home.Club == s.club() {
				return f.Home
			}
			if f.Away.Club == s.club() {
				return f.Away
			}
		}
	}
	return app.TeamLabel{Club: s.club()}
}

// userSchedule is the current season of the club's league.
func (s *server) userSchedule() (app.Schedule, bool) { return s.scheduleOf(s.club()) }

func (s *server) seasonDone(sc app.Schedule) bool {
	t, _ := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season})
	return t.Complete
}

// fixtureInfo describes a fixture of any league season or cup edition.
func (s *server) fixtureInfo(id ids.FixtureID) (app.FixtureInfo, bool) { return s.w.FixtureInfo(id) }

// scheduleOf is the current season of a club's league.
func (s *server) scheduleOf(club ids.ClubID) (app.Schedule, bool) {
	for _, sc := range s.w.Schedules() {
		for _, f := range sc.Rounds[0].Fixtures {
			if f.Home.Club == club || f.Away.Club == club {
				return sc, true
			}
		}
	}
	return app.Schedule{}, false
}

// clubOpponent names the club's opponent in a fixture and the venue, plus opponent club ID.
func (s *server) clubOpponent(f app.FixtureLine, club ids.ClubID) (string, ids.ClubID) {
	if f.Home.Club == club {
		return f.Away.ClubName + " (home)", f.Away.Club
	}
	return f.Home.ClubName + " (away)", f.Home.Club
}

// opponent names the user club's opponent in a fixture and the venue.
func (s *server) opponent(f app.FixtureLine) string {
	opp, _ := s.clubOpponent(f, s.club())
	return opp
}

func (s *server) pendingFixture() (ids.FixtureID, bool) {
	ready, ok := s.w.Pending()
	if !ok || len(ready.UserFixtures) == 0 {
		return 0, false
	}
	return ready.UserFixtures[0], true
}

// expiring returns the squad's players whose contracts end at the next
// contract-year end.
func (s *server) expiring() []app.SquadPlayer {
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	return slices.DeleteFunc(squad, func(p app.SquadPlayer) bool { return p.Contract.Expires != s.w.ContractYearEnd() })
}

// name is a player's name, from the squad or the free agents.
func (s *server) name(id ids.PlayerID) string {
	if n, ok := s.w.PlayerName(id); ok {
		return n
	}
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	for _, p := range append(squad, s.observedFreeAgents()...) {
		if p.Player == id {
			return p.Name
		}
	}
	return fmt.Sprintf("player %d", id)
}

// Player pages run only after a managed club has been chosen. Use that club
// for every observation, including other clubs' players and market candidates.
func (s *server) observedFreeAgents() []app.SquadPlayer {
	rows, _ := s.w.ObservedFreeAgents(s.club())
	return rows
}

func (s *server) transferList() []app.ListedPlayer {
	rows, _ := s.w.ObservedTransferList(s.club())
	return rows
}
