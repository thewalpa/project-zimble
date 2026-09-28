package main

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
	"github.com/thewalpa/project-zimble/internal/storage"
)

//go:embed templates/*.html
var templateFS embed.FS

// pageNames are the pages, each rendered inside layout.html.
var pageNames = []string{"choose", "home", "squad", "lineup", "table", "fixtures", "free", "inbox", "finances", "report", "cup", "transfers"}

type config struct {
	seed     random.Seed
	club     ids.ClubID // zero: choose in the browser
	loadPath string
	savePath string
}

// server is the web client: one career and what the pages need between
// requests. The world has one writer; every request holds mu.
//
// Changes are POST forms carrying the revision the page was rendered at
// ("rev"). A form from an out-of-date page is refused, never applied to a
// world it did not show. After a change the browser is redirected to a page
// (post/redirect/get), with notes for the next page kept here.
type server struct {
	mu            sync.Mutex
	w             *app.World // nil until a club is chosen
	seed          random.Seed
	savePath      string
	saved         bool         // the career exists on disk...
	savedRevision app.Revision // ...at this revision
	warnedYearEnd sim.GameInstant
	notes         []note
	report        *matchReport // the latest matchday, shown on the home page
	pages         map[string]*template.Template
	mux           *http.ServeMux
}

type note struct {
	Text  string
	Error bool
}

func newServer(cfg config) (*server, error) {
	s := &server{seed: cfg.seed, savePath: cfg.savePath, pages: map[string]*template.Template{}}
	for _, name := range pageNames {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	switch {
	case cfg.loadPath != "":
		w, err := storage.Load(cfg.loadPath)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", cfg.loadPath, err)
		}
		if _, ok := w.UserClub(); !ok {
			return nil, fmt.Errorf("%s has no managed club; start a new career instead", cfg.loadPath)
		}
		s.w, s.seed, s.saved, s.savedRevision = w, w.Summary().Seed, true, w.Revision()
	case cfg.club != 0:
		if err := s.newCareer(cfg.club); err != nil {
			return nil, err
		}
	}

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /{$}", s.page(s.home))
	for _, p := range []struct {
		path string
		view func(*http.Request) (string, any, error)
	}{
		{"/squad", s.squad}, {"/lineup", s.lineup}, {"/table", s.table}, {"/fixtures", s.fixtures}, {"/cup", s.cup},
		{"/report", s.reportPage}, {"/free", s.free}, {"/inbox", s.inbox}, {"/finances", s.finances}, {"/transfers", s.transfers},
	} {
		s.mux.HandleFunc("GET "+p.path, s.page(s.needCareer(p.view)))
	}
	for _, a := range []struct {
		path string
		act  func(url.Values) (string, error)
	}{
		{"/new", s.chooseClub}, {"/continue", s.next}, {"/season", s.playSeason}, {"/lineup", s.submitLineup},
		{"/renew", s.renew}, {"/release", s.release}, {"/sign", s.sign}, {"/bid", s.bid}, {"/answer", s.answer}, {"/save", s.save},
	} {
		s.mux.HandleFunc("POST "+a.path, s.action(a.act))
	}
	return s, nil
}

func (s *server) ServeHTTP(rw http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(rw, r) }

// page renders a view. A view returns its page name and data.
func (s *server) page(view func(*http.Request) (string, any, error)) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		name, data, err := view(r)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		layout := s.layout(name, data)
		s.notes = nil // shown now
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.pages[name].Execute(rw, layout); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
	}
}

// needCareer sends a page to the club chooser while there is no career.
func (s *server) needCareer(view func(*http.Request) (string, any, error)) func(*http.Request) (string, any, error) {
	return func(r *http.Request) (string, any, error) {
		if s.w == nil {
			return s.home(r)
		}
		return view(r)
	}
}

// action runs a change from a form and redirects to the page it names. An
// error becomes a note on the page the form came from.
func (s *server) action(act func(url.Values) (string, error)) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(rw, "cross-site request refused", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		back := r.PostForm.Get("back")
		if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") {
			back = "/"
		}
		to, err := "", s.checkRevision(r.PostForm)
		if err == nil {
			to, err = act(r.PostForm)
		}
		if err != nil {
			s.notes = append(s.notes, note{Text: strings.TrimPrefix(err.Error(), "app: "), Error: true})
			to = back
		}
		http.Redirect(rw, r, to, http.StatusSeeOther)
	}
}

// sameOrigin refuses requests another site makes the browser send: this
// server changes a game on the user's machine, so only its own pages may.
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	}
	return true
}

var errStalePage = errors.New("the page was out of date, so nothing was changed; here is the current state")

// checkRevision refuses a form rendered at another world revision. Every
// form of a career page carries one.
func (s *server) checkRevision(form url.Values) error {
	if s.w == nil {
		return nil
	}
	rev, err := strconv.ParseUint(form.Get("rev"), 10, 64)
	if err != nil || app.Revision(rev) != s.w.Revision() {
		return errStalePage
	}
	return nil
}

