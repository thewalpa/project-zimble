package main

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// --- league -----------------------------------------------------------------

// leagueTable is one league's standings with the promotion and relegation
// places marked.
type leagueTable struct {
	app.Table
	Marked    []markedRow
	CupPlaces []app.CupQualifier
	Legend    string // what the marks mean; empty when the league has none
}

type markedRow struct {
	app.TableRow
	Mark string // "up", "down" or ""
}

type tableView struct {
	Tables []leagueTable // the club's league first
	Club   ids.ClubID
	Sort   SortState
}

// promotionMarks marks each place by what it means for next season (see
// app.TableMark): "po-up" or "po-down" for a play-off place, "up" or "down"
// once the play-offs have moved the team. The legend explains the marks the
// table uses.
func (s *server) promotionMarks(t app.Table) (mark func(rank int) string, legend string) {
	ref := competitions.SeasonRef{Competition: t.Competition, Season: t.Season}
	marks := map[int]string{}
	used := map[string]bool{}
	for _, row := range t.Rows {
		m := ""
		switch s.w.TableMark(ref, row.Rank) {
		case app.MarkPlayoffUp:
			m = "po-up"
		case app.MarkPlayoffDown:
			m = "po-down"
		case app.MarkPromoted:
			m = "up"
		case app.MarkRelegated:
			m = "down"
		}
		marks[row.Rank], used[m] = m, true
	}
	up, down := s.w.PromotionPlaces(t.Competition)
	var parts []string
	if up > 0 {
		parts = append(parts, fmt.Sprintf("△ the top %d play off for promotion", up))
	}
	if down > 0 {
		parts = append(parts, fmt.Sprintf("▽ the bottom %d play off to stay up", down))
	}
	if used["up"] {
		parts = append(parts, "▲ promoted")
	}
	if used["down"] {
		parts = append(parts, "▼ relegated")
	}
	return func(rank int) string { return marks[rank] }, strings.Join(parts, " · ")
}

func (s *server) table(r *http.Request) (string, any, error) {
	sc, ok := s.userSchedule()
	if !ok {
		return "", nil, errors.New("your club has no season")
	}
	sortState := newSortState(r, "rank", "asc")
	v := tableView{Club: s.club(), Sort: sortState}
	for _, t := range s.w.Tables() {
		if t.RoundsCompleted == 0 && t.Season > 1 {
			// The off-season: last season's final table.
			t, _ = s.w.Table(competitions.SeasonRef{Competition: t.Competition, Season: t.Season - 1})
		}
		t.Rows = append([]app.TableRow(nil), t.Rows...)
		sortTableRows(t.Rows, sortState.Col, sortState.Dir)
		mark, legend := s.promotionMarks(t)
		lt := leagueTable{Table: t, Legend: legend, CupPlaces: s.w.CupQualifiers(t.Competition)}
		for _, row := range t.Rows {
			lt.Marked = append(lt.Marked, markedRow{TableRow: row, Mark: mark(row.Rank)})
		}
		if t.Competition == sc.Competition {
			v.Tables = append([]leagueTable{lt}, v.Tables...)
		} else {
			v.Tables = append(v.Tables, lt)
		}
	}
	return "table", v, nil
}

type fixtureRow struct {
	Fixture      ids.FixtureID
	Name         string // e.g. "Round 3", "Continental Cup final"
	Round        int
	When         string
	Opponent     string
	OpponentClub ids.ClubID
	Played       bool
	Result       string
	Outcome      string
	at           sim.GameInstant // When, for sorting
}

type fixturesView struct {
	Club   app.TeamLabel
	Clubs  []clubOption
	Season string
	Rows   []fixtureRow
	Sort   SortState
}

