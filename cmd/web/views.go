package main

import (
	"cmp"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/careers"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
	"github.com/thewalpa/project-zimble/internal/storage"
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
	Page     string
	Career   bool
	Club     app.TeamLabel
	Date     string
	Balance  money.Money
	Rev      app.Revision
	Unsaved  bool
	Pending  bool // a matchday of the club is waiting
	Notes    []note
	Offers   []recoverOffer
	Data     any
	SaveName string
	SavesDir string
	Unread   int // unread inbox messages
}

func (s *server) layout(page string, data any) layout {
	l := layout{
		Page:     page,
		Notes:    s.notes,
		Offers:   s.offers,
		Data:     data,
		SaveName: filepath.Base(s.savePath),
		SavesDir: filepath.Clean(s.savesDir),
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

// matchName names a fixture's round for display: "Round 3" in a league,
// "Continental Cup quarter-final" in a cup, "Promotion Play-off" in a
// play-off (one round).
func matchName(info app.FixtureInfo) string {
	if info.Playoff {
		return info.CompetitionName
	}
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

type messageView struct {
	Event uint64
	When  string
	Text  string
	Read  bool
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
	for _, p := range s.w.Playoffs() {
		for _, r := range p.Rounds {
			for _, f := range r.Ties {
				if f.Home.Club != s.club() && f.Away.Club != s.club() {
					continue
				}
				fv := &fixtureView{Name: p.Name, Round: int(r.Round), Opponent: s.opponent(f), When: cal.Format(r.Kickoff), kickoff: r.Kickoff}
				switch {
				case r.Status == competitions.RoundAwaitingResults && v.Matchday == nil:
					v.Matchday = fv
				case r.Status == competitions.RoundScheduled && (v.Next == nil || r.Kickoff < v.Next.kickoff):
					v.Next = fv
				}
			}
		}
	}
	if v.Matchday != nil {
		if fix, ok := s.pendingFixture(); ok {
			if ml, err := s.w.MatchdayLineup(fix); err == nil {
				v.Matchday.LineupSource = s.w.LineupSourceLabel(ml)
				v.Matchday.DroppedNotes = s.w.LineupDroppedMessages(ml)
			}
		}
	}
	if l, live := s.w.LiveMatch(); live {
		v.Live = fmt.Sprintf("%d'  %s %d-%d %s", l.Position.Minute, l.Home.ClubName, l.View.Score[0], l.View.Score[1], l.Away.ClubName)
	}
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
	Nation    string
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
	SquadText   string
	YearOptions []int
	Sort        SortState
	Probable    *reportPitch // another club's lineup if it played today
}

func (s *server) squad(r *http.Request) (string, any, error) {
	viewClub := s.club()
	if cStr := r.URL.Query().Get("club"); cStr != "" {
		if n, err := strconv.ParseUint(cStr, 10, 64); err == nil && n != 0 {
			if _, ok := s.w.ObservedSquad(s.club(), ids.ClubID(n)); ok {
				viewClub = ids.ClubID(n)
			}
		}
	}
	squad, _ := s.w.ObservedSquad(s.club(), viewClub)
	clubLabel, _ := s.w.ClubLabel(viewClub)
	isUserClub := (viewClub == s.club())
	end := s.w.ContractYearEnd()
	defs := s.w.Content()
	var mins []string
	for _, q := range defs.Roster {
		mins = append(mins, fmt.Sprintf("%d %s", q.Min, q.Position))
	}
	squadText := fmt.Sprintf("You have %d players; a squad holds at most %d, and at least %s.", len(squad), defs.SquadLimit, strings.Join(mins, ", "))
	e := defs.Economy
	sortState := newSortState(r, "pos", "asc")
	v := squadView{
		Club:        clubLabel,
		IsUserClub:  isUserClub,
		ContractEnd: s.endDate(end),
		SquadText:   squadText,
		Sort:        sortState,
	}
	for y := e.ContractYears[0]; y <= e.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, c := range s.w.Summary().ClubRows {
		v.Clubs = append(v.Clubs, clubOption{
			ID:        c.ID,
			Name:      c.Name,
			ShortName: c.ShortName,
			Nation:    c.Nation,
			Selected:  c.ID == viewClub,
			IsUser:    c.ID == s.club(),
		})
		if c.ID == viewClub && !isUserClub {
			if l, err := s.w.ProbableLineup(c.SeniorTeam); err == nil {
				v.Probable = s.pitchOf(c.Name, l)
				v.Probable.Title = "Probable lineup"
				v.Probable.Note = "What the club would field if it played today; the squad changes by kickoff."
			}
		}
	}
	for _, p := range squad {
		c, _ := s.w.Calendar().Civil(p.Contract.Expires)
		row := squadRow{SquadPlayer: p, Ends: c.Year, FinalYear: isUserClub && p.Contract.Expires == end}
		if row.FinalYear {
			row.Offer, _ = s.w.SuggestContract(p.Player)
		}
		v.Rows = append(v.Rows, row)
	}
	sortSquadRows(v.Rows, sortState.Col, sortState.Dir)
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
	WindowNote  string // while a transfer window is open
	Sort        SortState
}

func (s *server) free(r *http.Request) (string, any, error) {
	defs := s.w.Content()
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	hasRoom := len(squad) < defs.SquadLimit
	_, locked := s.w.Pending()
	sortState := newSortState(r, "ovr", "desc")
	v := freeView{Locked: locked, RetireAge: players.FreeAgentRetirementAge, Sort: sortState}
	if s.w.TransferWindow().Open {
		win := s.w.TransferWindow()
		v.WindowNote = fmt.Sprintf("Until %s, only you may sign free agents; AI clubs can sign them from then.", s.w.Calendar().Format(win.FreeAgentsOpen))
	}
	for y := defs.Economy.ContractYears[0]; y <= defs.Economy.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, p := range s.observedFreeAgents() {
		o, _ := s.w.SuggestContract(p.Player)
		v.Rows = append(v.Rows, freeRow{SquadPlayer: p, Offer: o, Room: hasRoom})
	}
	sortFreeRows(v.Rows, sortState.Col, sortState.Dir)
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
	Slot         string
	Availability string
	Selectable   bool
}

type hiddenLineupSlot struct {
	Player ids.PlayerID
	Slot   string
}

// pitchChip is one player on the lineup pitch, the bench or among the
// unselected squad.
type pitchChip struct {
	Player        ids.PlayerID
	Name          string
	Short         string // the last part of the name, for the pitch
	Position      string
	Overall       int
	Condition     uint8
	DaysOut       uint16
	Natural       string // slot value of the player's natural role
	OutOfPosition bool   // starts in another role than his natural one
	Unavailable   bool   // may not be named for this match
}

// pitchLine is one line of starters, in slot order: the match spreads a
// line's players across the width in this order.
type pitchLine struct {
	Slot    string
	Label   string
	Players []pitchChip
}

// pitchView draws the lineup: lines from attack down to goal, then the bench
// and the rest of the squad. Order lists the lineup's player IDs as the
// form's "order" field expects them.
type pitchView struct {
	Lines     []pitchLine
	Bench     []pitchChip
	Reserves  []pitchChip
	Formation string
	Starters  int
	Want      int
	Order     string
}

type lineupView struct {
	Fixture          ids.FixtureID
	Plan             bool
	HasFixture       bool
	Unavailable      []string
	Title            string
	State            string
	Mentality        string
	Mentalities      []string
	Rows             []lineupRow
	HiddenSlots      []hiddenLineupSlot
	Slots            []slotOption
	Sort             SortState
	DroppedNotes     []string
	IsSuggested      bool
	AvailableOnly    bool
	AvailabilityNote string
	Pitch            pitchView
}

// newPitch places l's players on the pitch. Players who have left the squad
// are not drawn; the page's notes name them.
func newPitch(l selection.Lineup, squad []app.SquadPlayer, byPlayer map[ids.PlayerID]app.LineupEligibility, availableOnly bool) pitchView {
	bySquad := make(map[ids.PlayerID]app.SquadPlayer, len(squad))
	for _, p := range squad {
		bySquad[p.Player] = p
	}
	chip := func(p app.SquadPlayer, slot string) pitchChip {
		natural := slotName(app.NaturalRole(p.Position))
		short := p.Name
		if i := strings.LastIndex(short, " "); i >= 0 {
			short = short[i+1:]
		}
		return pitchChip{
			Player: p.Player, Name: p.Name, Short: short, Position: p.Position.String(), Overall: p.Overall,
			Condition: p.Condition, DaysOut: p.DaysOut, Natural: natural,
			OutOfPosition: slot != "bench" && slot != "out" && slot != natural,
			Unavailable:   !byPlayer[p.Player].Eligibility.Selectable(),
		}
	}
	v := pitchView{Formation: app.FormationLabel(l), Want: matches.StartersPerTeam}
	var order []string
	for _, line := range []struct {
		role  matches.Role
		label string
	}{{matches.Forward, "Attack"}, {matches.Midfielder, "Midfield"}, {matches.Defender, "Defence"}, {matches.Goalkeeper, "Goal"}} {
		pl := pitchLine{Slot: slotName(line.role), Label: line.label}
		for _, st := range l.Starters {
			if p, ok := bySquad[st.Player]; ok && st.Role == line.role {
				pl.Players = append(pl.Players, chip(p, pl.Slot))
			}
		}
		v.Lines = append(v.Lines, pl)
	}
	selected := map[ids.PlayerID]bool{}
	for _, st := range l.Starters {
		selected[st.Player] = true
		if _, ok := bySquad[st.Player]; ok {
			v.Starters++
			order = append(order, strconv.FormatUint(uint64(st.Player), 10))
		}
	}
	for _, id := range l.Bench {
		selected[id] = true
		if p, ok := bySquad[id]; ok {
			v.Bench = append(v.Bench, chip(p, "bench"))
			order = append(order, strconv.FormatUint(uint64(id), 10))
		}
	}
	for _, p := range squad {
		if selected[p.Player] || availableOnly && !byPlayer[p.Player].Eligibility.Selectable() {
			continue
		}
		v.Reserves = append(v.Reserves, chip(p, "out"))
	}
	slices.SortStableFunc(v.Reserves, func(a, b pitchChip) int {
		return cmp.Or(cmp.Compare(slotRank(a.Natural), slotRank(b.Natural)), cmp.Compare(b.Overall, a.Overall))
	})
	v.Order = strings.Join(order, " ")
	return v
}

// slotRank orders slot values goalkeeper first.
func slotRank(slot string) matches.Role { return slotRoles[slot] }

func (s *server) lineup(r *http.Request) (string, any, error) {
	fixture, hasFixture := s.pendingFixture()
	planMode := r.URL.Query().Get("plan") == "1" || !hasFixture
	if _, live := s.w.LiveMatch(); live && !planMode {
		// The match has kicked off: changes are substitutions now.
		return s.live(r)
	}
	suggest := r.URL.Query().Get("suggest") == "1"
	var l selection.Lineup
	var state string
	var droppedNotes []string
	var eligibility []app.LineupEligibility
	var plan app.TeamPlan
	if planMode {
		var err error
		plan, err = s.w.TeamPlan()
		if err != nil {
			return "", nil, err
		}
		l, eligibility = plan.Lineup, plan.Squad
		state = "Starting point for matches without a submitted lineup"
		if plan.Saved {
			state = "Saved team plan; used for matches without a submitted lineup"
		}
	} else {
		if suggest {
			var err error
			if l, err = s.w.SuggestLineup(fixture); err != nil {
				return "", nil, err
			}
			state = "The assistant's suggestion (used unless you save changes)"
		} else {
			ml, err := s.w.MatchdayLineup(fixture)
			if err != nil {
				return "", nil, err
			}
			l = ml.Lineup
			state = s.w.LineupSourceLabel(ml)
			droppedNotes = s.w.LineupDroppedMessages(ml)
		}
		var err error
		eligibility, err = s.w.SquadEligibility(fixture)
		if err != nil {
			return "", nil, err
		}
	}
	byPlayer := make(map[ids.PlayerID]app.LineupEligibility, len(eligibility))
	emergency := false
	for _, e := range eligibility {
		byPlayer[e.Player] = e
		if e.Eligibility == app.EligibleInjured {
			emergency = true
		}
	}
	sortState := newSortState(r, "selection", "asc")
	v := lineupView{
		Fixture: fixture, Plan: planMode, HasFixture: hasFixture,
		State: state, Mentality: l.Tactics.Mentality.String(), Slots: slotOptions,
		Title:         "Team plan",
		Sort:          sortState,
		DroppedNotes:  droppedNotes,
		IsSuggested:   suggest,
		AvailableOnly: r.URL.Query().Get("available") == "1",
	}
	if !planMode {
		info, _ := s.fixtureInfo(fixture)
		v.Title = fmt.Sprintf("%s v %s, %s", matchName(info), s.opponent(info.FixtureLine), s.w.Calendar().Format(info.Kickoff))
	}
	for _, id := range plan.Unavailable {
		if name, ok := s.w.PlayerName(id); ok {
			v.Unavailable = append(v.Unavailable, name)
		}
	}
	if emergency {
		v.AvailabilityNote = "Not enough fit players for a legal eleven; injured players are selectable under the emergency rule."
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
	squad, _ := s.w.ObservedSquad(s.club(), s.club())
	for _, p := range squad {
		e := byPlayer[p.Player]
		if v.AvailableOnly && !e.Eligibility.Selectable() {
			v.HiddenSlots = append(v.HiddenSlots, hiddenLineupSlot{Player: p.Player, Slot: cmp.Or(slot[p.Player], "out")})
			continue
		}
		row := lineupRow{SquadPlayer: p, Slot: cmp.Or(slot[p.Player], "out"), Selectable: e.Eligibility.Selectable()}
		switch e.Eligibility {
		case app.EligibleFit:
			row.Availability = "Available"
		case app.EligibleInjured:
			row.Availability = "Injured, selectable: not enough fit players"
		case app.IneligibleInjured:
			row.Availability = fmt.Sprintf("Injured: %d days out", e.DaysOut)
		}
		v.Rows = append(v.Rows, row)
	}
	sortLineupRows(v.Rows, sortState.Col, sortState.Dir)
	v.Pitch = newPitch(l, squad, byPlayer, v.AvailableOnly)
	return "lineup", v, nil
}

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
	for _, c := range s.w.Playoffs() {
		for _, rd := range c.Rounds {
			for _, f := range rd.Ties {
				if f.Home.Club != viewClub && f.Away.Club != viewClub {
					continue
				}
				opp, oppClub := s.clubOpponent(f, viewClub)
				row := fixtureRow{
					Fixture: f.ID, Name: c.Name, Round: int(rd.Round), When: s.w.Calendar().Format(rd.Kickoff),
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
	sortFixtureRows(v.Rows, sortState.Col, sortState.Dir)
	return "fixtures", v, nil
}

// --- report -----------------------------------------------------------------

type goalView struct {
	Minute     uint16
	Team       string
	PlayerName string
}

type eventView struct {
	Minute uint16
	Text   string
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
	Events       []eventView
	Stats        []app.StatLine // none when the engine keeps no statistics
	Lineups      []*reportPitch // what each side started with, home first
}

// reportPitch is a lineup drawn read-only.
type reportPitch struct {
	Title     string
	Note      string // a caveat shown under the title
	Formation string
	Lines     []reportLine
	Bench     []reportChip
}

type reportLine struct {
	Label   string
	Players []reportChip
}

type reportChip struct {
	Player   ids.PlayerID
	Name     string
	Short    string
	Position string
	Overall  int // the player's overall now; 0 when unknown
}

func selectedText(b app.SelectedBy) string {
	if b == app.SelectedByManager {
		return "Your lineup"
	}
	return "The assistant's suggestion"
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
		if info.Playoff {
			v.Context = fmt.Sprintf("%s season %d", s.w.PlayoffTitle(info.Round.Season.Competition), info.Round.Season.Season)
		} else if info.Cup {
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
		v.Lineups = s.reportPitches(rep)

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
		v.Events = s.eventViews(rep.Events, rep.Home, rep.Away)
		v.Stats = app.StatLines(rep.Stats)
	} else if hasF && f.Played {
		if f.Home.Club == s.club() || f.Away.Club == s.club() {
			v.Outcome = outcome(f.Score, f.Shootout, f.Home.Club == s.club())
		}
	} else if !hasF {
		return "", nil, errors.New("match not found")
	}

	return "report", v, nil
}

// eventViews words match events for a timeline, in order.
func (s *server) eventViews(events []matches.MatchEvent, home, away app.TeamLabel) []eventView {
	playerName := func(id ids.PlayerID) string {
		name, _ := s.w.PlayerName(id)
		if name == "" {
			name = s.name(id)
		}
		return name
	}
	var out []eventView
	for _, e := range events {
		team := home.ShortName
		if e.Side == matches.Away {
			team = away.ShortName
		}
		var text string
		switch e.Kind {
		case matches.EventGoal:
			text = fmt.Sprintf("%s · Goal · %s", team, playerName(e.Player))
		case matches.EventSubstitution:
			text = fmt.Sprintf("%s · Substitution · %s on for %s", team, playerName(e.Player), playerName(e.Other))
		case matches.EventMentalityChange:
			text = fmt.Sprintf("%s · Mentality changed to %s", team, e.Mentality.String())
		case matches.EventPeriodEnd:
			if e.Period == matches.FirstHalf {
				text = "Half time"
			} else if e.Period == matches.SecondHalf {
				text = "Full time"
			}
		}
		if text != "" {
			out = append(out, eventView{Minute: e.Minute, Text: text})
		}
	}
	return out
}

// pitchOf draws a lineup read-only: starters by line, then the bench.
func (s *server) pitchOf(club string, l selection.Lineup) *reportPitch {
	chip := func(id ids.PlayerID) reportChip {
		name, _ := s.w.PlayerName(id)
		if name == "" {
			name = s.name(id)
		}
		c := reportChip{Player: id, Name: name, Short: name}
		if i := strings.LastIndex(name, " "); i >= 0 {
			c.Short = name[i+1:]
		}
		if p, ok := s.w.ObservedPlayerProfile(s.club(), id); ok {
			c.Position = p.Position.String()
			c.Overall = p.Overall
		}
		return c
	}
	v := &reportPitch{Title: club + " lineup", Formation: app.FormationLabel(l)}
	for _, line := range []struct {
		role  matches.Role
		label string
	}{{matches.Forward, "Attack"}, {matches.Midfielder, "Midfield"}, {matches.Defender, "Defence"}, {matches.Goalkeeper, "Goal"}} {
		rl := reportLine{Label: line.label}
		for _, st := range l.Starters {
			if st.Role == line.role {
				rl.Players = append(rl.Players, chip(st.Player))
			}
		}
		v.Lines = append(v.Lines, rl)
	}
	for _, id := range l.Bench {
		v.Bench = append(v.Bench, chip(id))
	}
	return v
}

// reportPitches draws what each side started the match with, home first;
// none for a result recorded without a report.
func (s *server) reportPitches(rep app.MatchReport) []*reportPitch {
	var out []*reportPitch
	for i, label := range []app.TeamLabel{rep.Home, rep.Away} {
		if l := rep.Lineups[i]; len(l.Starters) > 0 {
			out = append(out, s.pitchOf(label.ClubName, l))
		}
	}
	return out
}

type inboxView struct {
	Messages []messageView
	Unread   int
	Sort     SortState
}

func (s *server) inbox(r *http.Request) (string, any, error) {
	msgs := s.messages()
	slices.Reverse(msgs)
	sortState := newSortState(r, "when", "desc")
	sortInboxMessages(msgs, sortState.Col, sortState.Dir)
	return "inbox", inboxView{Messages: msgs, Unread: s.w.UnreadInboxCount(), Sort: sortState}, nil
}

// messages renders the inbox, oldest first.
func (s *server) messages() []messageView {
	cal := s.w.Calendar()
	var out []messageView
	for _, m := range s.w.Inbox() {
		out = append(out, messageView{Event: uint64(m.Event), When: cal.Format(m.At), Text: s.messageText(m), Read: m.Read})
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
		return fmt.Sprintf("Matchday: %s v %s (%s)", s.itemMatchName(m), m.OpponentLabel.ClubName, venue)
	case inbox.KindResult:
		return fmt.Sprintf("Result: %d-%d%s v %s (%s), %s", m.Goals[0], m.Goals[1], penalties(m.Shootout), m.OpponentLabel.ClubName, venue, s.itemMatchName(m))
	case inbox.KindSeasonEnded:
		if title := s.w.PlayoffTitle(m.Competition); title != "" {
			upper, _, _ := s.w.PlayoffDivisions(m.Competition)
			text := fmt.Sprintf("%s season %d decided: each tie's winner plays in %s next season", title, m.Season, upper)
			if tie, ok := s.playoffTie(competitions.SeasonRef{Competition: m.Competition, Season: competitions.Season(m.Season)}); ok {
				if outcome(tie.Score, tie.Shootout, tie.Home.Club == s.club()) == "W" {
					text += "; you won your tie"
				} else {
					text += "; you lost your tie"
				}
			}
			return text + "."
		}
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
			text += fmt.Sprintf("; you finished %s", app.Ordinal(m.Position))
			switch up, down := s.w.SeasonMove(competitions.SeasonRef{Competition: m.Competition, Season: competitions.Season(m.Season)}, m.Position); {
			case up:
				text += ": promoted to the division above"
			case down:
				text += ": relegated to the division below"
			}
			text += "."
		}
		return text
	case inbox.KindSeasonStarted:
		if title := s.w.PlayoffTitle(m.Competition); title != "" {
			return fmt.Sprintf("%s season %d drawn: kickoff %s", title, m.Season, s.w.Calendar().Format(m.Kickoff))
		}
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
	case inbox.KindReleased:
		return fmt.Sprintf("%s left the club as a free agent; you paid %s", m.PlayerName, m.Compensation)
	case inbox.KindInjured:
		return fmt.Sprintf("%s is out for %d days", m.PlayerName, m.Days)
	case inbox.KindRecovered:
		return fmt.Sprintf("%s is fit again", m.PlayerName)
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
	Sort       SortState
}

func (s *server) finances(r *http.Request) (string, any, error) {
	fin, ok := s.w.Finances(s.club())
	if !ok {
		return "", nil, errors.New("your club has no account")
	}
	sortState := newSortState(r, "when", "desc")
	v := financesView{Balance: fin.Balance, WeeklyWage: fin.WeeklyWage, Entries: len(fin.Entries), Sort: sortState}
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
		if e.Player != 0 {
			what = fmt.Sprintf("%s, %s", what, s.name(e.Player))
		}
		v.Rows = append(v.Rows, ledgerRow{When: s.w.Calendar().Format(e.At), What: what, Amount: e.Amount, Balance: running})
	}
	slices.Reverse(v.Rows)
	v.Rows = v.Rows[:min(len(v.Rows), 40)]
	sortLedgerRows(v.Rows, sortState.Col, sortState.Dir)
	return "finances", v, nil
}

// itemMatchName names an inbox message's round like matchName.
func (s *server) itemMatchName(m app.InboxItem) string {
	if s.w.PlayoffTitle(m.Competition) != "" {
		return m.CompetitionName
	}
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
		title := capitalize(rRound.Name)
		if rRound.Name != "final" {
			title += "s"
		}
		rv := cupRoundView{Title: title, When: s.w.Calendar().Format(rRound.Kickoff)}
		for _, f := range rRound.Ties {
			t := cupTieView{Fixture: f.ID, Home: f.Home, Away: f.Away, Played: f.Played, Mine: f.Home.Club == s.club() || f.Away.Club == s.club()}
			if f.Played {
				t.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], penalties(f.Shootout))
				home := outcome(f.Score, f.Shootout, true) == "W"
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
				t.Result = fmt.Sprintf("%d-%d%s", f.Score[0], f.Score[1], penalties(f.Shootout))
				home := outcome(f.Score, f.Shootout, true) == "W"
				t.HomeWins, t.AwayWins = home, !home
			}
			v.Mine = v.Mine || t.Mine
			v.Ties.Ties = append(v.Ties.Ties, t)
		}
	}
	return v
}

// playoffTie is the user club's tie in a play-off edition, once played.
func (s *server) playoffTie(ref competitions.SeasonRef) (app.FixtureLine, bool) {
	p, ok := s.w.Playoff(ref)
	if !ok {
		return app.FixtureLine{}, false
	}
	for _, r := range p.Rounds {
		for _, f := range r.Ties {
			if f.Played && (f.Home.Club == s.club() || f.Away.Club == s.club()) {
				return f, true
			}
		}
	}
	return app.FixtureLine{}, false
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

type playerCareerView struct {
	ClubID, Club, From, Until, Joined, Left string
}

type playerView struct {
	app.PlayerProfile
	Ends    int
	IsMine  bool
	Career  []playerCareerView
	Missing string // set when the ID names no player
}

func playerJoined(joined careers.Joined) string {
	switch joined {
	case careers.JoinedAtStart:
		return "At career start"
	case careers.JoinedYouth:
		return "Joined through youth"
	case careers.JoinedFree:
		return "Signed as a free agent"
	case careers.JoinedTransfer:
		return "Signed by transfer"
	default:
		return "Joined"
	}
}

func playerLeft(left careers.Left) string {
	switch left {
	case careers.LeftNot:
		return "Current club"
	case careers.LeftTransfer:
		return "Sold"
	case careers.LeftExpired:
		return "Contract expired"
	case careers.LeftReleased:
		return "Released"
	case careers.LeftRetired:
		return "Retired"
	default:
		return "Left"
	}
}

func (s *server) player(r *http.Request) (string, any, error) {
	arg := r.URL.Query().Get("id")
	n, err := strconv.ParseUint(arg, 10, 64)
	if err != nil || n == 0 {
		return "player", playerView{Missing: fmt.Sprintf("%q is not a player ID.", arg)}, nil
	}
	p, ok := s.w.ObservedPlayerProfile(s.club(), ids.PlayerID(n))
	if !ok {
		return "player", playerView{Missing: fmt.Sprintf("There is no player %d.", n)}, nil
	}
	c, _ := s.w.Calendar().Civil(p.Contract.Expires)
	spells, _ := s.w.PlayerCareer(ids.PlayerID(n))
	history := make([]playerCareerView, 0, len(spells))
	for _, spell := range spells {
		from := s.w.Calendar().Format(spell.From)
		if spell.Joined == careers.JoinedAtStart {
			from = "before " + from
		}
		until := "Present"
		if !spell.Current() {
			until = s.w.Calendar().Format(spell.Until)
		}
		joined := playerJoined(spell.Joined)
		if spell.Joined == careers.JoinedTransfer {
			joined += " for " + spell.Fee.String()
		}
		history = append(history, playerCareerView{ClubID: fmt.Sprint(spell.Club), Club: spell.ClubName, From: from, Until: until, Joined: joined, Left: playerLeft(spell.Left)})
	}
	return "player", playerView{PlayerProfile: p, Ends: c.Year, IsMine: p.Club != 0 && p.Club == s.club(), Career: history}, nil
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
