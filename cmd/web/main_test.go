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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/storage"
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
	contains(t, page, "Matchday: round 1 v Brackenmoor Town (home)", "Pick lineup", "Play match")

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

	page = c.post("/continue", nil) // the contract year, then the first matchday of season 2
	contains(t, page, "Matchday: round 1 v Greyfen United (away)", "Oscar Bellamy left the club as a free agent")
	page = c.get("/free")
	contains(t, page, "Elias Adeyemi", "Signings wait until the matchday has been played", "disabled")
	sign := url.Values{"player": {"72"}, "years": {"1"}, "wage": {"1160"}}
	contains(t, c.post("/sign", sign), "squads cannot change while rounds await results")
	c.post("/continue", nil)
	contains(t, c.post("/sign", sign), "Elias Adeyemi joined until 1 July 2027 at 1,160.00 a week.")
	contains(t, c.get("/squad"), "Elias Adeyemi")
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

// Every page renders at every stage of a career: a matchday, after it, the
// off-season and after the player year (with retirements and youth).
func TestEveryPageRenders(t *testing.T) {
	c := career(t)
	pages := []string{"/", "/squad", "/lineup", "/table", "/fixtures", "/free", "/inbox", "/finances"}
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
