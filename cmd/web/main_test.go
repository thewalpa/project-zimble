package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/storage"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// client drives a server as a browser would: GET pages, POST forms and
// follow the redirect to the resulting page.
type client struct {
	t   *testing.T
	s   *server
	srv *httptest.Server
}

func newClient(t *testing.T, cfg config) *client {
	t.Helper()
	s, err := newServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return &client{t: t, s: s, srv: srv}
}

func career(t *testing.T) *client {
	return newClient(t, config{seed: 42, club: 3, savePath: filepath.Join(t.TempDir(), "career.json")})
}

func (c *client) get(path string) string {
	c.t.Helper()
	res, err := c.srv.Client().Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.body(res, http.StatusOK)
}

// post submits a form from the current page: the world's revision is added
// unless the form sets one.
func (c *client) post(path string, form url.Values) string {
	c.t.Helper()
	form = maps.Clone(form)
	if form == nil {
		form = url.Values{}
	}
	if !form.Has("rev") && c.s.w != nil {
		form.Set("rev", strconv.FormatUint(uint64(c.s.w.Revision()), 10))
	}
	res, err := c.srv.Client().PostForm(c.srv.URL+path, form)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.body(res, http.StatusOK)
}

func (c *client) body(res *http.Response, status int) string {
	c.t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != status {
		c.t.Fatalf("%s %s: status %d, want %d\n%s", res.Request.Method, res.Request.URL.Path, res.StatusCode, status, b)
	}
	return string(b)
}

func contains(t *testing.T, page string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(page, w) {
			t.Fatalf("page missing %q; its notes: %q\nmain:\n%s", w, notesOf(page), mainOf(page))
		}
	}
}

// notesOf and mainOf keep failure output readable.
func notesOf(page string) []string {
	var out []string
	for _, part := range strings.Split(page, `<div class="note`)[1:] {
		part = part[strings.Index(part, ">")+1:]
		out = append(out, part[:strings.Index(part, "</div>")])
	}
	return out
}

func mainOf(page string) string {
	if i := strings.Index(page, "<main>"); i >= 0 {
		return page[i:]
	}
	return page
}

func TestNewCareerFromTheBrowser(t *testing.T) {
	c := newClient(t, config{seed: 42, savePath: filepath.Join(t.TempDir(), "c.json")})
	page := c.get("/")
	contains(t, page, "New career", "World seed 42", "Quillford FC", `name="club" value="3"`)
	contains(t, c.get("/squad"), "New career") // no career yet: every page offers the clubs
	page = c.post("/new", url.Values{"club": {"3"}})
	contains(t, page, "Welcome!", "Quillford FC", "Founders League season 1", "Next match", "Round 1 v Brackenmoor Town (home)", "Unsaved")
	contains(t, c.post("/new", url.Values{"club": {"4"}}), "a career is already under way")
	if club, _ := c.s.w.UserClub(); club != 3 {
		t.Fatalf("managing club %d", club)
	}
	bad := newClient(t, config{seed: 42})
	contains(t, bad.post("/new", url.Values{"club": {"99"}}), "unknown club", "New career")
}

// Continue goes to the matchday; the lineup form edits the suggestion and
// submits it as the club's lineup; Play match plays it and reports the
// result.
func TestPlayingAMatchday(t *testing.T) {
	c := career(t)
	page := c.post("/continue", nil)
	contains(t, page, "Matchday: Round 1 v Brackenmoor Town (home)", "Pick lineup", "Play match")

	page = c.get("/lineup")
	if n := strings.Count(page, `name="slot-`); n != 20 {
		t.Fatalf("%d players in the lineup form", n)
	}
	contains(t, page, "The assistant&#39;s suggestion")
	fixture, _ := c.s.pendingFixture()
	suggested, err := c.s.w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	form := lineupForm(c, fixture, "attacking")
	page = c.post("/lineup", form)
	contains(t, page, "Lineup saved", "Your saved lineup", `<option value="attacking" selected>`)
	got, ok := c.s.w.SubmittedLineup(fixture)
	if !ok || got.Tactics.Mentality != matches.Attacking || len(got.Starters) != 11 || len(got.Bench) != len(suggested.Bench) {
		t.Fatalf("submitted %+v, %v", got, ok)
	}

	page = c.post("/continue", nil)
	contains(t, page, "Latest result", "Brackenmoor Town", "Other results")
	if c.s.report == nil || !strings.Contains(page, c.s.report.Title) {
		t.Fatalf("report %+v not shown", c.s.report)
	}
	if _, pending := c.s.w.Pending(); pending {
		t.Fatal("the matchday is still waiting")
	}
	contains(t, c.get("/fixtures"), `class="pill `)
	contains(t, c.get("/table"), "1 of 14 rounds played")
}

// lineupForm is the lineup page's form for the suggested lineup with
// another mentality.
func lineupForm(c *client, fixture any, mentality string) url.Values {
	c.t.Helper()
	f, _ := c.s.pendingFixture()
	l, err := c.s.w.SuggestLineup(f)
	if err != nil {
		c.t.Fatal(err)
	}
	form := url.Values{"fixture": {fmt.Sprint(fixture)}, "mentality": {mentality}}
	squad, _ := c.s.w.Squad(c.s.club())
	for _, p := range squad {
		form.Set(fmt.Sprintf("slot-%d", p.Player), "out")
	}
	for _, st := range l.Starters {
		form.Set(fmt.Sprintf("slot-%d", st.Player), slotName(st.Role))
	}
	for _, p := range l.Bench {
		form.Set(fmt.Sprintf("slot-%d", p), "bench")
	}
	return form
}

