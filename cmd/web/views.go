package main

import (
	"cmp"
	"errors"
	"fmt"
	"html/template"
	"slices"

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
func (s *server) userSchedule() (app.Schedule, bool) {
	for _, sc := range s.w.Schedules() {
		for _, f := range sc.Rounds[0].Fixtures {
			if f.Home.Club == s.club() || f.Away.Club == s.club() {
				return sc, true
			}
		}
	}
	return app.Schedule{}, false
}

func (s *server) seasonDone(sc app.Schedule) bool {
	t, _ := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season})
	return t.Complete
}

func (s *server) fixtureInfo(id ids.FixtureID) (app.FixtureLine, app.RoundSchedule, bool) {
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

// opponent names the club's opponent in a fixture and the venue.
func (s *server) opponent(f app.FixtureLine) string {
	if f.Home.Club == s.club() {
		return f.Away.ClubName + " (home)"
	}
	return f.Home.ClubName + " (away)"
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

// outcome is W, D or L for the club in a played fixture.
func outcome(score [2]uint16, home bool) string {
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
	Round    int
	Opponent string
	When     string
}

type matchReport struct {
	Title   string
	Outcome string
	Goals   []string
	Others  []string
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

func (s *server) home() (string, any, error) {
	if s.w == nil {
		preview, err := app.NewWorld(app.DefaultConfig(s.seed))
		if err != nil {
			return "", nil, err
		}
		return "choose", chooseView{Seed: uint64(s.seed), Clubs: preview.Summary().ClubRows}, nil
	}
	cal := s.w.Calendar()
	v := homeView{Report: s.report, ContractEnd: cal.Format(s.w.ContractYearEnd()), Expiring: len(s.expiring())}
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
				fv := &fixtureView{Round: int(r.Round), Opponent: s.opponent(f), When: cal.Format(r.Kickoff)}
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
		line := fmt.Sprintf("%s %d-%d %s", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName)
		if m.Home.Club != s.club() && m.Away.Club != s.club() {
			r.Others = append(r.Others, line)
			continue
		}
		r.Title, r.Outcome = line, outcome(m.Score, m.Home.Club == s.club())
		names := map[ids.PlayerID]string{}
		for _, c := range []ids.ClubID{m.Home.Club, m.Away.Club} {
			squad, _ := s.w.Squad(c)
			for _, p := range squad {
				names[p.Player] = p.Name
			}
		}
		for _, g := range m.Goals {
			team := m.Home.ShortName
			if g.Side == matches.Away {
				team = m.Away.ShortName
			}
			r.Goals = append(r.Goals, fmt.Sprintf("%d'  %s %s", g.Minute, team, names[g.Scorer]))
		}
	}
	if r.Title == "" {
		return nil
	}
	return r
}

// --- squad and contracts --------------------------------------------------------

type squadRow struct {
	app.SquadPlayer
	Ends      int
	FinalYear bool
	Offer     app.ContractOffer // the usual terms, for final-year players
}

type squadView struct {
	Rows        []squadRow
	ContractEnd string
	YearOptions []int
}

func (s *server) squad() (string, any, error) {
	squad, _ := s.w.Squad(s.club())
	end := s.w.ContractYearEnd()
	e := s.w.Content().Economy
	v := squadView{ContractEnd: s.endDate(end)}
	for y := e.ContractYears[0]; y <= e.ContractYears[1]; y++ {
		v.YearOptions = append(v.YearOptions, y)
	}
	for _, p := range squad {
		c, _ := s.w.Calendar().Civil(p.Contract.Expires)
		row := squadRow{SquadPlayer: p, Ends: c.Year, FinalYear: p.Contract.Expires == end}
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

func (s *server) free() (string, any, error) {
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

func (s *server) lineup() (string, any, error) {
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
	f, r, _ := s.fixtureInfo(fixture)
	v := lineupView{
		Fixture: fixture, State: state, Mentality: l.Tactics.Mentality.String(), Slots: slotOptions,
		Title: fmt.Sprintf("Round %d v %s, %s", r.Round, s.opponent(f), s.w.Calendar().Format(r.Kickoff)),
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
	Table app.Table
	Club  ids.ClubID
}

func (s *server) table() (string, any, error) {
	sc, ok := s.userSchedule()
	if !ok {
		return "", nil, errors.New("your club has no season")
	}
	t, _ := s.w.Table(competitions.SeasonRef{Competition: sc.Competition, Season: sc.Season})
	return "table", tableView{Table: t, Club: s.club()}, nil
}

type fixtureRow struct {
	Round    int
	When     string
	Opponent string
	Result   string
	Outcome  string
}

type fixturesView struct {
	Season string
	Rows   []fixtureRow
}

func (s *server) fixtures() (string, any, error) {
	sc, ok := s.userSchedule()
	if !ok {
		return "", nil, errors.New("your club has no season")
	}
	v := fixturesView{Season: fmt.Sprintf("%s season %d", sc.CompetitionName, sc.Season)}
	for _, r := range sc.Rounds {
		for _, f := range r.Fixtures {
			if f.Home.Club != s.club() && f.Away.Club != s.club() {
				continue
			}
			row := fixtureRow{Round: int(r.Round), When: s.w.Calendar().Format(r.Kickoff), Opponent: s.opponent(f)}
			if f.Played {
				row.Result = fmt.Sprintf("%d-%d", f.Score[0], f.Score[1])
				row.Outcome = outcome(f.Score, f.Home.Club == s.club())
			}
			v.Rows = append(v.Rows, row)
		}
	}
	return "fixtures", v, nil
}

// --- inbox and money ------------------------------------------------------------

func (s *server) inbox() (string, any, error) {
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
		return fmt.Sprintf("Matchday: round %d v %s (%s)", m.Round, m.OpponentLabel.ClubName, venue)
	case inbox.KindResult:
		return fmt.Sprintf("Result: %d-%d v %s (%s)", m.Goals[0], m.Goals[1], m.OpponentLabel.ClubName, venue)
	case inbox.KindSeasonEnded:
		text := fmt.Sprintf("%s season %d ended: champion %s", m.CompetitionName, m.Season, m.ChampionLabel.ClubName)
		if m.Position > 0 {
			text += fmt.Sprintf("; you finished %d.", m.Position)
		}
		return text
	case inbox.KindSeasonStarted:
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
	return fmt.Sprintf("Message kind %d", m.Kind)
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

func (s *server) finances() (string, any, error) {
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
		v.Rows = append(v.Rows, ledgerRow{When: s.w.Calendar().Format(e.At), What: what, Amount: e.Amount, Balance: running})
	}
	slices.Reverse(v.Rows)
	v.Rows = v.Rows[:min(len(v.Rows), 40)]
	return "finances", v, nil
}