func (s *server) say(format string, args ...any) {
	s.notes = append(s.notes, note{Text: fmt.Sprintf(format, args...)})
}

func (s *server) club() ids.ClubID {
	c, _ := s.w.UserClub()
	return c
}

// --- careers -----------------------------------------------------------------

func (s *server) newCareer(club ids.ClubID) error {
	cfg := app.DefaultConfig(s.seed)
	cfg.UserClub = club
	w, err := app.NewWorld(cfg)
	if err != nil {
		return err
	}
	s.w, s.saved, s.report, s.warnedYearEnd = w, false, nil, 0
	return nil
}

func (s *server) chooseClub(form url.Values) (string, error) {
	if s.w != nil {
		return "/", errors.New("a career is already under way")
	}
	n, err := strconv.ParseUint(form.Get("club"), 10, 64)
	if err != nil {
		return "", errors.New("choose a club")
	}
	if err := s.newCareer(ids.ClubID(n)); err != nil {
		return "", err
	}
	s.say("Welcome! Start with Continue to go to your first matchday.")
	return "/", nil
}

func (s *server) save(url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("there is no career to save")
	}
	if err := storage.Save(s.savePath, s.w); err != nil {
		return "", err
	}
	s.saved, s.savedRevision = true, s.w.Revision()
	s.say("Saved to %s. Resume with: go run ./cmd/web -load %s", s.savePath, s.savePath)
	return "/", nil
}

// --- progression ---------------------------------------------------------------

// next plays the waiting matchday, or goes to the club's next one.
func (s *server) next(url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	if _, ok := s.w.Pending(); ok {
		return "/", s.play()
	}
	return "/", s.advance()
}

// advance continues to the club's next matchday, resolving other clubs'
// matches on the way. Like the terminal client, it stops once the day
// before the contract-year end while players in their final year would
// leave, at the opening of a transfer window, and while a window is open
// after the first transfer run with news for the club.
func (s *server) advance() error {
	seen := s.lastInboxEvent()
	for {
		target, warn := s.w.Now()+400*sim.GameInstant(sim.Day), false
		end := s.w.ContractYearEnd()
		if stop := end - sim.GameInstant(sim.Day); s.warnedYearEnd != end && target >= end && stop > s.w.Now() && len(s.expiring()) > 0 {
			target, warn = stop, true
		}
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
		switch {
		case !ok && opening:
			s.say("The transfer window has opened: buy players on the Transfers page. Continue goes on to the next transfer news.")
			return nil
		case !ok && stepping && s.transferNews(seen):
			s.say("Transfer news: see the Inbox and the Transfers page.")
			return nil
		case !ok && stepping:
			continue
		case !ok && warn:
			s.warnedYearEnd = end
			s.say("%d of your players' contracts end tomorrow. Renew the ones you want to keep on the Squad page; the others leave as free agents.", len(s.expiring()))
			return nil
		case !ok:
			s.say("Nothing is scheduled before %s.", s.w.Calendar().Format(target))
			return nil
		case len(ready.UserFixtures) == 0:
			if _, err := s.resolve(); err != nil {
				return err
			}
			continue
		}
		info, _ := s.fixtureInfo(ready.UserFixtures[0])
		s.say("Matchday: %s v %s. Check your lineup, then play the match.", matchName(info), s.opponent(info.FixtureLine))
		return nil
	}
}

// play resolves the waiting matchday and keeps its report for the home page.
func (s *server) play() error {
	res, err := s.resolve()
	if err != nil {
		return err
	}
	s.report = s.reportOf(res)
	return nil
}

func (s *server) resolve() (app.RoundsResolved, error) {
	ready, ok := s.w.Pending()
	if !ok {
		return app.RoundsResolved{}, errors.New("no match is waiting")
	}
	cmd := app.ResolveRounds{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision()}
	for _, r := range ready.Rounds {
		cmd.Rounds = append(cmd.Rounds, r.Round)
	}
	return s.w.ResolveRounds(cmd)
}

// playSeason plays every remaining match of the club's season, with the AI's
// lineups except where one was submitted.
func (s *server) playSeason(url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	sc, ok := s.userSchedule()
	if !ok {
		return "", errors.New("your club has no season")
	}
	for {
		if _, ok := s.w.Pending(); ok {
			if err := s.play(); err != nil {
				return "", err
			}
			continue
		}
		if s.seasonDone(sc) {
			// Run the season end due now, which schedules the next
			// season and draws any cup it qualifies teams for.
			if _, err := s.w.Continue(s.w.Now()); err != nil {
				return "", err
			}
			break
		}
		res, err := s.w.Continue(s.w.Now() + 400*sim.GameInstant(sim.Day))
		if err != nil {
			return "", err
		}
		if _, ok := res.(app.FixtureRoundReady); !ok {
			return "", errors.New("no more matches are scheduled")
		}
	}
	s.say("%s season %d is finished. Continue takes you into the next one.", sc.CompetitionName, sc.Season)
	return "/table", nil
}

