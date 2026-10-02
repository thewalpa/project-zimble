package main

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
)

// --- club pages -----------------------------------------------------------------
//
// A club has a page, /club/{id}, with a submenu of tabs, /club/{id}/{tab}:
// overview, squad, fixtures, transfers and history. The manager's own club
// adds Lineup, Medical and Finances, which are pages of their own that carry the same
// heading and submenu.

// errNotFound makes a page answer 404.
var errNotFound = errors.New("not found")

// clubTab is one entry of a club page's submenu.
type clubTab struct {
	Label, Href string
	On          bool
}

// clubHead is the heading and submenu of a club's pages.
type clubHead struct {
	ID     ids.ClubID
	Name   string
	Short  string
	Nation string
	Mine   bool
	Tabs   []clubTab
}

// clubHeadOf builds the heading of a club's pages, marking the current tab.
func (s *server) clubHeadOf(club ids.ClubID, current string) (clubHead, bool) {
	c, ok := s.clubRow(club)
	if !ok {
		return clubHead{}, false
	}
	h := clubHead{ID: c.ID, Name: c.Name, Short: c.ShortName, Nation: c.Nation, Mine: c.ID == s.club()}
	base := fmt.Sprintf("/club/%d", c.ID)
	add := func(key, label, href string) {
		h.Tabs = append(h.Tabs, clubTab{Label: label, Href: href, On: key == current})
	}
	add("overview", "Overview", base)
	add("squad", "Squad", base+"/squad")
	if h.Mine {
		add("lineup", "Lineup", "/lineup")
		add("medical", "Medical", "/medical")
	}
	add("fixtures", "Fixtures", base+"/fixtures")
	add("transfers", "Transfers", base+"/transfers")
	add("history", "History", base+"/history")
	if h.Mine {
		add("finances", "Finances", "/finances")
	}
	return h, true
}

type clubView struct {
	Head clubHead
	Tab  string

	Overview  *clubOverview
	Squad     *squadView
	Fixtures  *fixturesView
	Transfers *clubTransfersView
	History   *clubHistoryView
}

// navEntry is the navigation entry a club page belongs to: the manager's own
// club is the Club group, another club is among the Clubs.
func (v clubView) navEntry() string {
	if !v.Head.Mine {
		return "clubs"
	}
	switch v.Tab {
	case "squad", "fixtures":
		return v.Tab
	}
	return "club"
}

// clubPage serves /club/{id} and /club/{id}/{tab}.
func (s *server) clubPage(r *http.Request) (string, any, error) {
	n, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || n == 0 {
		return "", nil, fmt.Errorf("%w: %q is not a club", errNotFound, r.PathValue("id"))
	}
	club := ids.ClubID(n)
	tab := cmp.Or(r.PathValue("tab"), "overview")
	head, ok := s.clubHeadOf(club, tab)
	if !ok {
		return "", nil, fmt.Errorf("%w: there is no club %d", errNotFound, n)
	}
	v := clubView{Head: head, Tab: tab}
	switch tab {
	case "overview":
		o, err := s.overviewOf(r, club)
		if err != nil {
			return "", nil, err
		}
		v.Overview = &o
	case "squad":
		sq := s.squadViewOf(r, club)
		v.Squad = &sq
	case "fixtures":
		f, err := s.fixturesOf(r, club)
		if err != nil {
			return "", nil, err
		}
		v.Fixtures = &f
	case "transfers":
		t := s.clubTransfersOf(club)
		v.Transfers = &t
	case "history":
		h := s.clubHistoryOf(club)
		v.History = &h
	default:
		return "", nil, fmt.Errorf("%w: a club has no %q page", errNotFound, tab)
	}
	return "club", v, nil
}

// redirectToClub sends the old per-club addresses (/squad, /fixtures, with
// or without ?club=) to the club's page, keeping the sort.
func (s *server) redirectToClub(tab string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		club := ids.ClubID(0)
		if s.w != nil {
			club = s.club()
		}
		q := r.URL.Query()
		if n, err := strconv.ParseUint(q.Get("club"), 10, 64); err == nil && n != 0 {
			club = ids.ClubID(n)
		}
		q.Del("club")
		to := "/"
		if club != 0 {
			to = fmt.Sprintf("/club/%d/%s", club, tab)
			if len(q) > 0 {
				to += "?" + q.Encode()
			}
		}
		http.Redirect(rw, r, to, http.StatusSeeOther)
	}
}

