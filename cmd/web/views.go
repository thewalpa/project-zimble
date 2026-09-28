package main

import (
	"cmp"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
)

var funcs = template.FuncMap{
	// units renders money as whole units for a form field.
	"units": func(m money.Money) int64 { return int64(m) / money.MinorPerUnit },
}

// layout is what every page gets: the career header, notes and the page's
// own data.
type layout struct {
	Page    string
	Career  bool
	Club    app.TeamLabel
	Date    string
	Balance money.Money
	Rev     app.Revision
	Unsaved bool
	Pending bool // a matchday of the club is waiting
	Notes   []note
	Data    any
}

func (s *server) layout(page string, data any) layout {
	l := layout{Page: page, Notes: s.notes, Data: data}
	if s.w == nil {
		return l
	}
	l.Career, l.Club, l.Date, l.Rev = true, s.clubLabel(), s.w.Calendar().Format(s.w.Now()), s.w.Revision()
	l.Unsaved = !s.saved || s.w.Revision() != s.savedRevision
	_, l.Pending = s.pendingFixture()
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

// matchName names a fixture's round for display: "Round 3" in a league,
// "Continental Cup quarter-final" in a cup.
func matchName(info app.FixtureInfo) string {
	if info.Cup {
		return info.CompetitionName + " " + info.RoundName
	}
	return capitalize(info.RoundName)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// penalties renders a shootout, e.g. " (4-3 on penalties)", or nothing.
func penalties(p [2]uint16) string {
	if p == [2]uint16{} {
		return ""
	}
	return fmt.Sprintf(" (%d-%d on penalties)", p[0], p[1])
}

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
	squad, _ := s.w.Squad(s.club())
	return slices.DeleteFunc(squad, func(p app.SquadPlayer) bool { return p.Contract.Expires != s.w.ContractYearEnd() })
}

// name is a player's name, from the squad or the free agents.
func (s *server) name(id ids.PlayerID) string {
	if n, ok := s.w.PlayerName(id); ok {
		return n
	}
	squad, _ := s.w.Squad(s.club())
	for _, p := range append(squad, s.w.FreeAgents()...) {
		if p.Player == id {
			return p.Name
		}
	}
	return fmt.Sprintf("player %d", id)
}

// endDate renders a contract end, e.g. "1 July 2029".
func (s *server) endDate(at sim.GameInstant) string {
	c, _ := s.w.Calendar().Civil(at)
	months := [...]string{"January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
	return fmt.Sprintf("%d %s %d", c.Day, months[c.Month-1], c.Year)
}

// outcome is W, D or L for the club in a played fixture; a shootout
// decides a level knockout match.
func outcome(score, shootout [2]uint16, home bool) string {
	if score[0] == score[1] {
		score = shootout
	}
	us, them := score[0], score[1]
	if !home {
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

// --- home ---------------------------------------------------------------------

type fixtureView struct {
	Name     string // e.g. "Round 3", "Continental Cup semi-final"
	Round    int
	Opponent string
	When     string
	kickoff  sim.GameInstant
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

type messageView struct {
	When string
	Text string
}

type homeView struct {
	Season      string
	Played      int
	Rounds      int
	Position    int
	WeeklyWage  money.Money
	Expiring    int
	ContractEnd string
	Window      string // while a transfer window is open
	Bids        int    // bids for the club's players awaiting an answer
	Live        string
	Matchday    *fixtureView
	Next        *fixtureView
	Report      *matchReport
	Inbox       []messageView
}

type chooseView struct {
	Seed  uint64
	Clubs []app.ClubSummary
}

func (s *server) home(*http.Request) (string, any, error) {
	if s.w == nil {
		preview, err := app.NewWorld(app.DefaultConfig(s.seed))
		if err != nil {
			return "", nil, err
		}
		return "choose", chooseView{Seed: uint64(s.seed), Clubs: preview.Summary().ClubRows}, nil
	}
	cal := s.w.Calendar()
	v := homeView{Report: s.report, ContractEnd: cal.Format(s.w.ContractYearEnd()), Expiring: len(s.expiring())}
	if s.w.TransferWindow().Open {
		v.Window, v.Bids = s.windowText(), s.openBidsForUs()
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
			for _, f := range r.Fixtures {
				if f.Home.Club != s.club() && f.Away.Club != s.club() {
					continue
				}
				fv := &fixtureView{Name: fmt.Sprintf("Round %d", r.Round), Round: int(r.Round), Opponent: s.opponent(f), When: cal.Format(r.Kickoff), kickoff: r.Kickoff}
				switch {
				case r.Status == competitions.RoundAwaitingResults && v.Matchday == nil:
					v.Matchday = fv
				case r.Status == competitions.RoundScheduled && v.Next == nil:
					v.Next = fv
				}
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
	// A cup tie may be the matchday, or come before the next league match.
	for _, c := range s.w.Cups() {
		for _, r := range c.Rounds {
			for _, f := range r.Ties {
				if f.Home.Club != s.club() && f.Away.Club != s.club() {
					continue
				}
				fv := &fixtureView{Name: c.Name + " " + r.Name, Round: int(r.Round), Opponent: s.opponent(f), When: cal.Format(r.Kickoff), kickoff: r.Kickoff}
				switch {
				case r.Status == competitions.RoundAwaitingResults && v.Matchday == nil:
					v.Matchday = fv
				case r.Status == competitions.RoundScheduled && (v.Next == nil || r.Kickoff < v.Next.kickoff):
					v.Next = fv
				}
			}
		}
	}
	if l, live := s.w.LiveMatch(); live {
		v.Live = fmt.Sprintf("%d'  %s %d-%d %s", l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
	}
	msgs := s.messages()
	v.Inbox = msgs[max(len(msgs)-8, 0):]
	slices.Reverse(v.Inbox)
	return "home", v, nil
}

// reportOf summarizes a resolved matchday: the club's match with its
// scorers, then the other results.
func (s *server) reportOf(res app.RoundsResolved) *matchReport {
	r := &matchReport{}
	for _, m := range res.Matches {
		line := fmt.Sprintf("%s %d-%d %s%s", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName, penalties(m.Shootout))
		if m.Home.Club != s.club() && m.Away.Club != s.club() {
			r.Others = append(r.Others, otherResult{Fixture: m.Fixture, Title: line})
			continue
		}
		r.Fixture = m.Fixture
		r.Title, r.Outcome = line, outcome(m.Score, m.Shootout, m.Home.Club == s.club())
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

// --- squad and contracts --------------------------------------------------------

type clubOption struct {
	ID        ids.ClubID
	Name      string
	ShortName string
	Selected  bool
	IsUser    bool
}

type squadRow struct {
	app.SquadPlayer
	Ends      int
	FinalYear bool
	Offer     app.ContractOffer // the usual terms, for final-year players
}

type squadView struct {
	Club        app.TeamLabel
	IsUserClub  bool
	Clubs       []clubOption
	Rows        []squadRow
	ContractEnd string
	YearOptions []int
}

func (s *server) squad(r *http.Request) (string, any, error) {
	viewClub := s.club()
	if cStr := r.URL.Query().Get("club"); cStr != "" {
		if n, err := strconv.ParseUint(cStr, 10, 64); err == nil && n != 0 {
			if _, ok := s.w.Squad(ids.ClubID(n)); ok {
				viewClub = ids.ClubID(n)
			}
		}
	}
	squad, _ := s.w.Squad(viewClub)
	clubLabel, _ := s.w.ClubLabel(viewClub)
	isUserClub := (viewClub == s.club())
	end := s.w.ContractYearEnd()
	e := s.w.Content().Economy
	v := squadView{
		Club:        clubLabel,
		IsUserClub:  isUserClub,
		ContractEnd: s.endDate(end),
	}
	for y := e.ContractYears[0]; y <= e.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
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
	for _, p := range squad {
		c, _ := s.w.Calendar().Civil(p.Contract.Expires)
		row := squadRow{SquadPlayer: p, Ends: c.Year, FinalYear: isUserClub && p.Contract.Expires == end}
		if row.FinalYear {
			row.Offer, _ = s.w.SuggestContract(p.Player)
		}
		v.Rows = append(v.Rows, row)
	}
	slices.SortStableFunc(v.Rows, func(a, b squadRow) int { return cmp.Compare(a.Position, b.Position) })
	return "squad", v, nil
}

type freeRow struct {
	app.SquadPlayer
	Offer app.ContractOffer
	Room  bool // the squad has room at the player's position
}

type freeView struct {
	Rows        []freeRow
	Locked      bool // a matchday is waiting: squads cannot change
	YearOptions []int
	RetireAge   int
}

func (s *server) free(*http.Request) (string, any, error) {
	defs := s.w.Content()
	counts := map[players.Position]int{}
	squad, _ := s.w.Squad(s.club())
	for _, p := range squad {
		counts[p.Position]++
	}
	_, locked := s.w.Pending()
	v := freeView{Locked: locked, RetireAge: players.FreeAgentRetirementAge}
	for y := defs.Economy.ContractYears[0]; y <= defs.Economy.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, p := range s.w.FreeAgents() {
		o, _ := s.w.SuggestContract(p.Player)
		v.Rows = append(v.Rows, freeRow{SquadPlayer: p, Offer: o, Room: counts[p.Position] < defs.Quota(p.Position).Count})
	}
	slices.SortStableFunc(v.Rows, func(a, b freeRow) int { return cmp.Compare(b.Overall, a.Overall) })
	return "free", v, nil
}

// --- lineup -----------------------------------------------------------------

type slotOption struct {
	Value string
	Label string
}

var slotOptions = []slotOption{
	{"gk", "Start in goal"}, {"df", "Start in defence"}, {"mf", "Start in midfield"}, {"fw", "Start in attack"},
	{"bench", "Bench"}, {"out", "Not selected"},
}

type lineupRow struct {
	app.SquadPlayer
	Slot string
}

type lineupView struct {
	Fixture     ids.FixtureID
	Title       string
	State       string
	Mentality   string
	Mentalities []string
	Rows        []lineupRow
	Slots       []slotOption
}

func (s *server) lineup(*http.Request) (string, any, error) {
	fixture, ok := s.pendingFixture()
	if !ok {
		return "lineup", lineupView{}, nil
	}
	l, submitted := s.w.SubmittedLineup(fixture)
	state := "Your saved lineup"
	if !submitted {
		var err error
		if l, err = s.w.SuggestLineup(fixture); err != nil {
			return "", nil, err
		}
		state = "The assistant's suggestion (used unless you save changes)"
	}
	info, _ := s.fixtureInfo(fixture)
	v := lineupView{
		Fixture: fixture, State: state, Mentality: l.Tactics.Mentality.String(), Slots: slotOptions,
		Title: fmt.Sprintf("%s v %s, %s", matchName(info), s.opponent(info.FixtureLine), s.w.Calendar().Format(info.Kickoff)),
	}
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		v.Mentalities = append(v.Mentalities, m.String())
	}
	slot := map[ids.PlayerID]string{}
	for _, st := range l.Starters {
		slot[st.Player] = slotName(st.Role)
	}
	for _, p := range l.Bench {
		slot[p] = "bench"
	}
	squad, _ := s.w.Squad(s.club())
	for _, p := range squad {
		row := lineupRow{SquadPlayer: p, Slot: cmp.Or(slot[p.Player], "out")}
		v.Rows = append(v.Rows, row)
	}
	order := map[string]int{"gk": 0, "df": 1, "mf": 2, "fw": 3, "bench": 4, "out": 5}
	slices.SortStableFunc(v.Rows, func(a, b lineupRow) int {
		return cmp.Or(cmp.Compare(order[a.Slot], order[b.Slot]), cmp.Compare(a.Position, b.Position))
	})
	return "lineup", v, nil
}

// --- league -----------------------------------------------------------------

type tableView struct {
	Tables []app.Table // the club's league first
	Club   ids.ClubID
}

func (s *server) table(*http.Request) (string, any, error) {
	sc, ok := s.userSchedule()
	if !ok {
		return "", nil, errors.New("your club has no season")
	}
	v := tableView{Club: s.club()}
	for _, t := range s.w.Tables() {
		if t.RoundsCompleted == 0 && t.Season > 1 {
			// The off-season: last season's final table.
			t, _ = s.w.Table(competitions.SeasonRef{Competition: t.Competition, Season: t.Season - 1})
		}
		if t.Competition == sc.Competition {
			v.Tables = append([]app.Table{t}, v.Tables...)
		} else {
			v.Tables = append(v.Tables, t)
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
}

type fixturesView struct {
	Club   app.TeamLabel
	Clubs  []clubOption
	Season string
	Rows   []fixtureRow
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
	v := fixturesView{
		Club:   clubLabel,
		Season: fmt.Sprintf("%s season %d", sc.CompetitionName, sc.Season),
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
				Opponent:     opp,
				OpponentClub: oppClub,
				Played:       f.Played,
			}
			if f.Played {
				row.Result = fmt.Sprintf("%d-%d", f.Score[0], f.Score[1])
				row.Outcome = outcome(f.Score, f.Shootout, f.Home.Club == viewClub)
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
					Fixture: f.ID, Name: c.Name + " " + rd.Name, Round: int(rd.Round), When: s.w.Calendar().Format(rd.Kickoff),
					Opponent: opp, OpponentClub: oppClub, Played: f.Played,
				}
				if f.Played {
					row.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], penalties(f.Shootout))
					row.Outcome = outcome(f.Score, f.Shootout, f.Home.Club == viewClub)
				}
				v.Rows = append(v.Rows, row)
			}
		}
	}
	return "fixtures", v, nil
}

// --- report -----------------------------------------------------------------

type goalView struct {
	Minute     uint16
	Team       string
	PlayerName string
}

type reportView struct {
	Fixture      ids.FixtureID
	Context      string // e.g. "Founders League season 1 · Round 3", "Continental Cup 1 · Final"
	Penalties    string // e.g. "Won 4-3 on penalties" (home first)
	Competition  string
	Season       competitions.Season
	Round        competitions.Round
	Kickoff      string
	Played       bool
	Home         app.TeamLabel
	Away         app.TeamLabel
	Score        [2]uint16
	Outcome      string
	HomeSelected string
	AwaySelected string
	Goals        []goalView
}

func selectedText(b app.SelectedBy) string {
	if b == app.SelectedByManager {
		return "Manager selection"
	}
	return "Assistant selection"
}

func (s *server) reportPage(r *http.Request) (string, any, error) {
	var fixID ids.FixtureID
	if v := r.URL.Query().Get("fixture"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			fixID = ids.FixtureID(n)
		}
	} else if v := r.URL.Query().Get("id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			fixID = ids.FixtureID(n)
		}
	}
	if fixID == 0 {
		if s.report != nil && s.report.Fixture != 0 {
			fixID = s.report.Fixture
		} else {
			for _, sc := range s.w.Schedules() {
				for _, rd := range sc.Rounds {
					for _, f := range rd.Fixtures {
						if f.Played {
							fixID = f.ID
						}
					}
				}
			}
		}
	}
	if fixID == 0 {
		return "report", reportView{}, nil
	}

	rep, ok := s.w.MatchReport(fixID)
	info, hasF := s.fixtureInfo(fixID)
	f := info.FixtureLine
	cal := s.w.Calendar()

	v := reportView{
		Fixture: fixID,
	}

	if hasF {
		v.Competition = info.CompetitionName
		v.Round = info.Round.Round
		v.Season = info.Round.Season.Season
		v.Kickoff = cal.Format(info.Kickoff)
		v.Home = f.Home
		v.Away = f.Away
		v.Played = f.Played
		v.Score = f.Score
		if info.Cup {
			v.Context = fmt.Sprintf("%s %d · %s", info.CompetitionName, info.Round.Season.Season, capitalize(info.RoundName))
		} else {
			v.Context = fmt.Sprintf("%s season %d · %s", info.CompetitionName, info.Round.Season.Season, capitalize(info.RoundName))
		}
		if f.Shootout != [2]uint16{} {
			v.Penalties = fmt.Sprintf("%d-%d on penalties", f.Shootout[0], f.Shootout[1])
		}
	}

	if ok {
		v.Played = true
		v.Home = rep.Home
		v.Away = rep.Away
		v.Score = rep.Score
		v.Round = rep.Round.Round
		v.Season = rep.Round.Season.Season
		if rep.Home.Club == s.club() || rep.Away.Club == s.club() {
			v.Outcome = outcome(rep.Score, rep.Shootout, rep.Home.Club == s.club())
		}
		v.HomeSelected = selectedText(rep.Selected[0])
		v.AwaySelected = selectedText(rep.Selected[1])

		for _, g := range rep.Goals {
			team := rep.Home.ShortName
			if g.Side == matches.Away {
				team = rep.Away.ShortName
			}
			pName, _ := s.w.PlayerName(g.Scorer)
			if pName == "" {
				pName = s.name(g.Scorer)
			}
			v.Goals = append(v.Goals, goalView{Minute: g.Minute, Team: team, PlayerName: pName})
		}
	} else if hasF && f.Played {
		if f.Home.Club == s.club() || f.Away.Club == s.club() {
			v.Outcome = outcome(f.Score, f.Shootout, f.Home.Club == s.club())
		}
	} else if !hasF {
		return "", nil, errors.New("match not found")
	}

	return "report", v, nil
}

// --- inbox and money ------------------------------------------------------------

func (s *server) inbox(*http.Request) (string, any, error) {
	msgs := s.messages()
	slices.Reverse(msgs)
	return "inbox", msgs, nil
}

// messages renders the inbox, oldest first.
func (s *server) messages() []messageView {
	cal := s.w.Calendar()
	var out []messageView
	for _, m := range s.w.Inbox() {
		out = append(out, messageView{When: cal.Format(m.At), Text: s.messageText(m)})
	}
	return out
}

func (s *server) messageText(m app.InboxItem) string {
	venue := "away"
	if m.Home {
		venue = "home"
	}
	switch m.Kind {
	case inbox.KindMatchday:
		return fmt.Sprintf("Matchday: %s v %s (%s)", itemMatchName(m), m.OpponentLabel.ClubName, venue)
	case inbox.KindResult:
		return fmt.Sprintf("Result: %d-%d%s v %s (%s), %s", m.Goals[0], m.Goals[1], penalties(m.Shootout), m.OpponentLabel.ClubName, venue, itemMatchName(m))
	case inbox.KindSeasonEnded:
		if m.Cup {
			text := fmt.Sprintf("%s %d won by %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
			switch m.Stage {
			case "":
			case "winner":
				text += ": your club won it!"
			default:
				text += fmt.Sprintf("; you went out in the %s.", m.Stage)
			}
			return text
		}
		text := fmt.Sprintf("%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
		if m.Position > 0 {
			text += fmt.Sprintf("; you finished %d.", m.Position)
		}
		return text
	case inbox.KindSeasonStarted:
		if m.Cup {
			return fmt.Sprintf("%s %d drawn: first kickoff %s", m.CompetitionName, m.Season, s.w.Calendar().Format(m.Kickoff))
		}
		return fmt.Sprintf("%s season %d scheduled: first kickoff %s", m.CompetitionName, m.Season, s.w.Calendar().Format(m.Kickoff))
	case inbox.KindRenewed:
		return fmt.Sprintf("%s renewed until %s at %s a week", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindPlayerLeft:
		return fmt.Sprintf("%s left the club as a free agent", m.PlayerName)
	case inbox.KindPlayerJoined:
		return fmt.Sprintf("%s joined until %s at %s a week", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindRetired:
		return fmt.Sprintf("%s retired at %d", m.PlayerName, m.Age)
	case inbox.KindYouthJoined:
		return fmt.Sprintf("%s joined from the youth ranks until %s at %s a week", m.PlayerName, s.endDate(m.Expires), m.WeeklyWage)
	case inbox.KindDeveloped:
		return fmt.Sprintf("Development: %d of your players improved and %d declined over the year", m.Improved, m.Declined)
	}
	return s.transferText(m)
}

type ledgerRow struct {
	When    string
	What    string
	Amount  money.Money
	Balance money.Money
}

type financesView struct {
	Balance    money.Money
	WeeklyWage money.Money
	Entries    int
	Rows       []ledgerRow // newest first
}

func (s *server) finances(*http.Request) (string, any, error) {
	fin, ok := s.w.Finances(s.club())
	if !ok {
		return "", nil, errors.New("your club has no account")
	}
	v := financesView{Balance: fin.Balance, WeeklyWage: fin.WeeklyWage, Entries: len(fin.Entries)}
	var running money.Money
	for _, e := range fin.Entries {
		running += e.Amount
		what := e.Kind.String()
		if e.Fixture != 0 {
			what = fmt.Sprintf("%s, fixture %d", what, e.Fixture)
		}
		if e.Offer != 0 {
			what = fmt.Sprintf("%s, offer %d", what, e.Offer)
		}
		v.Rows = append(v.Rows, ledgerRow{When: s.w.Calendar().Format(e.At), What: what, Amount: e.Amount, Balance: running})
	}
	slices.Reverse(v.Rows)
	v.Rows = v.Rows[:min(len(v.Rows), 40)]
	return "finances", v, nil
}

// itemMatchName names an inbox message's round like matchName.
func itemMatchName(m app.InboxItem) string {
	if m.Cup {
		return m.CompetitionName + " " + m.RoundName
	}
	return capitalize(m.RoundName)
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
}

// cup shows the latest edition of each cup: rounds, ties, results and the
// winner. Before the first edition it explains how teams qualify.
func (s *server) cup(*http.Request) (string, any, error) {
	var out []cupView
	for _, c := range s.w.Cups() {
		v := cupView{Name: c.Name, Edition: int(c.Edition), Champion: c.Champion}
		for _, e := range c.Entrants {
			v.Mine = v.Mine || e.Club == s.club()
		}
		for _, r := range c.Rounds {
			title := capitalize(r.Name)
			if r.Name != "final" {
				title += "s"
			}
			rv := cupRoundView{Title: title, When: s.w.Calendar().Format(r.Kickoff)}
			for _, f := range r.Ties {
				t := cupTieView{Fixture: f.ID, Home: f.Home, Away: f.Away, Played: f.Played, Mine: f.Home.Club == s.club() || f.Away.Club == s.club()}
				if f.Played {
					t.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], penalties(f.Shootout))
					home := outcome(f.Score, f.Shootout, true) == "W"
					t.HomeWins, t.AwayWins = home, !home
				}
				rv.Ties = append(rv.Ties, t)
			}
			v.Rounds = append(v.Rounds, rv)
		}
		out = append(out, v)
	}
	return "cup", out, nil
}