func (s *server) fixtures(r *http.Request) (string, any, error) {
	viewClub := s.club()
	if cStr := r.URL.Query().Get("club"); cStr != "" {
		if n, err := strconv.ParseUint(cStr, 10, 64); err == nil && n != 0 {
			if _, ok := s.w.ClubLabel(ids.ClubID(n)); ok {
				viewClub = ids.ClubID(n)
			}
		}
	}
	sc, ok := s.scheduleOf(viewClub)
	if !ok {
		return "", nil, errors.New("the club has no season")
	}
	clubLabel, _ := s.w.ClubLabel(viewClub)
	sortState := newSortState(r, "round", "asc")
	v := fixturesView{
		Club:   clubLabel,
		Season: fmt.Sprintf("%s season %d", sc.CompetitionName, sc.Season),
		Sort:   sortState,
	}
	for _, c := range s.w.Summary().ClubRows {
		v.Clubs = append(v.Clubs, clubOption{
			ID:        c.ID,
			Name:      c.Name,
			ShortName: c.ShortName,
			Selected:  c.ID == viewClub,
			IsUser:    c.ID == s.club(),
		})
	}
	for _, rd := range sc.Rounds {
		for _, f := range rd.Fixtures {
			if f.Home.Club != viewClub && f.Away.Club != viewClub {
				continue
			}
			opp, oppClub := s.clubOpponent(f, viewClub)
			row := fixtureRow{
				Fixture:      f.ID,
				Name:         fmt.Sprintf("Round %d", rd.Round),
				Round:        int(rd.Round),
				When:         s.w.Calendar().Format(rd.Kickoff),
				at:           rd.Kickoff,
				Opponent:     opp,
				OpponentClub: oppClub,
				Played:       f.Played,
			}
			if f.Played {
				row.Result = fmt.Sprintf("%d-%d", f.Score[0], f.Score[1])
				row.Outcome = present.Outcome(f.Score, f.Shootout, f.Home.Club == viewClub)
			}
			v.Rows = append(v.Rows, row)
		}
	}
	for _, c := range s.w.Cups() {
		for _, rd := range c.Rounds {
			for _, f := range rd.Ties {
				if f.Home.Club != viewClub && f.Away.Club != viewClub {
					continue
				}
				opp, oppClub := s.clubOpponent(f, viewClub)
				row := fixtureRow{
					Fixture: f.ID, Name: c.Name + " " + rd.Name, Round: int(rd.Round), When: s.w.Calendar().Format(rd.Kickoff), at: rd.Kickoff,
					Opponent: opp, OpponentClub: oppClub, Played: f.Played,
				}
				if f.Played {
					row.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], present.Penalties(f.Shootout))
					row.Outcome = present.Outcome(f.Score, f.Shootout, f.Home.Club == viewClub)
				}
				v.Rows = append(v.Rows, row)
			}
		}
	}
	for _, c := range s.w.Playoffs() {
		for _, rd := range c.Rounds {
			for _, f := range rd.Ties {
				if f.Home.Club != viewClub && f.Away.Club != viewClub {
					continue
				}
				opp, oppClub := s.clubOpponent(f, viewClub)
				row := fixtureRow{
					Fixture: f.ID, Name: c.Name, Round: int(rd.Round), When: s.w.Calendar().Format(rd.Kickoff), at: rd.Kickoff,
					Opponent: opp, OpponentClub: oppClub, Played: f.Played,
				}
				if f.Played {
					row.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], present.Penalties(f.Shootout))
					row.Outcome = present.Outcome(f.Score, f.Shootout, f.Home.Club == viewClub)
				}
				v.Rows = append(v.Rows, row)
			}
		}
	}
	sortFixtureRows(v.Rows, sortState.Col, sortState.Dir)
	return "fixtures", v, nil
}

// --- cup -----------------------------------------------------------------

type cupRoundView struct {
	Title string // e.g. "Quarter-finals", "Final"
	When  string
	Ties  []cupTieView
}

type cupTieView struct {
	Fixture  ids.FixtureID
	Home     app.TeamLabel
	Away     app.TeamLabel
	Played   bool
	Result   string // e.g. "1-1 (4-3 on penalties)"
	Mine     bool   // the user club plays
	HomeWins bool
	AwayWins bool
}

type cupView struct {
	Name     string
	Edition  int
	Rounds   []cupRoundView
	Champion *app.TeamLabel
	Mine     bool // the user club is in this edition
	Sort     SortState
}

// cup shows the latest edition of each cup: rounds, ties, results and the
// winner. Before the first edition it explains how teams qualify.
func (s *server) cup(r *http.Request) (string, any, error) {
	sortState := newSortState(r, "fixture", "asc")
	var out []cupView
	for _, c := range s.w.Cups() {
		out = append(out, s.cupView(c, sortState))
	}
	return "cup", out, nil
}

func (s *server) cupView(c app.CupEdition, sortState SortState) cupView {
	v := cupView{Name: c.Name, Edition: int(c.Edition), Champion: c.Champion, Sort: sortState}
	for _, e := range c.Entrants {
		v.Mine = v.Mine || e.Club == s.club()
	}
	for _, rRound := range c.Rounds {
		title := present.Capitalize(rRound.Name)
		if rRound.Name != "final" {
			title += "s"
		}
		rv := cupRoundView{Title: title, When: s.w.Calendar().Format(rRound.Kickoff)}
		for _, f := range rRound.Ties {
			t := cupTieView{Fixture: f.ID, Home: f.Home, Away: f.Away, Played: f.Played, Mine: f.Home.Club == s.club() || f.Away.Club == s.club()}
			if f.Played {
				t.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], present.Penalties(f.Shootout))
				home := present.Outcome(f.Score, f.Shootout, true) == "W"
				t.HomeWins, t.AwayWins = home, !home
			}
			rv.Ties = append(rv.Ties, t)
		}
		sortCupTies(rv.Ties, sortState.Col, sortState.Dir)
		v.Rounds = append(v.Rounds, rv)
	}
	return v
}

