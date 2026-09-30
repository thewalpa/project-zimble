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
	"github.com/thewalpa/project-zimble/internal/inbox"
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
	contains(t, c.get("/"), "<h2>Ahead</h2>", `href="/fixtures"`, "the last day to bid", "contract ends")
	page := c.post("/continue", nil)
	contains(t, page, "Matchday: Round 1 v Brackenmoor Town (home)", "Pick lineup", "Play match")
	contains(t, page, "<h2>Ahead</h2>", "<b>Now:</b> <a href=\"/lineup\">Matchday: ")

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
	contains(t, c.get(fmt.Sprintf("/report?fixture=%d", fixture)), "Quillford FC lineup</h2>", "Brackenmoor Town lineup</h2>", "Formation ", `aria-label="Attack"`, `aria-label="Bench"`, `href="/player?id=`, `<span class="ovr">`, ", overall ")
	if n := strings.Count(c.get(fmt.Sprintf("/report?fixture=%d", fixture)), `class="pitch"`); n != 2 {
		t.Fatalf("%d pitches in the report, want both sides'", n)
	}
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

func TestTeamPlanCanBeEditedBetweenMatchesAndIsUsed(t *testing.T) {
	c := career(t)
	page := c.get("/lineup")
	contains(t, page, "Team plan", "Save team plan", "Save this lineup as your team plan")
	plan, err := c.s.w.TeamPlan()
	if err != nil {
		c.t.Fatal(err)
	}
	form := url.Values{"plan": {"1"}, "mentality": {"attacking"}}
	squad, _ := c.s.w.Squad(c.s.club())
	for _, p := range squad {
		form.Set(fmt.Sprintf("slot-%d", p.Player), "out")
	}
	for _, slot := range plan.Lineup.Starters {
		form.Set(fmt.Sprintf("slot-%d", slot.Player), slotName(slot.Role))
	}
	for _, player := range plan.Lineup.Bench {
		form.Set(fmt.Sprintf("slot-%d", player), "bench")
	}
	form.Set("slot-57", "bench")
	form.Set("slot-58", "fw")
	page = c.post("/lineup", form)
	contains(t, page, "Team plan saved.", "Saved team plan; used for matches without a submitted lineup")
	saved, err := c.s.w.TeamPlan()
	if err != nil || !saved.Saved || saved.Lineup.Tactics.Mentality.String() != "attacking" {
		c.t.Fatalf("team plan %+v, %v", saved, err)
	}
	if saved.Lineup.Starters[9].Player != 58 && saved.Lineup.Starters[10].Player != 58 {
		c.t.Fatalf("starter 58 was not saved: %+v", saved.Lineup.Starters)
	}
	contains(t, c.post("/continue", nil), "Lineup: Your saved team plan for this match")
}

// pitchZones reads the lineup pitch: the player IDs in each zone (a line's
// slot, "bench" or "out"), in the order shown.
func pitchZones(page string) map[string][]string {
	zones := map[string][]string{}
	slot := regexp.MustCompile(`^data-slot="(\w+)"`)
	id := regexp.MustCompile(`class="chip[^"]*" data-id="(\d+)"`)
	for _, part := range strings.Split(page, `<div class="zone `)[1:] {
		part = part[strings.Index(part, "data-slot"):]
		name := slot.FindStringSubmatch(part)[1]
		for _, m := range id.FindAllStringSubmatch(part, -1) {
			zones[name] = append(zones[name], m[1])
		}
	}
	return zones
}