// --- overview -----------------------------------------------------------------

type clubOverview struct {
	Standing *clubStanding // nil before the club has a season with results
	Form     []string      // the last results, oldest first: W, D or L
	Last     *fixtureRow
	Next     *fixtureRow
	Squad    clubSquadFacts
	Top      []app.SquadPlayer
	Honours  []string
	Listed   []app.ListedPlayer
	Mine     bool
	Balance  money.Money
	Wages    money.Money
}

type clubStanding struct {
	League                      string
	Season                      int
	Place                       string
	Complete                    bool // the season is over: the final table
	P, W, D, L, GF, GA, GD, Pts int
}

type clubSquadFacts struct {
	Players   int
	Average   int
	Positions string
}

func (s *server) overviewOf(r *http.Request, club ids.ClubID) (clubOverview, error) {
	o := clubOverview{Mine: club == s.club()}
	// The latest league season in which the club has played: the one under
	// way, or the last one in the off-season.
	var seasons = s.w.ClubSeasons(club)
	for _, cs := range slices.Backward(seasons) {
		if cs.League && cs.Standing.Played > 0 {
			st := cs.Standing
			o.Standing = &clubStanding{League: cs.Name, Season: int(cs.Season.Season), Place: app.Ordinal(st.Rank), Complete: cs.Complete,
				P: st.Played, W: st.Won, D: st.Drawn, L: st.Lost, GF: st.GoalsFor, GA: st.GoalsAgainst, GD: st.GoalDifference(), Pts: st.Points}
			break
		}
	}
	for _, cs := range seasons {
		if cs.Champion {
			o.Honours = append(o.Honours, fmt.Sprintf("%s, season %d", honourName(cs), cs.Season.Season))
		}
	}
	slices.Reverse(o.Honours)

	f, err := s.fixturesOf(r, club)
	if err != nil {
		return o, err
	}
	rows := slices.Clone(f.Rows)
	slices.SortStableFunc(rows, func(a, b fixtureRow) int { return cmp.Compare(a.at, b.at) })
	for i := range rows {
		if rows[i].Played {
			o.Last = &rows[i]
			o.Form = append(o.Form, rows[i].Outcome)
		} else if o.Next == nil {
			o.Next = &rows[i]
		}
	}
	o.Form = o.Form[max(0, len(o.Form)-5):]

	if observed, ok := s.w.ObservedClubs(s.club()); ok {
		for _, c := range observed {
			if c.ID != club {
				continue
			}
			o.Squad = clubSquadFacts{Players: c.Players, Average: c.AverageOverall}
			for i, p := range c.Positions {
				if i > 0 {
					o.Squad.Positions += " · "
				}
				o.Squad.Positions += fmt.Sprintf("%d %s", p.Count, p.Position)
			}
		}
	}
	squad, _ := s.w.ObservedSquad(s.club(), club)
	slices.SortStableFunc(squad, func(a, b app.SquadPlayer) int { return cmp.Compare(b.Overall, a.Overall) })
	o.Top = squad[:min(5, len(squad))]
	for _, l := range s.transferList() {
		if l.Club == club {
			o.Listed = append(o.Listed, l)
		}
	}
	if o.Mine {
		if fin, ok := s.w.Finances(club); ok {
			o.Balance, o.Wages = fin.Balance, fin.WeeklyWage
		}
	}
	return o, nil
}

// honourName is the title a champion season won.
func honourName(cs app.ClubSeason) string {
	if cs.League {
		return cs.Name + " champions"
	}
	return cs.Name + " winners"
}

// --- transfers -----------------------------------------------------------------

type clubMoveRow struct {
	When       string
	What       string
	Player     ids.PlayerID
	PlayerName string
	Other      ids.ClubID
	OtherName  string
	Fee        string // a transfer fee, or what a release cost; empty when free
}

type clubTransfersView struct {
	Mine   bool
	Moves  []clubMoveRow // newest first
	Listed []app.ListedPlayer
}

