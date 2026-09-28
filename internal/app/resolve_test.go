package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/matches"
)

// goldenSeasonSeed42 pins every official result of the default seed-42
// season. It covers world generation, scheduling, AI selection, condition,
// the AI transfer market in the first window and the match model: an
// intended change must bump the responsible version (worldgen/content/
// random, competitions.ScheduleVersion, ai.SelectionVersion,
// ai.TransfersVersion, medical.Version, simple.ModelVersion) and update
// this value. Last changed by sellers keeping their stars
// (ai.TransfersVersion 4: selling prices, settling players and stars'
// choice of club change the first window's trades).
//
// It covers the first league's season 1, which the other leagues and the
// cup leave unchanged; the second league's season 1 and the first cup
// edition have their own goldens.
const (
	goldenSeasonSeed42       = "1f8629fbd4f7a3e5f6500d5e8349113e8bc47d0c4f5767b0b40e3dba724e87ba"
	goldenSecondLeagueSeed42 = "5eb745975cbf186becac730ccad449b1da3fd97c6ff539e8e846f1a7d3cf9663"
	goldenCupSeed42          = "57b765e77cdb236ab01cc83319d887ef57afce1cb17de1f756d7b2c6c1e4d791"
)

// seasonEnd is one day after the last kickoff of every league.
func seasonEnd(w *World) sim.GameInstant {
	var last sim.GameInstant
	for _, s := range w.Schedules() {
		last = max(last, s.Rounds[len(s.Rounds)-1].Kickoff)
	}
	return last + day
}

func readyBatch(t *testing.T, w *World) FixtureRoundReady {
	t.Helper()
	res := mustContinue(t, w, seasonEnd(w))
	ready, ok := res.(FixtureRoundReady)
	if !ok {
		t.Fatalf("Continue = %#v, want FixtureRoundReady", res)
	}
	return ready
}

func commandFor(ready FixtureRoundReady, id CommandID) ResolveRounds {
	cmd := ResolveRounds{ID: id, ExpectedRevision: ready.Revision}
	for _, r := range ready.Rounds {
		cmd.Rounds = append(cmd.Rounds, r.Round)
	}
	return cmd
}

// cupEnd is one day after the final of every cup's latest edition, or now.
func cupEnd(w *World) sim.GameInstant {
	end := w.Now()
	for _, c := range w.Cups() {
		end = max(end, c.Rounds[len(c.Rounds)-1].Kickoff+day)
	}
	return end
}

// playSeason plays the leagues' current seasons and then the cup editions
// they qualify teams for, to one day after each cup final.
func playSeason(t *testing.T, w *World) []RoundsResolved {
	t.Helper()
	out := playLeagues(t, w)
	return append(out, playCup(t, w)...)
}

// playCup plays every cup's latest edition, to one day after its final.
func playCup(t *testing.T, w *World) []RoundsResolved {
	t.Helper()
	return playUntil(t, w, cupEnd)
}

// playLeagues plays the leagues' current seasons, to one day after their
// last kickoff (which creates the next seasons and any cup edition).
func playLeagues(t *testing.T, w *World) []RoundsResolved {
	t.Helper()
	return playUntil(t, w, seasonEnd)
}

// playUntil alternates Continue and ResolveRounds until Continue reaches
// target(w), recomputed before each Continue.
func playUntil(t *testing.T, w *World, target func(*World) sim.GameInstant) []RoundsResolved {
	t.Helper()
	var out []RoundsResolved
	for id := CommandID(len(w.commands) + 1); ; id++ { // next unused command ID
		res := mustContinue(t, w, target(w))
		ready, ok := res.(FixtureRoundReady)
		if !ok {
			return out
		}
		resolved, err := w.ResolveRounds(commandFor(ready, id))
		if err != nil {
			t.Fatalf("resolve %d: %v", id, err)
		}
		out = append(out, resolved)
	}
}