// The lineup page draws the team on a pitch, line by line in slot order,
// with the bench and the unselected players beside it. The pitch posts its
// left-to-right order with the form, and saving keeps it.
func TestLineupPitch(t *testing.T) {
	c := career(t)
	plan, err := c.s.w.TeamPlan()
	if err != nil {
		t.Fatal(err)
	}
	l := plan.Lineup
	page := c.get("/lineup")
	contains(t, page, `Formation <span data-formation>`+app.FormationLabel(l)+`</span>`, "11 of 11 starters", "Bench", "Not selected")
	want := map[string][]string{}
	var order []string
	for _, st := range l.Starters {
		want[slotName(st.Role)] = append(want[slotName(st.Role)], fmt.Sprint(st.Player))
		order = append(order, fmt.Sprint(st.Player))
	}
	for _, p := range l.Bench {
		want["bench"] = append(want["bench"], fmt.Sprint(p))
		order = append(order, fmt.Sprint(p))
	}
	zones := pitchZones(page)
	squad, _ := c.s.w.Squad(c.s.club())
	if len(zones["out"]) != len(squad)-len(order) {
		t.Fatalf("%d unselected players on the page, want %d", len(zones["out"]), len(squad)-len(order))
	}
	delete(zones, "out")
	if !maps.EqualFunc(zones, want, slices.Equal) {
		t.Fatalf("pitch %v, want %v", zones, want)
	}
	contains(t, page, `name="order" value="`+strings.Join(order, " ")+`"`)

	// Reverse every line and the bench, as dragging would, and move a
	// defender up front.
	form := url.Values{"plan": {"1"}, "mentality": {"balanced"}}
	for _, p := range squad {
		form.Set(fmt.Sprintf("slot-%d", p.Player), "out")
	}
	for slot, ids := range want {
		for _, id := range ids {
			form.Set("slot-"+id, slot)
		}
	}
	moved := want["df"][0]
	form.Set("slot-"+moved, "fw")
	slices.Reverse(order)
	form.Set("order", strings.Join(order, " "))
	page = c.post("/lineup", form)
	contains(t, page, "Team plan saved.")
	for _, ids := range want {
		slices.Reverse(ids)
	}
	want["df"] = want["df"][:len(want["df"])-1]
	want["fw"] = append(want["fw"], moved) // defenders come after forwards in the reversed order
	zones = pitchZones(page)
	delete(zones, "out")
	if !maps.EqualFunc(zones, want, slices.Equal) {
		t.Fatalf("saved pitch %v, want %v", zones, want)
	}
	if !regexp.MustCompile(`class="chip oop[^"]*" data-id="` + moved + `"`).MatchString(page) {
		t.Fatalf("defender %s up front is not marked out of position", moved)
	}
	saved, _ := c.s.w.TeamPlan()
	contains(t, page, "Formation <span data-formation>"+app.FormationLabel(saved.Lineup)+"</span>")
	if app.FormationLabel(saved.Lineup) == app.FormationLabel(l) {
		t.Fatalf("formation still %s after moving a defender up front", app.FormationLabel(l))
	}
}