// --- lineup ------------------------------------------------------------------

// Lineup form values: for each squad player "slot-ID" is a role (gk, df, mf,
// fw) to start in, "bench" or "out". Starters are ordered goalkeeper,
// defenders, midfielders, forwards, then by ID.
var slotRoles = map[string]matches.Role{"gk": matches.Goalkeeper, "df": matches.Defender, "mf": matches.Midfielder, "fw": matches.Forward}

// slotName is the form value for starting in a role.
func slotName(r matches.Role) string {
	for name, role := range slotRoles {
		if role == r {
			return name
		}
	}
	return "out"
}

func (s *server) submitLineup(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	fixture, ok := s.pendingFixture()
	if !ok || form.Get("fixture") != strconv.FormatUint(uint64(fixture), 10) {
		return "/", errors.New("that match is no longer waiting")
	}
	var l selection.Lineup
	mentality, ok := parseMentality(form.Get("mentality"))
	if !ok {
		return "", errors.New("choose a mentality")
	}
	l.Tactics.Mentality = mentality
	squad, _ := s.w.Squad(s.club())
	for _, p := range squad {
		v := form.Get(fmt.Sprintf("slot-%d", p.Player))
		if role, ok := slotRoles[v]; ok {
			l.Starters = append(l.Starters, selection.Slot{Player: p.Player, Role: role})
		} else if v == "bench" {
			l.Bench = append(l.Bench, p.Player)
		}
	}
	slices.SortStableFunc(l.Starters, func(a, b selection.Slot) int { return cmp.Compare(a.Role, b.Role) })
	if n := len(l.Starters); n != matches.StartersPerTeam {
		return "", fmt.Errorf("pick exactly %d starters (you picked %d)", matches.StartersPerTeam, n)
	}
	if _, err := s.w.SubmitLineup(app.SubmitLineup{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Fixture: fixture, Lineup: l}); err != nil {
		return "", err
	}
	s.say("Lineup saved: it will be played. Press Play match when you are ready.")
	return "/lineup", nil
}

func parseMentality(v string) (matches.Mentality, bool) {
	for m := matches.Defensive; m <= matches.Attacking; m++ {
		if m.String() == v {
			return m, true
		}
	}
	return 0, false
}

// --- contracts ----------------------------------------------------------------

// offer reads "player", "years" and "wage" (whole units) from a form.
func offer(form url.Values) (ids.PlayerID, app.ContractOffer, error) {
	player, err := strconv.ParseUint(form.Get("player"), 10, 64)
	if err != nil || player == 0 {
		return 0, app.ContractOffer{}, errors.New("unknown player")
	}
	years, err := strconv.Atoi(form.Get("years"))
	if err != nil {
		return 0, app.ContractOffer{}, errors.New("the contract length must be a whole number of years")
	}
	units, err := strconv.ParseInt(strings.ReplaceAll(form.Get("wage"), ",", ""), 10, 64)
	if err != nil || units <= 0 || units > 1_000_000_000 {
		return 0, app.ContractOffer{}, errors.New("the weekly wage must be a positive whole amount")
	}
	return ids.PlayerID(player), app.ContractOffer{Years: years, WeeklyWage: money.Units(units)}, nil
}

func (s *server) renew(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	player, o, err := offer(form)
	if err != nil {
		return "", err
	}
	res, err := s.w.RenewContract(app.RenewContract{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Offer: o})
	if err != nil {
		return "", err
	}
	s.say("%s signed a new contract until %s at %s a week.", s.name(player), s.endDate(res.Contract.Expires), res.Contract.WeeklyWage)
	return "/squad", nil
}

func (s *server) sign(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	player, o, err := offer(form)
	if err != nil {
		return "", err
	}
	name := s.name(player) // read before the player joins
	res, err := s.w.SignPlayer(app.SignPlayer{ID: s.w.NextCommandID(), ExpectedRevision: s.w.Revision(), Player: player, Offer: o})
	if err != nil {
		return "", err
	}
	s.say("%s joined until %s at %s a week.", name, s.endDate(res.Contract.Expires), res.Contract.WeeklyWage)
	return "/squad", nil
}

func (s *server) release(form url.Values) (string, error) {
	if s.w == nil {
		return "", errors.New("choose a club first")
	}
	playerID, err := strconv.ParseUint(form.Get("player"), 10, 64)
	if err != nil || playerID == 0 {
		return "", errors.New("unknown player")
	}
	player := ids.PlayerID(playerID)
	name := s.name(player)
	res, err := s.w.ReleasePlayer(app.ReleasePlayer{
		ID:               s.w.NextCommandID(),
		ExpectedRevision: s.w.Revision(),
		Player:           player,
	})
	if err != nil {
		return "", err
	}
	s.say("%s was released and is now a free agent. You paid %s.", name, res.Compensation)
	return "/squad", nil
}