func TestLineupRejections(t *testing.T) {
	c := career(t)
	contains(t, c.get("/lineup"), "No match is waiting")
	contains(t, c.post("/lineup", url.Values{"fixture": {"1"}}), "that match is no longer waiting")
	c.post("/continue", nil)
	fixture, _ := c.s.pendingFixture()
	f, _ := c.s.w.SuggestLineup(fixture)
	cases := map[string]func(url.Values){
		"pick exactly 11 starters (you picked 10)": func(v url.Values) { v.Set(fmt.Sprintf("slot-%d", f.Starters[5].Player), "out") },
		"2 starting goalkeepers": func(v url.Values) {
			v.Set(fmt.Sprintf("slot-%d", f.Starters[5].Player), "gk")
		},
		"choose a mentality":              func(v url.Values) { v.Set("mentality", "reckless") },
		"that match is no longer waiting": func(v url.Values) { v.Set("fixture", "999") },
		"exceeds 7": func(v url.Values) { // every unselected player to the bench
			for k, vals := range v {
				if vals[0] == "out" {
					v.Set(k, "bench")
				}
			}
		},
	}
	for want, mutate := range cases {
		form := lineupForm(c, fixture, "balanced")
		mutate(form)
		page := c.post("/lineup", form)
		contains(t, page, want)
		if _, ok := c.s.w.SubmittedLineup(fixture); ok {
			t.Fatalf("%s: a lineup was submitted", want)
		}
	}
}