func (s *server) clubTransfersOf(club ids.ClubID) clubTransfersView {
	v := clubTransfersView{Mine: club == s.club()}
	for _, m := range slices.Backward(s.w.ClubMoves(club)) {
		row := clubMoveRow{When: s.w.Calendar().Format(m.At), What: moveText(m.Kind), Player: m.Player, PlayerName: m.PlayerName, Other: m.Other, OtherName: m.OtherName}
		if m.Fee > 0 {
			row.Fee = m.Fee.String()
		}
		v.Moves = append(v.Moves, row)
	}
	for _, l := range s.transferList() {
		if l.Club == club {
			v.Listed = append(v.Listed, l)
		}
	}
	return v
}

func moveText(k app.ClubMoveKind) string {
	switch k {
	case app.MoveBought:
		return "Bought from"
	case app.MoveSold:
		return "Sold to"
	case app.MoveSigned:
		return "Signed as a free agent"
	case app.MoveYouth:
		return "Joined from the youth ranks"
	case app.MoveReleased:
		return "Released"
	case app.MoveExpired:
		return "Contract ran out"
	case app.MoveRetired:
		return "Retired"
	}
	return ""
}

// --- history -----------------------------------------------------------------

type clubSeasonRow struct {
	Competition ids.CompetitionID
	Season      int
	Name        string
	League      bool
	Place       string // league: "3rd"; cup: the round the run ended in
	Record      string // league: "12 pts, 4-3-5, 18-20"
	Note        string // "Champions", "Promoted", ...
	InProgress  bool
}

type clubHistoryView struct {
	Rows    []clubSeasonRow // newest first
	Titles  int
	Cups    int
	Seasons int
}

func (s *server) clubHistoryOf(club ids.ClubID) clubHistoryView {
	var v clubHistoryView
	for _, cs := range slices.Backward(s.w.ClubSeasons(club)) {
		row := clubSeasonRow{Competition: cs.Season.Competition, Season: int(cs.Season.Season), Name: cs.Name, League: cs.League, InProgress: !cs.Complete}
		if cs.League {
			st := cs.Standing
			row.Place = app.Ordinal(st.Rank)
			row.Record = fmt.Sprintf("%d pts · %d-%d-%d · %d-%d", st.Points, st.Won, st.Drawn, st.Lost, st.GoalsFor, st.GoalsAgainst)
			if st.Played == 0 {
				row.Place, row.Record = "", "not started"
			}
		} else {
			row.Place = strings.ToUpper(cs.Stage[:1]) + cs.Stage[1:]
		}
		switch {
		case !cs.Complete:
		case cs.Champion && cs.League:
			row.Note, v.Titles = "Champions", v.Titles+1
		case cs.Champion:
			row.Note, v.Cups = "Winners", v.Cups+1
		case cs.Promoted:
			row.Note = "Promoted"
		case cs.Relegated:
			row.Note = "Relegated"
		}
		if cs.Complete {
			v.Seasons++
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

// --- the directory of clubs -----------------------------------------------------

type clubListRow struct {
	ID      ids.ClubID
	Name    string
	Short   string
	Nation  string
	Place   string
	Played  int
	Points  int
	Players int
	Average int
	Mine    bool
}

type clubListGroup struct {
	League string
	Rows   []clubListRow
}

// clubs lists every club by league, in table order, linking to its page.
func (s *server) clubs(r *http.Request) (string, any, error) {
	observed, _ := s.w.ObservedClubs(s.club())
	byID := map[ids.ClubID]app.ClubSummary{}
	for _, c := range observed {
		byID[c.ID] = c
	}
	var groups []clubListGroup
	for _, t := range s.w.Tables() {
		g := clubListGroup{League: t.CompetitionName}
		for _, row := range t.Rows {
			c := byID[row.Label.Club]
			r := clubListRow{ID: c.ID, Name: c.Name, Short: c.ShortName, Nation: c.Nation, Played: row.Played, Points: row.Points,
				Players: c.Players, Average: c.AverageOverall, Mine: c.ID == s.club()}
			if row.Played > 0 {
				r.Place = app.Ordinal(row.Rank)
			}
			g.Rows = append(g.Rows, r)
		}
		groups = append(groups, g)
	}
	return "clubs", groups, nil
}