// --- promotion play-offs ------------------------------------------------------

type playoffView struct {
	Title        string // with the divisions
	Edition      int
	Upper, Lower string
	Complete     bool
	Ties         cupRoundView
	Mine         bool // the user club plays a tie
}

// playoffs shows the latest edition of each promotion play-off.
func (s *server) playoffs(r *http.Request) (string, any, error) {
	var out []playoffView
	for _, p := range s.w.Playoffs() {
		out = append(out, s.playoffView(p))
	}
	return "playoffs", out, nil
}

func (s *server) playoffView(p app.PlayoffEdition) playoffView {
	v := playoffView{Title: s.w.PlayoffTitle(p.Competition), Edition: int(p.Edition), Complete: p.Complete}
	v.Upper, v.Lower, _ = s.w.PlayoffDivisions(p.Competition)
	for _, r := range p.Rounds {
		v.Ties = cupRoundView{Title: "Ties", When: s.w.Calendar().Format(r.Kickoff)}
		for _, f := range r.Ties {
			t := cupTieView{Fixture: f.ID, Home: f.Home, Away: f.Away, Played: f.Played, Mine: f.Home.Club == s.club() || f.Away.Club == s.club()}
			if f.Played {
				t.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], present.Penalties(f.Shootout))
				home := present.Outcome(f.Score, f.Shootout, true) == "W"
				t.HomeWins, t.AwayWins = home, !home
			}
			v.Mine = v.Mine || t.Mine
			v.Ties.Ties = append(v.Ties.Ties, t)
		}
	}
	return v
}

// --- history ---------------------------------------------------------------

type historyRow struct {
	Competition ids.CompetitionID
	Season      int
	Name        string
	Complete    bool
	Champion    *app.TeamLabel
	Mine        bool // the user club won it
	Playoff     bool // a promotion play-off: no champion
}

type historyView struct {
	Rows []historyRow
	// Detail is set when one season is chosen: a league's table or a cup's
	// bracket.
	Title   string
	Table   *leagueTable
	Cup     *cupView
	Playoff *playoffView
	Club    ids.ClubID
}

// history lists every league season and cup edition with its champion, and
// shows one season's final table or bracket on request.
func (s *server) history(r *http.Request) (string, any, error) {
	v := historyView{Club: s.club()}
	for _, h := range s.w.History() {
		row := historyRow{Competition: h.Season.Competition, Season: int(h.Season.Season), Name: h.CompetitionName, Complete: h.Complete, Champion: h.Champion}
		if title := s.w.PlayoffTitle(h.Season.Competition); title != "" {
			row.Name, row.Playoff = title, true
		}
		row.Mine = h.Champion != nil && h.Champion.Club == s.club()
		v.Rows = append(v.Rows, row)
	}
	slices.SortStableFunc(v.Rows, func(a, b historyRow) int { return cmp.Compare(b.Season, a.Season) })
	q := r.URL.Query()
	comp, err1 := strconv.ParseUint(q.Get("competition"), 10, 64)
	season, err2 := strconv.ParseUint(q.Get("season"), 10, 16)
	if err1 != nil || err2 != nil {
		return "history", v, nil
	}
	ref := competitions.SeasonRef{Competition: ids.CompetitionID(comp), Season: competitions.Season(season)}
	for _, h := range s.w.History() {
		if h.Season != ref {
			continue
		}
		v.Title = fmt.Sprintf("%s season %d", h.CompetitionName, ref.Season)
		if p, ok := s.w.Playoff(ref); ok {
			pv := s.playoffView(p)
			v.Title, v.Playoff = fmt.Sprintf("%s season %d", pv.Title, p.Edition), &pv
		} else if h.Format == competitions.FormatKnockout {
			if c, ok := s.w.Cup(ref); ok {
				cv := s.cupView(c, newSortState(r, "fixture", "asc"))
				v.Title = fmt.Sprintf("%s %d", c.Name, c.Edition)
				v.Cup = &cv
			}
		} else if t, ok := s.w.Table(ref); ok {
			mark, legend := s.promotionMarks(t)
			lt := leagueTable{Table: t, Legend: legend, CupPlaces: s.w.CupQualifiers(t.Competition)}
			for _, row := range t.Rows {
				lt.Marked = append(lt.Marked, markedRow{TableRow: row, Mark: mark(row.Rank)})
			}
			v.Table = &lt
		}
		return "history", v, nil
	}
	return "", nil, errors.New("no such season")
}