// A form from an out-of-date page changes nothing; neither does a request
// another site makes the browser send.
func TestStaleFormsAndCrossSiteRequests(t *testing.T) {
	c := career(t)
	old := strconv.FormatUint(uint64(c.s.w.Revision()), 10)
	c.post("/continue", nil)
	now, rev := c.s.w.Now(), c.s.w.Revision()
	for _, path := range []string{"/continue", "/season", "/save"} {
		contains(t, c.post(path, url.Values{"rev": {old}}), "the page was out of date")
	}
	contains(t, c.post("/continue", url.Values{"rev": {"x"}}), "the page was out of date")
	if c.s.w.Now() != now || c.s.w.Revision() != rev || c.s.saved {
		t.Fatal("a stale form changed the career")
	}

	for name, header := range map[string][2]string{
		"cross-site fetch": {"Sec-Fetch-Site", "cross-site"},
		"foreign origin":   {"Origin", "http://evil.example"},
	} {
		req, _ := http.NewRequest(http.MethodPost, c.srv.URL+"/continue", strings.NewReader("rev="+strconv.FormatUint(uint64(rev), 10)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(header[0], header[1])
		res, err := c.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		c.body(res, http.StatusForbidden)
		if c.s.w.Revision() != rev {
			t.Fatalf("%s: the request changed the career", name)
		}
	}
	res, err := c.srv.Client().Get(c.srv.URL + "/continue")
	if err != nil {
		t.Fatal(err)
	}
	c.body(res, http.StatusMethodNotAllowed)
	res, _ = c.srv.Client().Get(c.srv.URL + "/nowhere")
	c.body(res, http.StatusNotFound)
}

// The contract flow of the terminal client, in the browser: the stop the
// day before the contract-year end, renewals, the contract year, and
// signings refused on a matchday and accepted after it.
func TestContractsInTheBrowser(t *testing.T) {
	c := career(t)
	contains(t, c.post("/season", nil), "Founders League season 1 is finished", "final table")
	page := c.post("/continue", nil)
	contains(t, page, "7 of your players&#39; contracts end tomorrow", "Tue 2026-06-30 00:00 UTC")
	page = c.get("/squad")
	if n := strings.Count(page, `action="/renew"`); n != 7 {
		t.Fatalf("%d renewal forms, want 7", n)
	}
	offer, err := c.s.w.SuggestContract(56)
	if err != nil {
		t.Fatal(err)
	}
	renew := url.Values{"player": {"56"}, "years": {strconv.Itoa(offer.Years)}, "wage": {strconv.FormatInt(int64(offer.WeeklyWage)/100, 10)}}
	contains(t, c.post("/renew", renew), "Callum Ibsen signed a new contract until 1 July 2028 at 2,570.00 a week.")
	contains(t, c.post("/renew", url.Values{"player": {"45"}, "years": {"2"}, "wage": {"1"}}), "contract offer rejected: weekly wage 1.00")
	contains(t, c.post("/renew", url.Values{"player": {"45"}, "years": {"two"}, "wage": {"1"}}), "whole number of years")
	contains(t, c.post("/renew", url.Values{"player": {"44"}, "years": {"2"}, "wage": {"3,000"}}), "the contract is not in its final year")

	page = c.post("/continue", nil) // the contract year opens the transfer window
	contains(t, page, "The transfer window has opened", "Oscar Bellamy left the club as a free agent")
	page = c.post("/continue", nil) // no transfer news: the first matchday of season 2
	contains(t, page, "Matchday: Round 1 v Greyfen United (away)")
	page = c.get("/free") // AI clubs signed the others in the transfer window
	contains(t, page, "Kofi Doyle", "Signings wait until the matchday has been played", "disabled")
	sign := url.Values{"player": {"178"}, "years": {"1"}, "wage": {"940"}}
	contains(t, c.post("/sign", sign), "squads cannot change while rounds await results")
	c.post("/continue", nil)
	contains(t, c.post("/sign", sign), "Kofi Doyle joined until 1 July 2027 at 940.00 a week.")
	contains(t, c.get("/squad"), "Kofi Doyle")
	contains(t, c.post("/sign", sign), "the player is not a free agent")
}

func TestSaveAndLoad(t *testing.T) {
	c := career(t)
	c.post("/continue", nil)
	page := c.post("/save", nil)
	contains(t, page, "Saved to "+c.s.savePath)
	if strings.Contains(page, ">Unsaved<") {
		t.Fatal("the page still says unsaved")
	}
	w, err := storage.Load(c.s.savePath)
	if err != nil || w.Revision() != c.s.w.Revision() {
		t.Fatalf("load: %v", err)
	}
	loaded := newClient(t, config{loadPath: c.s.savePath, savePath: c.s.savePath})
	if loaded.s.seed != 42 || loaded.s.w.Revision() != c.s.w.Revision() {
		t.Fatalf("loaded seed %d revision %d", loaded.s.seed, loaded.s.w.Revision())
	}
	contains(t, loaded.get("/"), "Round 1 v Brackenmoor Town (home)", "Play match")

	plain := filepath.Join(t.TempDir(), "plain.json")
	nobody, err := app.NewWorld(app.DefaultConfig(random.Seed(1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Save(plain, nobody); err != nil {
		t.Fatal(err)
	}
	if _, err := newServer(config{loadPath: plain}); err == nil || !strings.Contains(err.Error(), "no managed club") {
		t.Fatalf("loading a career without a club: %v", err)
	}
}

func TestSaveSelectorOnStartupAndMultipleSaves(t *testing.T) {
	dir := t.TempDir()

	// 1. Create two saves in dir
	cfg1 := app.DefaultConfig(random.Seed(42))
	cfg1.UserClub = ids.ClubID(3)
	w1, err := app.NewWorld(cfg1)
	if err != nil {
		t.Fatal(err)
	}
	save1 := filepath.Join(dir, "quillford.json")
	if err := storage.Save(save1, w1); err != nil {
		t.Fatal(err)
	}

	cfg2 := app.DefaultConfig(random.Seed(42))
	cfg2.UserClub = ids.ClubID(4)
	w2, err := app.NewWorld(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	save2 := filepath.Join(dir, "brackenmoor.json")
	if err := storage.Save(save2, w2); err != nil {
		t.Fatal(err)
	}

	// 2. Start server without -load (s.w == nil) pointing savesDir to dir
	c := newClient(t, config{savesDir: dir})
	startPage := c.get("/")
	contains(t, startPage, "Saved careers", "quillford.json", "Quillford FC", "brackenmoor.json", "Brackenmoor Town", "Load career", "New career")

	// 3. Load quillford save
	page := c.post("/load", url.Values{"file": {"quillford.json"}})
	contains(t, page, "Loaded quillford.json (Quillford FC", "Round 1 v Brackenmoor Town (home)", "Active file: <b>quillford.json</b>", "brackenmoor.json")

	// 4. Save as a new copy: "season-split"
	page = c.post("/save", url.Values{"name": {"season-split"}})
	contains(t, page, "Saved to "+filepath.Join(dir, "season-split.json"))
	if _, err := os.Stat(filepath.Join(dir, "season-split.json")); err != nil {
		t.Fatalf("expected season-split.json to exist: %v", err)
	}

	// 5. Verify the active file is now season-split.json and quillford.json is in other saves
	page = c.get("/")
	contains(t, page, "Active file: <b>season-split.json</b>", "quillford.json", "brackenmoor.json")

	// 6. Test path traversal protection
	errPage := c.post("/load", url.Values{"file": {"../../etc/passwd"}})
	contains(t, errPage, "invalid save file path")

	errPage = c.post("/save", url.Values{"name": {"../../evil"}})
	contains(t, errPage, "invalid save name")

	// 7. Switch career directly to brackenmoor.json
	page = c.post("/load", url.Values{"file": {"brackenmoor.json"}})
	contains(t, page, "Loaded brackenmoor.json (Brackenmoor Town", "Active file: <b>brackenmoor.json</b>")
}

// Every page renders at every stage of a career: a matchday, after it, the
// off-season and after the player year (with retirements and youth).
func TestEveryPageRenders(t *testing.T) {
	c := career(t)
	pages := []string{"/", "/squad", "/lineup", "/table", "/fixtures", "/free", "/inbox", "/finances", "/report"}
	check := func(stage string) {
		for _, p := range pages {
			page := c.get(p)
			if !strings.Contains(page, "</html>") || strings.Contains(page, "template:") {
				t.Fatalf("%s at %s did not render:\n%s", p, stage, page)
			}
		}
	}
	check("the start")
	c.post("/continue", nil)
	check("a matchday")
	c.post("/continue", nil)
	check("after a match")
	for range 2 {
		c.post("/season", nil)
		check("the off-season")
		c.post("/continue", nil) // the contract stop
		c.post("/continue", nil) // the contract year and the next matchday
		check("a new season")
	}
	contains(t, c.get("/inbox"), "retired at 36", "joined from the youth ranks", "Development:")
}

func TestFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	var addr string
	var handler http.Handler
	serve := func(a string, h http.Handler) error { addr, handler = a, h; return nil }
	seed := func() (uint64, error) { return 7, nil }
	if err := run([]string{"-club", "2", "-addr", "127.0.0.1:9999"}, &out, &errOut, seed, serve); err != nil {
		t.Fatal(err)
	}
	s := handler.(*server)
	if club, _ := s.w.UserClub(); addr != "127.0.0.1:9999" || s.seed != 7 || club != 2 || s.savePath != "career.json" {
		t.Fatalf("addr %s seed %d club %d save %s", addr, s.seed, club, s.savePath)
	}
	contains(t, out.String(), "http://127.0.0.1:9999", "seed 7")
	for _, args := range [][]string{{"-load", "x.json", "-seed", "1"}, {"extra"}, {"-load", filepath.Join(t.TempDir(), "missing.json")}} {
		if err := run(args, &out, &errOut, seed, serve); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	failing := func() (uint64, error) { return 0, errors.New("no entropy") }
	if err := run(nil, &out, &errOut, failing, serve); err == nil {
		t.Error("a failed seed draw was ignored")
	}
}

func TestOtherTeamSquads(t *testing.T) {
	c := career(t)
	squadPage := c.get("/squad")
	contains(t, squadPage, "Squad", "QUI (You)")

	tablePage := c.get("/table")
	contains(t, tablePage, `<a href="/squad?club=1">`, `<a href="/squad?club=2">`, `<a href="/squad?club=3">`)

	club1Page := c.get("/squad?club=1")
	contains(t, club1Page, "Hollowick Town squad", "GK", "DEF", "PAS", "FIN", "PAC", "STA")
	if strings.Contains(club1Page, `action="/renew"`) {
		t.Fatal("other club's squad has renewal form")
	}
	contains(t, club1Page, `href="/squad?club=3"`)
}

func TestGameReportsWhenClickingOnScores(t *testing.T) {
	c := career(t)
	c.post("/continue", nil)
	homePage := c.post("/continue", nil)

	contains(t, homePage, "Latest result", `<a href="/report?fixture=`)
	if c.s.report == nil {
		t.Fatal("expected match report")
	}

	fixturesPage := c.get("/fixtures")
	contains(t, fixturesPage, fmt.Sprintf(`<a href="/report?fixture=%d">`, c.s.report.Fixture))

	reportPage := c.get(fmt.Sprintf("/report?fixture=%d", c.s.report.Fixture))
	contains(t, reportPage, "Game report", "Founders League", "Round 1", "Goals", "Back to fixtures", "League table")

	for _, other := range c.s.report.Others {
		otherPage := c.get(fmt.Sprintf("/report?fixture=%d", other.Fixture))
		contains(t, otherPage, "Game report", "Round 1", "Back to fixtures")
	}
}

// A cup run in the browser: the cup page before and after the draw, the
// cup tie as the next match and the matchday, a win on penalties, the
// bracket, the club's fixtures and both league tables.
func TestCupInTheBrowser(t *testing.T) {
	c := newClient(t, config{seed: 42, club: 4, savePath: filepath.Join(t.TempDir(), "career.json")}) // Brackenmoor Town
	contains(t, c.get("/cup"), "No edition has been drawn yet")
	c.post("/season", nil)
	contains(t, c.get("/"), "Next match", "Continental Cup quarter-final v Foxmere Town (away)")
	contains(t, c.post("/continue", nil), "Matchday: Continental Cup quarter-final v Foxmere Town (away).")
	contains(t, c.get("/lineup"), "Continental Cup quarter-final v Foxmere Town (away)")
	c.post("/continue", nil) // the quarter-final
	c.post("/continue", nil) // to the semi-final
	page := c.post("/continue", nil)
	contains(t, page, "Glenrock Town 0-0 Brackenmoor Town (5-4 on penalties)", `class="pill L"`)
	contains(t, c.get("/cup"), "Continental Cup 1", "Quarter-finals", "Semi-finals", "Final", "0-0 (5-4 on penalties)", "Your club is in it.")
	contains(t, c.get("/fixtures"), "Continental Cup quarter-final", "Continental Cup semi-final")
	contains(t, c.get("/table"), "Founders League season 1", "Harbour League season 1")
	c.post("/continue", nil) // the final, without the club
	contains(t, c.get("/inbox"), "Continental Cup 1 won by Glenrock Town; you went out in the semi-final.", "(4-5 on penalties) v Glenrock Town (away), Continental Cup semi-final")
	contains(t, c.get("/cup"), "Won by <b>Glenrock Town</b>")

	// Eldhaven United (club 6) wins it.
	c = newClient(t, config{seed: 42, club: 6, savePath: filepath.Join(t.TempDir(), "career.json")})
	c.post("/season", nil)
	for range 7 { // the quarter-final, semi-final and final, each a matchday and a match; then the cup ends
		c.post("/continue", nil)
	}
	contains(t, c.get("/inbox"), "Continental Cup 1 won by Eldhaven United: your club won it!")
}

// In the window the manager bids from the market, the answer arrives with
// Continue, and an AI club's bid for one of the manager's players is
// answered on the Transfers page.
func TestTransfersInTheBrowser(t *testing.T) {
	c := career(t)
	c.post("/season", nil)
	c.post("/continue", nil)
	page := c.post("/continue", nil)
	contains(t, page, "The transfer window has opened", `href="/transfers"`)
	page = c.get("/transfers?pos=FW")
	contains(t, page, "Jonas Gallo", "240,000.00", `action="/bid"`)
	if strings.Contains(page, "Your squad is full") {
		t.Fatal("the manager has room at FW after the contract year")
	}
	bid := func(player, fee string) string {
		o, err := c.s.w.SuggestContract(ids.PlayerID(mustAtoi(t, player)))
		if err != nil {
			t.Fatal(err)
		}
		return c.post("/bid", url.Values{"player": {player}, "fee": {fee}, "years": {strconv.Itoa(o.Years)},
			"wage": {strconv.FormatInt(int64(o.WeeklyWage)/100, 10)}, "back": {"/transfers"}})
	}
	for _, p := range []string{"282", "244", "315"} {
		bid(p, strconv.FormatInt(int64(valueOf(t, c, p))/100, 10))
	}
	contains(t, bid("37", "240,000"), "You bid 240,000.00 for Jonas Gallo", "Your bids awaiting an answer")
	contains(t, bid("238", "500000"), "You bid 500,000.00")
	contains(t, bid("238", "700000"), "your club has already bid for the player in this window")
	contains(t, bid("37", "abc"), "the fee must be a positive whole amount")

	page = c.post("/continue", nil)
	contains(t, page, "Transfer news", "Jonas Gallo joined from Saltmere Athletic for 240,000.00",
		"Your bid of 500,000.00 for Oscar Adeyemi of Ironbridge Wanderers was rejected")
	// AI clubs bid for the manager's players only when he lists them.
	contains(t, c.post("/list", url.Values{"player": {"44"}, "asking": {strconv.FormatInt(int64(valueOf(t, c, "44"))/100, 10)}, "back": {"/squad"}}),
		"Elias Gallo is on the transfer list")
	page = c.post("/continue", nil)
	contains(t, page, "Hollowick Town bid 1,200,000.00 for Elias Gallo", "1 bids for your players await your answer")
	page = c.get("/transfers")
	contains(t, page, "Bids for your players", `action="/answer"`, "Transfers in this window")
	contains(t, c.post("/answer", url.Values{"offer": {"99"}, "accept": {"yes"}, "back": {"/transfers"}}), "no open offer for one of your players")
	contains(t, c.post("/answer", url.Values{"offer": {"49"}, "accept": {"yes"}, "back": {"/transfers"}}), "Accepted: the transfer is complete.")
	contains(t, c.get("/inbox"), "Elias Gallo left for Hollowick Town for 1,200,000.00")
	contains(t, c.get("/finances"), "transfer fee, offer 49")
	contains(t, c.get("/squad?club=1"), "asking price")
}

// The manager lists and unlists players through the squad page. The
// transfer page shows every listed player, offers bids for other clubs'
// players, and marks listed players in the wider market.
func TestTransferListInTheBrowser(t *testing.T) {
	c := career(t)
	page := c.get("/squad")
	contains(t, page, `action="/list"`, `aria-label="Asking price for Callum Ibsen"`, ">List</button>")

	page = c.post("/list", url.Values{"player": {"56"}, "asking": {"700,000"}, "back": {"/squad"}})
	contains(t, page, "Callum Ibsen is on the transfer list at 700,000.00", "Listed at 700,000.00", ">Change</button>", ">Unlist</button>")
	page = c.get("/transfers")
	contains(t, page, "Transfer list", "Quillford FC (You)", "Callum Ibsen", "700,000.00", ">Unlist</button>",
		"You receive bids only for players you put on the transfer list")

	page = c.post("/list", url.Values{"player": {"56"}, "unlist": {"yes"}, "back": {"/transfers"}})
	contains(t, page, "Callum Ibsen is off the transfer list", "No players are on the transfer list")
	c.post("/list", url.Values{"player": {"56"}, "asking": {"700000"}, "back": {"/squad"}})

	// A player at the positional minimum cannot be listed.
	minimum := career(t)
	minimum.post("/release", url.Values{"player": {"41"}, "back": {"/squad"}})
	contains(t, minimum.post("/list", url.Values{"player": {"42"}, "asking": {"500000"}, "back": {"/squad"}}),
		"the squad would fall below its minimum at that position: 2 GK, minimum 2")

	page = c.post("/continue", nil)
	contains(t, page, "Hollowick Town bid 700,000.00 for Callum Ibsen")
	var other app.ListedPlayer
	for day := 0; day < 3 && other.Player == 0; day++ {
		for _, listed := range c.s.w.TransferList() {
			if listed.Club != c.s.club() {
				other = listed
				break
			}
		}
		if other.Player == 0 {
			c.post("/continue", nil)
		}
	}
	if other.Player == 0 {
		t.Fatal("AI clubs put no player on the transfer list")
	}
	page = c.get("/transfers?pos=" + other.Position.String())
	contains(t, page, other.ClubName, other.Name, `action="/bid"`,
		other.Name+`</a> <span class="muted">listed</span>`)
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// valueOf is a player's asking price.
func valueOf(t *testing.T, c *client, player string) money.Money {
	t.Helper()
	for _, row := range c.s.w.Summary().ClubRows {
		squad, _ := c.s.w.Squad(row.ID)
		for _, p := range squad {
			if strconv.FormatUint(uint64(p.Player), 10) == player {
				return p.Value
			}
		}
	}
	t.Fatalf("player %s is at no club", player)
	return 0
}

func TestSortableLists(t *testing.T) {
	c := career(t)

	// 1. Squad sorting
	squadOvrDesc := c.get("/squad?sort=ovr&dir=desc")
	contains(t, squadOvrDesc, "data-col=\"ovr\"", "sort-icon desc")

	squadNameAsc := c.get("/squad?sort=name&dir=asc")
	contains(t, squadNameAsc, "data-col=\"name\"", "sort-icon asc")

	// 2. Table sorting
	tableClub := c.get("/table?sort=club&dir=asc")
	contains(t, tableClub, "data-col=\"club\"", "sort-icon asc")

	// 3. Fixtures sorting
	fixturesOpp := c.get("/fixtures?sort=opp&dir=asc")
	contains(t, fixturesOpp, "data-col=\"opp\"", "sort-icon asc")

	// 4. Finances sorting
	financesAmount := c.get("/finances?sort=amount&dir=desc")
	contains(t, financesAmount, "data-col=\"amount\"", "sort-icon desc")

	// 5. Transfers market sorting
	transfersPrice := c.get("/transfers?sort=price&dir=desc")
	contains(t, transfersPrice, "data-col=\"price\"")

	// 6. Choose club sorting (no career)
	cNoClub := newClient(t, config{seed: 42, savePath: filepath.Join(t.TempDir(), "new.json")})
	chooseName := cNoClub.get("/?sort=name&dir=asc")
	contains(t, chooseName, "data-col=\"name\"", "sort-icon asc")

	// 7. Lineup sorting (when pending match)
	c.post("/continue", nil) // moves to matchday
	lineupOvr := c.get("/lineup?sort=ovr&dir=desc")
	contains(t, lineupOvr, "data-col=\"ovr\"", "sort-icon desc")

	// 8. Inbox sorting (after playing match)
	c.post("/continue", nil) // plays match
	inboxDate := c.get("/inbox?sort=when&dir=asc")
	contains(t, inboxDate, "data-col=\"when\"")
}

// Releasing a player shows the payoff and confirmation in the squad table;
// releasing pays the payoff, records it in finances and inbox, and frees the
// player. Squad limit is checked for bidding and signing.
func TestReleaseAndSquadLimitInTheBrowser(t *testing.T) {
	c := career(t)

	// Squad page shows squad limit and payoff preview with confirmation.
	page := c.get("/squad")
	contains(t, page, "You have 20 players; a squad holds at most 25, and at least 2 GK, 5 DF, 5 MF, 3 FW.",
		`action="/release"`, `confirm('Release Rafael Okafor for 179,400.00?');`, "Release (179,400.00)")

	// Transfers and squad limit:
	// Club 3 starts with 20 players and 4 forwards.
	// Transfer window is open at instant 0.
	squad, _ := c.s.w.Squad(c.s.club())
	if len(squad) != 20 {
		t.Fatalf("expected 20 players, got %d", len(squad))
	}
	forwards := 0
	for _, p := range squad {
		if p.Position.String() == "FW" {
			forwards++
		}
	}
	if forwards != 4 {
		t.Fatalf("expected 4 forwards, got %d", forwards)
	}

	// With 20 players, club 3 can bid for a forward even though it already has 4.
	fwPage := c.get("/transfers?pos=FW")
	contains(t, fwPage, `action="/bid"`)
	if strings.Contains(fwPage, "Your squad is full") {
		t.Fatal("expected room to bid for forward with 20 players")
	}
	if strings.Contains(fwPage, "<button disabled>Bid</button>") {
		t.Fatal("bid button should be enabled with 20 players")
	}

	// Bid for players from clubs above their position minimum to grow squad to 25 (the SquadLimit).
	bidPlayer := func(p ids.PlayerID) {
		o, err := c.s.w.SuggestContract(p)
		if err != nil {
			t.Fatal(err)
		}
		res := c.post("/bid", url.Values{
			"player": {strconv.FormatUint(uint64(p), 10)},
			"fee":    {strconv.FormatInt(int64(valueOf(t, c, strconv.FormatUint(uint64(p), 10)))/100, 10)},
			"years":  {strconv.Itoa(o.Years)},
			"wage":   {strconv.FormatInt(int64(o.WeeklyWage)/100, 10)},
			"back":   {"/transfers"},
		})
		contains(t, res, "You bid")
	}

	// AI clubs trade in the window too: players who moved in it, already
	// have a bid awaiting an answer, or were bid for by the club are taken.
	takenPlayers := func() map[ids.PlayerID]bool {
		opens := c.s.w.TransferWindow().Opens
		taken := map[ids.PlayerID]bool{}
		for _, o := range c.s.w.Offers() {
			if o.MadeAt >= opens && (o.Buyer == c.s.club() || o.Status == transfers.StatusOpen || o.Status == transfers.StatusCompleted) {
				taken[o.Player] = true
			}
		}
		return taken
	}
	cheapestTarget := func() ids.PlayerID {
		taken := takenPlayers()
		var cands []app.SquadPlayer
		for _, cr := range c.s.w.Summary().ClubRows {
			if cr.ID == c.s.club() {
				continue
			}
			otherSquad, _ := c.s.w.Squad(cr.ID)
			counts := map[string]int{}
			for _, p := range otherSquad {
				counts[p.Position.String()]++
			}
			for _, p := range otherSquad {
				if p.Position.String() == "GK" || taken[p.Player] {
					continue
				}
				if counts[p.Position.String()] > c.s.w.Content().Quota(p.Position).Min {
					cands = append(cands, p)
				}
			}
		}
		if len(cands) == 0 {
			t.Fatal("no player for sale")
		}
		return slices.MinFunc(cands, func(a, b app.SquadPlayer) int { return int(a.Value - b.Value) }).Player
	}

	for {
		sq, _ := c.s.w.Squad(c.s.club())
		if len(sq) >= 25 {
			break
		}
		bidPlayer(cheapestTarget())
		c.post("/continue", nil)
	}

	squad, _ = c.s.w.Squad(c.s.club())
	if len(squad) != 25 {
		t.Fatalf("expected squad at limit of 25, got %d", len(squad))
	}

	// At 25 players, the Bid buttons are disabled and squad full message is shown.
	fullPage := c.get("/transfers?pos=FW")
	contains(t, fullPage, "Your squad is full: a squad holds at most 25 players.")
	if !strings.Contains(fullPage, "<button disabled>Bid</button>") {
		t.Fatal("expected bid button to be disabled at 25 players")
	}
	if strings.Contains(fullPage, "<button >Bid</button>") || strings.Contains(fullPage, "<button>Bid</button>") {
		t.Fatal("found enabled bid button when squad is full")
	}

	// Releasing player 60 (Rafael Okafor) drops squad to 24.
	page = c.post("/release", url.Values{"player": {"60"}})
	contains(t, page, "Rafael Okafor was released and is now a free agent. You paid 179,400.00.")

	// Rafael Okafor is now on the free agents page with an active Sign button (since 24 < 25).
	freePage := c.get("/free")
	contains(t, freePage, "Rafael Okafor", "<button >Sign</button>")

	// Finances shows the contract payoff with the player's name and amount.
	contains(t, c.get("/finances"), "contract payoff, Rafael Okafor", "-179,400.00")

	// Inbox shows the release message.
	contains(t, c.get("/inbox"), "Rafael Okafor left the club as a free agent; you paid 179,400.00")

	// Sign Rafael Okafor back to return squad to 25.
	o, err := c.s.w.SuggestContract(ids.PlayerID(60))
	if err != nil {
		t.Fatal(err)
	}
	c.post("/sign", url.Values{
		"player": {"60"},
		"years":  {strconv.Itoa(o.Years)},
		"wage":   {strconv.FormatInt(int64(o.WeeklyWage)/100, 10)},
		"back":   {"/free"},
	})

	// Release goalkeeper 43 to have a free agent while bringing the squad back
	// to 25 via another bid. (A goalkeeper, because AI clubs sign free agents
	// for their vacancies in the window, and rarely need one.)
	c.post("/release", url.Values{"player": {"43"}})
	// Bid for another player from club 13 to fill back to 25:
	sq13, _ := c.s.w.Squad(ids.ClubID(13))
	taken := takenPlayers()
	bidPlayer(sq13[slices.IndexFunc(sq13, func(p app.SquadPlayer) bool { return p.Position.String() != "GK" && !taken[p.Player] })].Player)
	c.post("/continue", nil)

	squad, _ = c.s.w.Squad(c.s.club())
	if len(squad) != 25 {
		t.Fatalf("expected squad at limit of 25, got %d", len(squad))
	}

	// Free agents page reflects squad full.
	freeFull := c.get("/free")
	contains(t, freeFull, "Gareth Gallo", "squad full")
	if strings.Contains(freeFull, "Sign</button>") {
		t.Fatal("found sign button when squad is full")
	}

	// Squad minimum rejection: club 3 is down to 2 GKs (minimum 2), so releasing another is refused.
	page = c.post("/release", url.Values{"player": {"41"}})
	contains(t, page, "the squad would fall below its minimum at that position: 2 GK, minimum 2")

	// Releasing on matchday is refused.
	c.post("/continue", nil) // moves past window to matchday
	page = c.post("/release", url.Values{"player": {"41"}})
	contains(t, page, "squads cannot change while rounds await results")
	c.post("/continue", nil) // play match

	// Squad sorting by payoff.
	squadPayoff := c.get("/squad?sort=payoff&dir=desc")
	contains(t, squadPayoff, "data-col=\"payoff\"")
}

func TestFullWidthLayout(t *testing.T) {
	c := career(t)
	page := c.get("/")
	if strings.Contains(page, "max-width: 1100px") || strings.Contains(page, "margin: 0 auto") {
		t.Fatal("layout still contains centered max-width constraint")
	}
	contains(t, page, ".bar { padding: 12px 16px;", "nav { padding: 0 16px;", "main { padding: 20px 16px 48px;")
}

func TestLineupCarriesOverInTheBrowser(t *testing.T) {
	c := career(t)
	// Advance to Round 1 matchday.
	page := c.post("/continue", nil)
	contains(t, page, "Matchday: Round 1 v Brackenmoor Town (home)", "Lineup: The assistant&#39;s suggestion")

	// Submit changed lineup for Round 1.
	fixture, _ := c.s.pendingFixture()
	form := lineupForm(c, fixture, "attacking")
	page = c.post("/lineup", form)
	contains(t, page, "Lineup saved", "Your saved lineup for this match")

	// Play Round 1.
	page = c.post("/continue", nil)
	contains(t, page, "Latest result")

	// Release starter 44 (Elias Gallo) between rounds.
	page = c.post("/release", url.Values{"player": {"44"}})
	contains(t, page, "Elias Gallo was released")

	// Advance to Round 2 matchday.
	page = c.post("/continue", nil)
	contains(t, page, "Matchday: Round 2 v Hollowick Town (away)",
		"Lineup: Carried over from the last match (vs Brackenmoor Town)",
		"Elias Gallo has left the club;",
		"takes his place",
	)

	// Check /lineup page:
	lineupPage := c.get("/lineup")
	contains(t, lineupPage,
		"Carried over from the last match (vs Brackenmoor Town)",
		"Elias Gallo has left the club;",
		"takes his place",
		"Ask the assistant",
	)

	// Check /lineup?suggest=1 ("Ask the assistant"):
	suggestPage := c.get("/lineup?suggest=1")
	contains(t, suggestPage,
		"The assistant&#39;s suggestion (used unless you save changes)",
		"Your saved lineup",
	)

	// Play Round 2 without submitting:
	r2Fixture, _ := c.s.pendingFixture()
	page = c.post("/continue", nil)
	contains(t, page, "Latest result")

	// Report shows "Your lineup":
	reportPage := c.get(fmt.Sprintf("/report?fixture=%d", r2Fixture))
	contains(t, reportPage, "Your lineup")
}

func TestPlayerProfilePage(t *testing.T) {
	c := career(t)
	contains(t, c.get("/squad"), `href="/player?id=56"`)
	contains(t, c.get("/player?id=56"), "Callum Ibsen", "(You)", "Contract:", "until", "Value:", "Cond")
	// A player of another club shows that club, not "(You)".
	other := c.get("/player?id=1")
	contains(t, other, "</html>")
	if strings.Contains(other, "(You)") {
		t.Fatal("another club's player was marked as ours")
	}
	contains(t, c.get("/player?id=99999"), "There is no player 99999")
	contains(t, c.get("/player?id=x"), "is not a player ID")
}

// History lists the champions of past seasons and shows any season's final
// table or bracket.
func TestHistoryInTheBrowser(t *testing.T) {
	c := newClient(t, config{seed: 42, club: 6, savePath: filepath.Join(t.TempDir(), "career.json")})
	contains(t, c.get("/history"), "Founders League", "in progress")
	c.post("/season", nil)
	for range 7 {
		c.post("/continue", nil)
	}
	page := c.get("/history")
	contains(t, page, "Founders League", "Continental Cup", "Eldhaven United", "/history?competition=")
	m := regexp.MustCompile(`href="(/history\?competition=[^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(m) == 0 {
		t.Fatal("no season links")
	}
	var all string
	for _, l := range m {
		all += c.get(strings.ReplaceAll(l[1], "&amp;", "&"))
	}
	contains(t, all, "final table", "Quarter-finals", "Won by <b>Eldhaven United</b>", "← All seasons")
	res, err := c.srv.Client().Get(c.srv.URL + "/history?competition=999&season=9")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == http.StatusOK {
		t.Fatal("an unknown season should not render")
	}
}

// New messages are unread; acknowledging one lowers the count, survives a
// save and reload, and a stale form changes nothing.
func TestInboxReadState(t *testing.T) {
	c := career(t)
	c.post("/continue", nil)
	c.post("/continue", nil) // plays the first match: new messages arrive
	n := c.s.w.UnreadInboxCount()
	if n < 2 {
		t.Fatalf("%d unread messages after a match", n)
	}
	page := c.get("/inbox")
	contains(t, page, fmt.Sprintf("%d unread.", n), "Mark all read", "Inbox ("+strconv.Itoa(n)+")", `class="unread"`)

	var first uint64
	for _, m := range c.s.w.Inbox() {
		if !m.Read {
			first = uint64(m.Event)
			break
		}
	}
	c.post("/inbox/read", url.Values{"message": {strconv.FormatUint(first, 10)}})
	if got := c.s.w.UnreadInboxCount(); got != n-1 {
		t.Fatalf("unread %d after marking one, want %d", got, n-1)
	}

	before := c.s.w.Revision()
	page = c.post("/inbox/read", url.Values{"message": {"1"}, "rev": {"1"}})
	contains(t, page, "out of date")
	c.post("/inbox/read", url.Values{"message": {"999999"}})
	if c.s.w.Revision() != before || c.s.w.UnreadInboxCount() != n-1 {
		t.Fatal("a failed acknowledgement changed the world")
	}

	c.post("/save", url.Values{"name": {"career.json"}})
	w, err := storage.Load(c.s.savePath)
	if err != nil {
		t.Fatal(err)
	}
	if w.UnreadInboxCount() != n-1 {
		t.Fatalf("reloaded save has %d unread, want %d", w.UnreadInboxCount(), n-1)
	}

	c.post("/inbox/read", url.Values{"all": {"1"}})
	if c.s.w.UnreadInboxCount() != 0 {
		t.Fatal("mark all read left unread messages")
	}
	if strings.Contains(c.get("/inbox"), "Mark all read") {
		t.Fatal("the inbox still offers to mark all read")
	}
}