func TestLineupRejections(t *testing.T) {
	c := career(t)
	contains(t, c.get("/lineup"), "Team plan", "Starting point for matches without a submitted lineup", "Save team plan")
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
	for range 6 { // the cup run: quarter-final, semi-final and final, each a matchday and a match
		c.post("/continue", nil)
	}
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
	contains(t, c.post("/renew", renew), "Kieran Walsh signed a new contract until 1 July 2028 at 2,570.00 a week.")
	contains(t, c.post("/renew", url.Values{"player": {"45"}, "years": {"2"}, "wage": {"1"}}), "contract offer rejected: weekly wage 1.00")
	contains(t, c.post("/renew", url.Values{"player": {"45"}, "years": {"two"}, "wage": {"1"}}), "whole number of years")
	contains(t, c.post("/renew", url.Values{"player": {"44"}, "years": {"2"}, "wage": {"3,000"}}), "the contract is not in its final year")

	page = c.post("/continue", nil) // the contract year opens the transfer window
	contains(t, page, "The transfer window has opened", "Aaron Morrow left the club as a free agent")
	page = c.get("/free") // the AI clubs have not yet signed everyone
	contains(t, page, "Lars Kessler")
	sign := url.Values{"player": {"168"}, "years": {"1"}, "wage": {"940"}}
	contains(t, c.post("/sign", sign), "Lars Kessler joined until 1 July 2027 at 940.00 a week.")
	contains(t, c.get("/squad"), "Lars Kessler")
	contains(t, c.post("/sign", sign), "the player is not a free agent")
	page = c.post("/continue", nil) // no transfer news: the first matchday of season 2
	contains(t, page, "Matchday: Round 1 v Brackenmoor Town (home)")
	page = c.get("/free") // AI clubs signed the others in the transfer window
	contains(t, page, "Signings wait until the matchday has been played")
	contains(t, c.post("/sign", url.Values{"player": {"379"}, "years": {"1"}, "wage": {"940"}}), "squads cannot change while rounds await results")
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
	for range 3 {
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
	contains(t, club1Page, "Probable lineup</h2>", "the squad changes by kickoff")
	if strings.Contains(c.get("/squad"), "Probable lineup") {
		t.Fatal("the user club's squad page shows a forecast of its own lineup")
	}
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
	contains(t, reportPage, "Game report", "Founders League", "Round 1", "Match timeline", "Half time", "Full time", "Back to fixtures", "League table")
	if n := strings.Count(reportPage, "lineup</h2>"); n != 2 {
		t.Fatalf("%d lineups in a match played on the assistant's suggestion, want both sides'", n)
	}

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
	contains(t, c.get("/"), "Next match", "Continental Cup quarter-final v Ironbridge Wanderers (home)")
	contains(t, c.post("/continue", nil), "Matchday: Continental Cup quarter-final v Ironbridge Wanderers (home).")
	contains(t, c.get("/lineup"), "Continental Cup quarter-final v Ironbridge Wanderers (home)")
	page := c.post("/continue", nil) // the quarter-final
	contains(t, page, "Brackenmoor Town 1-1 Ironbridge Wanderers (2-4 on penalties)", `class="pill L"`)
	contains(t, c.get("/cup"), "Continental Cup 1", "Quarter-finals", "Semi-finals", "Final", "1-1 (2-4 on penalties)", "Your club is in it.")
	contains(t, c.get("/fixtures"), "Continental Cup quarter-final")
	contains(t, c.get("/table"), "Founders League season 1", "Harbour League season 1")
	c.post("/continue", nil) // the rest of the cup, without the club
	contains(t, c.get("/inbox"), "Continental Cup 1 won by Saltmere Athletic; you went out in the quarter-final.", "(2-4 on penalties) v Ironbridge Wanderers (home), Continental Cup quarter-final")
	contains(t, c.get("/cup"), "Won by <b>Saltmere Athletic</b>")

	// Hollowick Athletic (club 6 of seed 1) wins it.
	c = newClient(t, config{seed: 1, club: 6, savePath: filepath.Join(t.TempDir(), "career.json")})
	c.post("/season", nil)
	for range 7 { // the quarter-final, semi-final and final, each a matchday and a match; then the cup ends
		c.post("/continue", nil)
	}
	contains(t, c.get("/inbox"), "Continental Cup 1 won by Hollowick Athletic: your club won it!")
}

// In the window the manager bids from the market, the answer arrives with
// Continue, and an AI club's bid for one of the manager's players is
// answered on the Transfers page.
func TestTransfersInTheBrowser(t *testing.T) {
	c := career(t)
	c.post("/season", nil)
	for range 7 { // the cup run to the final, then the contract year
		c.post("/continue", nil)
	}
	page := c.post("/continue", nil)
	contains(t, page, "The transfer window has opened", `href="/transfers"`)
	page = c.get("/transfers?pos=FW")
	contains(t, page, "Rhys Underwood", "400,000.00", `action="/bid"`)
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
	for _, p := range []string{"344", "547"} {
		bid(p, strconv.FormatInt(int64(valueOf(t, c, p))/100, 10))
	}
	contains(t, bid("617", "400,000"), "You bid 400,000.00 for Rhys Underwood", "Your bids awaiting an answer")
	contains(t, bid("238", "500000"), "You bid 500,000.00")
	contains(t, bid("238", "700000"), "your club has already bid for the player in this window")
	contains(t, bid("617", "abc"), "the fee must be a positive whole amount")

	page = c.post("/continue", nil)
	contains(t, page, "Transfer news", "Rhys Underwood joined from Northwick Albion for 400,000.00",
		"Your bid of 500,000.00 for Leif Dekker of Ironbridge Wanderers was rejected")
	// AI clubs bid for the manager's players only when he lists them.
	contains(t, c.post("/list", url.Values{"player": {"44"}, "asking": {strconv.FormatInt(int64(valueOf(t, c, "44"))/100, 10)}, "back": {"/squad"}}),
		"Callum Doyle is on the transfer list")
	page = c.post("/continue", nil)
	contains(t, page, "Eldhaven United bid 1,200,000.00 for Callum Doyle", "1 bids for your players await your answer")
	contains(t, c.get("/"), "<b>Now:</b> <a href=\"/transfers\">Eldhaven United bid 1,200,000.00 for Callum Doyle. Answer by ")
	page = c.get("/transfers")
	contains(t, page, "Bids for your players", `action="/answer"`, "Transfers in this window")
	contains(t, c.post("/answer", url.Values{"offer": {"99"}, "accept": {"yes"}, "back": {"/transfers"}}), "no open offer for one of your players")
	contains(t, c.post("/answer", url.Values{"offer": {"84"}, "accept": {"yes"}, "back": {"/transfers"}}), "Accepted: the transfer is complete.")
	contains(t, c.get("/inbox"), "Callum Doyle left for Eldhaven United for 1,200,000.00")
	contains(t, c.get("/finances"), "transfer fee, offer 84")
	contains(t, c.get("/squad?club=1"), "asking price")
}

func TestRefusedAcceptedOfferWording(t *testing.T) {
	got := refusedOfferText(app.OfferView{PlayerName: "Elias Gallo", BuyerName: "Eldhaven United"})
	contains(t, got, "Accepted, but Elias Gallo refused to join Eldhaven United.")
}

func TestRefusedOfferInboxWording(t *testing.T) {
	c := career(t)
	msg := app.InboxItem{
		Message:    inbox.Message{Kind: inbox.KindOfferClosed, Outcome: uint8(transfers.StatusRefused), Selling: true},
		PlayerName: "Elias Gallo",
		ClubName:   "Eldhaven United",
	}
	contains(t, c.s.transferText(msg), "the player refused to join")
}

// The manager lists and unlists players through the squad page. The
// transfer page shows every listed player, offers bids for other clubs'
// players, and marks listed players in the wider market.
func TestTransferListInTheBrowser(t *testing.T) {
	c := career(t)
	page := c.get("/squad")
	contains(t, page, `action="/list"`, `aria-label="Asking price for Kieran Walsh"`, ">List</button>")

	page = c.post("/list", url.Values{"player": {"56"}, "asking": {"700,000"}, "back": {"/squad"}})
	contains(t, page, "Kieran Walsh is on the transfer list at 700,000.00", "Listed at 700,000.00", ">Change</button>", ">Unlist</button>")
	page = c.get("/transfers")
	contains(t, page, "Transfer list", "Quillford FC (You)", "Kieran Walsh", "700,000.00", ">Unlist</button>",
		"You receive bids only for players you put on the transfer list")

	page = c.post("/list", url.Values{"player": {"56"}, "unlist": {"yes"}, "back": {"/transfers"}})
	contains(t, page, "Kieran Walsh is off the transfer list", "No players are on the transfer list")
	c.post("/list", url.Values{"player": {"56"}, "asking": {"700000"}, "back": {"/squad"}})

	// A player at the positional minimum cannot be listed.
	minimum := career(t)
	minimum.post("/release", url.Values{"player": {"41"}, "back": {"/squad"}})
	contains(t, minimum.post("/list", url.Values{"player": {"42"}, "asking": {"500000"}, "back": {"/squad"}}),
		"the squad would fall below its minimum at that position: 2 GK, minimum 2")

	page = c.post("/continue", nil)
	contains(t, page, "Hollowick Town bid 700,000.00 for Kieran Walsh")
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
		`action="/release"`, `confirm('Release Rhys McAllister for 179,400.00?');`, "Release (179,400.00)")

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

	// Releasing player 60 (Rhys McAllister) drops squad to 24.
	page = c.post("/release", url.Values{"player": {"60"}})
	contains(t, page, "Rhys McAllister was released and is now a free agent. You paid 179,400.00.")

	// Rhys McAllister is now on the free agents page with an active Sign button (since 24 < 25).
	freePage := c.get("/free")
	contains(t, freePage, "Rhys McAllister", "<button >Sign</button>")

	// Finances shows the contract payoff with the player's name and amount.
	contains(t, c.get("/finances"), "contract payoff, Rhys McAllister", "-179,400.00")

	// Inbox shows the release message.
	contains(t, c.get("/inbox"), "Rhys McAllister left the club as a free agent; you paid 179,400.00")

	// Sign Rhys McAllister back to return squad to 25.
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
	contains(t, freeFull, "Callum McAllister", "squad full")
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

	// Release starter 44 (Callum Doyle) between rounds.
	page = c.post("/release", url.Values{"player": {"44"}})
	contains(t, page, "Callum Doyle was released")

	// Advance to Round 2 matchday.
	page = c.post("/continue", nil)
	contains(t, page, "Matchday: Round 2 v Hollowick Town (away)",
		"Lineup: Carried over from the last match (vs Brackenmoor Town)",
		"Callum Doyle has left the club;",
		"takes his place",
	)

	// Check /lineup page:
	lineupPage := c.get("/lineup")
	contains(t, lineupPage,
		"Carried over from the last match (vs Brackenmoor Town)",
		"Callum Doyle has left the club;",
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
	contains(t, c.get("/player?id=56"), "Kieran Walsh", "(You)", "Contract:", "until", "Value:", "Cond", "Club history", "At career start", "Current club", `/compare?a=56`)
	// A player of another club shows that club, not "(You)".
	other := c.get("/player?id=1")
	contains(t, other, "</html>")
	if strings.Contains(other, "(You)") {
		t.Fatal("another club's player was marked as ours")
	}
	contains(t, c.get("/player?id=99999"), "There is no player 99999")
	contains(t, c.get("/player?id=x"), "is not a player ID")
}

func TestComparePlayersPage(t *testing.T) {
	c := career(t)
	contains(t, c.get("/compare?a=56&b=1"), "Compare players", "Kieran Walsh", "Weekly wage", "Asking price", "Wage demand", "Attribute order", "id=\"my-squad\"")
	contains(t, c.get("/compare?a=Kieran+Walsh&b=1"), "Kieran Walsh", "Weekly wage")
	contains(t, c.get("/compare?a=56&b=99999"), "There is no player 99999")
	contains(t, c.get("/compare?a=x&b=1"), "is not a player ID")
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
	contains(t, all, "final table", "▼ the bottom 2 were relegated", `title="Relegated"`, "Quarter-finals", "Won by <b>Saltmere Athletic</b>", "← All seasons")
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

// Every row of the squad, player and lineup tables has as many cells as
// its header has columns.
func TestTableRowsMatchTheirHeaders(t *testing.T) {
	c := career(t)
	check := func(path, page string) {
		tables := strings.Split(page, "<table")[1:]
		if len(tables) == 0 {
			t.Fatalf("%s: no table", path)
		}
		for _, table := range tables {
			table = table[:strings.Index(table, "</table>")]
			rows := strings.Split(table, "<tr")[1:]
			if len(rows) == 0 || !strings.Contains(rows[0], "<th") {
				continue
			}
			header := strings.Count(rows[0], "<th")
			for i, row := range rows[1:] {
				if n := strings.Count(row, "<td"); n != header {
					t.Fatalf("%s: row %d has %d cells under %d columns", path, i+1, n, header)
				}
			}
		}
	}
	for _, path := range []string{"/squad", "/player?id=56", "/table", "/transfers", "/transfers?pos=GK", "/history"} {
		check(path, c.get(path))
	}
	contains(t, c.post("/continue", nil), "Pick lineup")
	check("/lineup", c.get("/lineup"))
}

// The squad, lineup, free-agent and profile pages show all eleven attributes
// and every player's nationality; the sort links take the new columns.
func TestAttributesAndNationalities(t *testing.T) {
	c := career(t)
	for _, path := range []string{"/squad", "/player?id=56"} {
		contains(t, c.get(path), "DRI", "HEA", "STR", "ACC", "PSN", "Westmark")
	}
	contains(t, c.get("/player?id=56"), "· Westmark ·")
	contains(t, c.get("/free"), "Nat")
	contains(t, c.get("/transfers"), "Nat", "(Eastmarch)") // the clubs' nations in the market
	squad, _ := c.s.w.Squad(c.s.club())
	firstPlayer := regexp.MustCompile(`<td><a href="/player\?id=(\d+)">`)
	for col, attr := range attributeColumns {
		page := c.get("/squad?sort=" + col + "&dir=desc")
		m := firstPlayer.FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("sort %s: no rows", col)
		}
		id, _ := strconv.Atoi(m[1])
		best, got := 0, -1
		for _, p := range squad {
			best = max(best, int(p.Attributes[attr]))
			if int(p.Player) == id {
				got = int(p.Attributes[attr])
			}
		}
		if got != best {
			t.Errorf("sort %s desc starts with %d, the best is %d", col, got, best)
		}
	}
	contains(t, c.get("/squad?sort=nat&dir=asc"), "Eastmarch")
	// The club chooser groups the 32 clubs by league, in competition order.
	page := newClient(t, config{seed: 42, savePath: filepath.Join(t.TempDir(), "c.json")}).get("/")
	if i, j := strings.Index(page, "Founders League"), strings.Index(page, "Harbour Second Division"); i < 0 || j < i {
		t.Fatal("the club chooser doesn't list the leagues in order")
	}
}

// A managed season hurts players: the inbox tells, the squad and lineup mark
// who is out, and a lineup that starts an injured player is explained.
func TestInjuriesInTheBrowser(t *testing.T) {
	c := career(t)
	var out app.SquadPlayer
	for i := 0; i < 120 && out.Player == 0; i++ {
		c.post("/continue", nil)
		if _, pending := c.s.pendingFixture(); !pending {
			continue
		}
		squad, _ := c.s.w.Squad(c.s.club())
		for _, p := range squad {
			if p.DaysOut > 0 {
				out = p
			}
		}
	}
	if out.Player == 0 {
		t.Fatal("nobody was injured on a matchday in 120 continues")
	}
	mark := fmt.Sprintf(`<span class="out" title="Injured: misses matches until he recovers">out %d d</span>`, out.DaysOut)
	contains(t, c.get("/squad"), mark)
	contains(t, c.get("/lineup"), mark, fmt.Sprintf("Injured: %d days out", out.DaysOut))
	filtered := c.get("/lineup?available=1")
	contains(t, filtered, "Available only", "checked")
	if strings.Contains(filtered, fmt.Sprintf(`href="/player?id=%d"`, out.Player)) {
		t.Fatalf("unavailable injured player %d is visible in the filtered editor", out.Player)
	}
	contains(t, c.get("/inbox"), out.Name+" is out for ")
	contains(t, c.get("/player?id="+strconv.Itoa(int(out.Player))), fmt.Sprintf("out %d d", out.DaysOut))
	fixture, _ := c.s.pendingFixture()
	form := lineupForm(c, fixture, "balanced")
	form.Set(fmt.Sprintf("slot-%d", out.Player), "bench")
	contains(t, c.post("/lineup", form), fmt.Sprintf("player %d is injured", out.Player))
}

// Tables mark the places that change division: the current table says who
// would move, a finished season's who did.
func TestPromotionAndRelegationMarks(t *testing.T) {
	c := career(t)
	page := c.get("/table")
	contains(t, page, "▼ the bottom 2 are relegated", "▲ the top 2 are promoted", "Harbour Second Division")
	if up, down := strings.Count(page, `class="up"`), strings.Count(page, `class="out"`); up != 4 || down != 4 {
		t.Fatalf("%d promotion and %d relegation marks in four leagues, want 4 and 4", up, down)
	}
	if strings.Count(page, "qualify for Continental Cup") != 2 {
		t.Fatal("only the first divisions send their top four to the cup")
	}
	c.post("/season", nil)
	for range 7 {
		c.post("/continue", nil)
	}
	contains(t, c.get("/table"), "▼ the bottom 2 were relegated", "▲ the top 2 were promoted", "final table", `title="Relegated"`, `title="Promoted"`)
}

// The market says which clubs will not sell at their price, and the window's
// last days say that AI clubs sell only what they can spare.
func TestMarketRefusalsInTheBrowser(t *testing.T) {
	c := career(t)
	c.post("/season", nil)
	for range 8 {
		c.post("/continue", nil)
	}
	if !c.s.w.TransferWindow().Open {
		t.Fatal("the transfer window is not open")
	}
	var refused string
	for _, pos := range []string{"GK", "DF", "MF", "FW"} {
		refused += c.get("/transfers?pos=" + pos)
	}
	contains(t, refused, `<span class="refused">won&#39;t join a weaker club</span>`)
	if strings.Contains(refused, "AI clubs now sell only") {
		t.Fatal("the closing notice is shown at the start of the window")
	}
	if _, err := c.s.w.Continue(c.s.w.TransferWindow().NeededClose); err != nil {
		t.Fatal(err)
	}
	if c.s.w.Now() < c.s.w.TransferWindow().NeededClose {
		t.Fatal("did not reach the last days of the window")
	}
	contains(t, c.get("/transfers"), "AI clubs now sell only listed players and players they can spare")
	contains(t, c.get("/free"), "Until Wed 2026-07-15 00:00 UTC, only you may sign free agents; AI clubs can sign them from then.")
}

// The season-end message says when the club goes up or down a division.
func TestSeasonEndSaysRelegation(t *testing.T) {
	c := newClient(t, config{seed: 42, club: 8, savePath: filepath.Join(t.TempDir(), "career.json")})
	c.post("/season", nil)
	c.post("/continue", nil)
	contains(t, c.get("/inbox"), "you finished 8: relegated to the division below.")
}