// firstSeasons returns each league's season-1 schedule, whatever season is
// current.
func firstSeasons(w *World) []Schedule {
	var out []Schedule
	for _, l := range w.leagues {
		out = append(out, w.schedule(leagueEntry{def: l.def, season: competitions.SeasonRef{Competition: l.def.ID, Season: 1}}))
	}
	return out
}

// resultsFingerprint hashes every league's season-1 fixtures and results.
// resultsFingerprint hashes the first league's season-1 fixtures and
// results.
func resultsFingerprint(w *World) string {
	return seasonFingerprint(w, competitions.SeasonRef{Competition: 1, Season: 1})
}

// seasonFingerprint hashes any season's fixtures and results (penalties
// only where there was a shootout, so league hashes keep their format).
func seasonFingerprint(w *World, ref competitions.SeasonRef) string {
	h := sha256.New()
	for _, f := range w.competitions.Fixtures(ref) {
		line := w.fixtureLine(f)
		fmt.Fprintf(h, "%d %d-%d %v %v\n", f.ID, f.Home, f.Away, line.Played, line.Score)
		if line.Shootout != [2]uint16{} {
			fmt.Fprintf(h, "penalties %v\n", line.Shootout)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestFullSeasonIntegrity(t *testing.T) {
	w := newWorld(t, 42)
	end := seasonEnd(w)
	resolved := playLeagues(t, w)
	if len(resolved) != 14 {
		t.Fatalf("%d batches resolved, want 14", len(resolved))
	}
	for i, r := range resolved {
		// All four leagues' rounds kick off together: one batch.
		if len(r.Matches) != 16 || len(r.Rounds) != 4 || r.Rounds[0].Round != competitions.Round(i+1) {
			t.Fatalf("batch %d: %d matches, rounds %v", i, len(r.Matches), r.Rounds)
		}
		if i > 0 && r.Revision <= resolved[i-1].Revision {
			t.Fatal("revisions do not increase")
		}
		k := firstSeasons(w)[0].Rounds[i].Kickoff
		if r.At != k {
			t.Fatalf("batch %d official at %d, kickoff %d", i, r.At, k)
		}
	}
	if res := mustContinue(t, w, end); res != (ReachedTarget{Now: end}) {
		t.Fatalf("after the season Continue = %#v", res)
	}
	season := competitions.SeasonRef{Competition: 1, Season: 1}
	if len(w.competitions.PendingRounds()) != 0 || w.leagues[0].season.Season != 2 {
		t.Fatal("rounds pending, or the next season was not created")
	}
	for _, task := range kickoffTasks(w) {
		if w.payloads[task.PayloadID].Season == season {
			t.Fatalf("season 1 kickoff task %d left after the season", task.ID)
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	results := w.competitions.Results(season)
	fixtures := w.competitions.Fixtures(season)
	if len(results) != 56 || len(fixtures) != 56 {
		t.Fatalf("%d results for %d fixtures", len(results), len(fixtures))
	}
	seen := map[ids.FixtureID]int{}
	for _, r := range results {
		seen[r.Fixture]++
	}
	for _, f := range fixtures {
		if seen[f.ID] != 1 {
			t.Fatalf("fixture %d has %d results", f.ID, seen[f.ID])
		}
	}
	for _, r := range firstSeasons(w)[0].Rounds {
		if r.Status != competitions.RoundCompleted {
			t.Fatalf("round %d is %s", r.Round, r.Status)
		}
	}

	table, _ := w.Table(season)
	if !table.Complete || table.RoundsCompleted != 14 || len(table.Rows) != 8 {
		t.Fatalf("table header %+v", table)
	}
	var gf, ga, wins, losses, draws int
	for _, row := range table.Rows {
		if row.Played != 14 || row.Won+row.Drawn+row.Lost != 14 || row.Points != 3*row.Won+row.Drawn {
			t.Fatalf("row %+v inconsistent", row.Standing)
		}
		gf += row.GoalsFor
		ga += row.GoalsAgainst
		wins += row.Won
		losses += row.Lost
		draws += row.Drawn
	}
	if gf != ga || wins != losses || draws%2 != 0 || wins+draws/2 != 56 {
		t.Fatalf("league totals: GF %d GA %d, W %d L %d D %d", gf, ga, wins, losses, draws)
	}
	assertStandingsMatchResults(t, results, table.Rows)
}

// assertStandingsMatchResults recomputes the table independently from the
// official results and the documented ranking rules.
func assertStandingsMatchResults(t *testing.T, results []competitions.Result, rows []TableRow) {
	t.Helper()
	type rec struct{ team, p, w, d, l, gf, ga, pts int }
	recs := map[ids.TeamID]*rec{}
	get := func(id ids.TeamID) *rec {
		if recs[id] == nil {
			recs[id] = &rec{team: int(id)}
		}
		return recs[id]
	}
	for _, r := range results {
		for _, side := range []struct {
			team      ids.TeamID
			for_, ag_ int
		}{{r.Home, int(r.HomeGoals), int(r.AwayGoals)}, {r.Away, int(r.AwayGoals), int(r.HomeGoals)}} {
			x := get(side.team)
			x.p++
			x.gf += side.for_
			x.ga += side.ag_
			switch {
			case side.for_ > side.ag_:
				x.w++
				x.pts += 3
			case side.for_ == side.ag_:
				x.d++
				x.pts++
			default:
				x.l++
			}
		}
	}
	var want []*rec
	for _, x := range recs {
		want = append(want, x)
	}
	sort.Slice(want, func(i, j int) bool {
		a, b := want[i], want[j]
		if a.pts != b.pts {
			return a.pts > b.pts
		}
		if a.gf-a.ga != b.gf-b.ga {
			return a.gf-a.ga > b.gf-b.ga
		}
		if a.gf != b.gf {
			return a.gf > b.gf
		}
		return a.team < b.team
	})
	if len(want) != len(rows) {
		t.Fatalf("%d independent rows, %d table rows", len(want), len(rows))
	}
	for i, x := range want {
		r := rows[i]
		got := rec{int(r.Team), r.Played, r.Won, r.Drawn, r.Lost, r.GoalsFor, r.GoalsAgainst, r.Points}
		if got != *x || r.Rank != i+1 {
			t.Fatalf("rank %d: table %+v, independent %+v", i+1, got, *x)
		}
	}
}

func TestFullSeasonIsReproducible(t *testing.T) {
	a, b := newWorld(t, 42), newWorld(t, 42)
	fixturesBefore := a.competitions.Fixtures(a.leagues[0].season)
	ra, rb := playSeason(t, a), playSeason(t, b)
	if !reflect.DeepEqual(ra, rb) || !reflect.DeepEqual(a.Tables(), b.Tables()) {
		t.Fatal("same seed produced different seasons")
	}
	for _, g := range []struct {
		ref    competitions.SeasonRef
		golden string
	}{
		{competitions.SeasonRef{Competition: 1, Season: 1}, goldenSeasonSeed42},
		{competitions.SeasonRef{Competition: 2, Season: 1}, goldenSecondLeagueSeed42},
		{competitions.SeasonRef{Competition: 3, Season: 1}, goldenCupSeed42},
	} {
		if got := seasonFingerprint(a, g.ref); got != g.golden {
			t.Errorf("%s fingerprint = %s, want %s", g.ref, got, g.golden)
		}
	}
	// Playing the season changed neither the generated world nor the
	// schedule.
	if a.Summary().Fingerprint != "97707b41d757f481c66c2e4f5161d9886ce31f7f3051b1add1d69b34779568bc" {
		t.Fatal("world fingerprint changed")
	}
	if !reflect.DeepEqual(a.competitions.Fixtures(competitions.SeasonRef{Competition: 1, Season: 1}), fixturesBefore) {
		t.Fatal("fixtures changed during the season")
	}
	if resultsFingerprint(newWorld(t, 43)) == resultsFingerprint(a) {
		t.Fatal("unplayed world fingerprints like a played one")
	}
}

func TestSimultaneousLeaguesResolveAsOneBatch(t *testing.T) {
	w := twoLeagueWorld(t)
	ready := readyBatch(t, w)
	res, err := w.ResolveRounds(commandFor(ready, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rounds) != 2 || len(res.Matches) != 8 {
		t.Fatalf("resolved %v with %d matches", res.Rounds, len(res.Matches))
	}
	for i := 1; i < len(res.Matches); i++ {
		if res.Matches[i].Fixture <= res.Matches[i-1].Fixture {
			t.Fatal("matches not in fixture ID order")
		}
	}
	for _, r := range ready.Rounds {
		if info, _ := w.competitions.Round(r.Round); info.Status != competitions.RoundCompleted {
			t.Fatalf("%s is %s", r.Round, info.Status)
		}
	}
	playSeason(t, w)
	for _, comp := range []ids.CompetitionID{1, 2} {
		table, _ := w.Table(competitions.SeasonRef{Competition: comp, Season: 1})
		if !table.Complete {
			t.Fatalf("competition %d incomplete", table.Competition)
		}
		assertStandingsMatchResults(t, w.competitions.Results(competitions.SeasonRef{Competition: table.Competition, Season: 1}), table.Rows)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOverlappingTeamsAreRejected(t *testing.T) {
	w := newWorld(t, 42)
	addLeague(t, w, 9) // league 1's eight teams, same kickoffs
	ready := readyBatch(t, w)
	before := snapshot(w)
	if _, err := w.ResolveRounds(commandFor(ready, 1)); !errors.Is(err, ErrOverlappingTeams) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(snapshot(w), before) {
		t.Fatal("rejected batch changed the world")
	}
}

func TestResolveDoesNotMoveTheClock(t *testing.T) {
	w := newWorld(t, 42)
	ready := readyBatch(t, w)
	tasks := w.scheduler.Pending()
	if _, err := w.ResolveRounds(commandFor(ready, 1)); err != nil {
		t.Fatal(err)
	}
	if w.Now() != ready.At || !reflect.DeepEqual(w.scheduler.Pending(), tasks) {
		t.Fatal("ResolveRounds moved the clock or ran tasks")
	}
	next := readyBatch(t, w)
	if next.Rounds[0].Round.Round != 2 || next.At != ready.At+7*day {
		t.Fatalf("next batch %+v", next)
	}
}

func TestDuplicateAndStaleCommands(t *testing.T) {
	w := newWorld(t, 42)
	ready := readyBatch(t, w)
	cmd := commandFor(ready, 7)
	first, err := w.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(w)
	tables := w.Tables()

	// Mutating the returned value does not reach the recorded result.
	first.Matches[0].Score = [2]uint16{99, 99}
	first.Matches[0].Goals = nil

	again, err := w.ResolveRounds(cmd)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if again.Matches[0].Score == [2]uint16{99, 99} || again.Revision != after.Revision {
		t.Fatalf("retry returned %+v", again.Matches[0])
	}
	if !reflect.DeepEqual(snapshot(w), after) || !reflect.DeepEqual(w.Tables(), tables) {
		t.Fatal("duplicate command changed the world or double-counted points")
	}

	reused := cmd
	reused.ExpectedRevision++
	if _, err := w.ResolveRounds(reused); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused ID with different revision: %v", err)
	}
	reused = cmd
	reused.Rounds = []competitions.RoundRef{{Season: w.leagues[0].season, Round: 2}}
	if _, err := w.ResolveRounds(reused); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused ID with different rounds: %v", err)
	}

	stale := commandFor(ready, 8)
	if _, err := w.ResolveRounds(stale); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("new ID, old revision: %v", err)
	}
	stale.ExpectedRevision = w.Revision()
	if _, err := w.ResolveRounds(stale); !errors.Is(err, ErrNoPendingRounds) {
		t.Fatalf("new ID, batch already resolved: %v", err)
	}
	if !reflect.DeepEqual(snapshot(w), after) || !reflect.DeepEqual(w.Tables(), tables) {
		t.Fatal("rejected commands changed the world")
	}
}

func TestInvalidResolveCommands(t *testing.T) {
	w := newWorld(t, 42)
	season := w.leagues[0].season
	if _, err := w.ResolveRounds(ResolveRounds{ID: 1, Rounds: []competitions.RoundRef{{Season: season, Round: 1}}}); !errors.Is(err, ErrNoPendingRounds) {
		t.Fatalf("before kickoff: %v", err)
	}
	ready := readyBatch(t, w)
	before := snapshot(w)
	good := commandFor(ready, 1)
	cases := map[string]struct {
		cmd  ResolveRounds
		want error
	}{
		"zero ID":         {ResolveRounds{ID: 0, ExpectedRevision: ready.Revision, Rounds: good.Rounds}, ErrInvalidCommand},
		"duplicate round": {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision, Rounds: append(slices.Clone(good.Rounds), good.Rounds...)}, ErrInvalidCommand},
		"zero round":      {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision, Rounds: []competitions.RoundRef{{Season: season}}}, ErrInvalidCommand},
		"empty batch":     {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision}, ErrBatchMismatch},
		"wrong round":     {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision, Rounds: []competitions.RoundRef{{Season: season, Round: 2}}}, ErrBatchMismatch},
		"extra round":     {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision, Rounds: append(slices.Clone(good.Rounds), competitions.RoundRef{Season: season, Round: 2})}, ErrBatchMismatch},
		"stale revision":  {ResolveRounds{ID: 2, ExpectedRevision: ready.Revision - 1, Rounds: good.Rounds}, ErrStaleRevision},
	}
	for name, c := range cases {
		if _, err := w.ResolveRounds(c.cmd); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if !reflect.DeepEqual(snapshot(w), before) {
		t.Fatal("rejected commands changed the world")
	}
}

// faultyEngine wraps the real engine and injects failures at chosen fixtures.
type faultyEngine struct {
	matches.Engine
	failStart   ids.FixtureID
	neverFinish ids.FixtureID
	tamperAt    ids.FixtureID
	tamper      func(*matches.MatchStepResult)
	started     int
}

func (f *faultyEngine) Start(in *matches.MatchInput, rs matches.RandomState) (matches.MatchSession, error) {
	f.started++
	if in.Match == f.failStart {
		return nil, errors.New("injected start failure")
	}
	s, err := f.Engine.Start(in, rs)
	if err != nil {
		return nil, err
	}
	return &faultySession{MatchSession: s, e: f, match: in.Match}, nil
}

type faultySession struct {
	matches.MatchSession
	e     *faultyEngine
	match ids.FixtureID
}

func (s *faultySession) Advance(req matches.AdvanceRequest, dst *matches.MatchStepResult) error {
	if s.match == s.e.neverFinish {
		dst.Status = matches.MatchRunning
		return nil
	}
	if err := s.MatchSession.Advance(req, dst); err != nil {
		return err
	}
	if dst.Status == matches.MatchFinished && s.match == s.e.tamperAt {
		s.e.tamper(dst)
	}
	return nil
}

// Every failure stage leaves the batch pending and the world unchanged, and
// a retry with the same command ID then produces exactly the results of a
// world that never failed.
func TestFailuresCannotPartiallyResolveABatch(t *testing.T) {
	clean := newWorld(t, 42)
	want, err := clean.ResolveRounds(commandFor(readyBatch(t, clean), 1))
	if err != nil {
		t.Fatal(err)
	}

	first, last := want.Matches[0].Fixture, want.Matches[len(want.Matches)-1].Fixture
	type fault struct {
		engine   *faultyEngine
		prepare  func(w *World) (undo func())
		minStart int // simulations that must have run before the failure
	}
	faults := map[string]fault{
		"preparation: team without goalkeepers": {prepare: func(w *World) func() {
			orig := w.employment
			var kept []employment.Assignment
			for _, a := range orig.Assignments() {
				if p, _ := w.players.Profile(a.Player); a.Team != want.Matches[3].Away.Team || p.Position != 1 {
					kept = append(kept, a)
				}
			}
			w.employment, _ = employment.New(kept)
			return func() { w.employment = orig }
		}},
		"simulation: start fails on third match": {engine: &faultyEngine{failStart: want.Matches[2].Fixture}, minStart: 3},
		"simulation: match never finishes":       {engine: &faultyEngine{neverFinish: last}, minStart: 8},
		"validation: score disagrees with goals": {engine: &faultyEngine{tamperAt: last, tamper: func(d *matches.MatchStepResult) { d.Outcome.Score[0]++ }}, minStart: 8},
		"validation: outcome for another match":  {engine: &faultyEngine{tamperAt: first, tamper: func(d *matches.MatchStepResult) { d.Outcome.Match = 999 }}, minStart: 8},
		"validation: wrong engine version":       {engine: &faultyEngine{tamperAt: last, tamper: func(d *matches.MatchStepResult) { d.Outcome.EngineVersion++ }}, minStart: 8},
		"validation: extra participant minutes":  {engine: &faultyEngine{tamperAt: last, tamper: func(d *matches.MatchStepResult) { d.Outcome.Participants[0].OffMinute = 80 }}, minStart: 8},
		"validation: result not completed":       {engine: &faultyEngine{tamperAt: last, tamper: func(d *matches.MatchStepResult) { d.Outcome.Status = matches.ResultPending }}, minStart: 8},
	}
	for name, f := range faults {
		w := newWorld(t, 42)
		ready := readyBatch(t, w)
		cmd := commandFor(ready, 1)
		before := snapshot(w)
		realEngine := w.engine
		undo := func() {}
		if f.prepare != nil {
			undo = f.prepare(w)
		}
		if f.engine != nil {
			f.engine.Engine = realEngine
			w.engine = f.engine
		}
		if _, err := w.ResolveRounds(cmd); err == nil {
			t.Fatalf("%s: ResolveRounds succeeded", name)
		}
		if f.engine != nil && f.engine.started < f.minStart {
			t.Fatalf("%s: failed after %d simulations, want at least %d", name, f.engine.started, f.minStart)
		}
		undo()
		w.engine = realEngine
		if !reflect.DeepEqual(snapshot(w), before) {
			t.Fatalf("%s: failure changed the world", name)
		}
		if p := w.competitions.PendingRounds(); len(p) != 4 || len(w.competitions.Results(w.leagues[0].season)) != 0 || len(w.competitions.Results(w.leagues[1].season)) != 0 {
			t.Fatalf("%s: batch no longer pending or partial results recorded", name)
		}
		got, err := w.ResolveRounds(cmd)
		if err != nil {
			t.Fatalf("%s: retry: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: retry produced different results", name)
		}
	}
}

func TestMatchReportAndQueries(t *testing.T) {
	w := newWorld(t, 42)
	if _, ok := w.MatchReport(1); ok {
		t.Fatal("MatchReport before resolution")
	}
	ready := readyBatch(t, w)
	res, err := w.ResolveRounds(commandFor(ready, 1))
	if err != nil {
		t.Fatal(err)
	}
	rep, ok := w.MatchReport(1)
	if !ok {
		t.Fatal("MatchReport not found after resolution")
	}
	if rep.Fixture != 1 || rep.Home.Club == 0 || rep.Away.Club == 0 {
		t.Fatalf("unexpected MatchReport: %+v", rep)
	}
	if rep.Score != res.Matches[0].Score {
		t.Fatalf("score mismatch: got %v, want %v", rep.Score, res.Matches[0].Score)
	}
	name, ok := w.PlayerName(1)
	if !ok || name == "" {
		t.Fatalf("PlayerName(1) = %q, %v", name, ok)
	}
	if _, ok := w.PlayerName(99999); ok {
		t.Fatal("PlayerName(99999) found")
	}
	label, ok := w.ClubLabel(1)
	if !ok || label.ClubName == "" {
		t.Fatalf("ClubLabel(1) = %+v, %v", label, ok)
	}
	if _, ok := w.ClubLabel(99999); ok {
		t.Fatal("ClubLabel(99999) found")
	}
}
