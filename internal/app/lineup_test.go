package app

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
)

const userClub ids.ClubID = 3

func userWorld(t *testing.T, seed uint64, club ids.ClubID) *World {
	t.Helper()
	cfg := DefaultConfig(random.Seed(seed))
	cfg.UserClub = club
	w, err := NewWorld(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustUserTeam(t *testing.T, w *World) ids.TeamID {
	t.Helper()
	team, ok := w.userTeam()
	if !ok {
		t.Fatal("world has no user team")
	}
	return team
}

// changedLineup is the AI suggestion made attacking, with the last starting
// forward swapped for the first outfield substitute.
func changedLineup(t *testing.T, w *World, fixture ids.FixtureID) selection.Lineup {
	t.Helper()
	l, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	l.Tactics.Mentality = matches.Attacking
	for i, id := range l.Bench {
		if p, _ := w.players.Profile(id); p.Position != players.Goalkeeper {
			l.Starters[10].Player, l.Bench[i] = id, l.Starters[10].Player
			return l
		}
	}
	t.Fatal("no outfield substitute")
	return l
}

func submit(t *testing.T, w *World, fixture ids.FixtureID, l selection.Lineup) LineupSubmitted {
	t.Helper()
	res, err := w.SubmitLineup(SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: l})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// resolveNow resolves the pending batch at the current revision.
func resolveNow(t *testing.T, w *World) RoundsResolved {
	t.Helper()
	ready, ok := w.Pending()
	if !ok {
		t.Fatal("nothing pending")
	}
	res, err := w.ResolveRounds(commandFor(ready, w.NextCommandID()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// playWithLineups plays every remaining batch, submitting pick's lineup for
// each user fixture first (none if pick is nil).
func playWithLineups(t *testing.T, w *World, pick func(ids.FixtureID) selection.Lineup) []RoundsResolved {
	t.Helper()
	var out []RoundsResolved
	for {
		res := mustContinue(t, w, seasonEnd(w))
		ready, ok := res.(FixtureRoundReady)
		if !ok {
			return out
		}
		if pick != nil {
			for _, f := range ready.UserFixtures {
				submit(t, w, f, pick(f))
			}
		}
		out = append(out, resolveNow(t, w))
	}
}

func TestChoosingAUserClub(t *testing.T) {
	cfg := DefaultConfig(42)
	cfg.UserClub = 99
	if w, err := NewWorld(cfg); !errors.Is(err, ErrUnknownClub) || w != nil {
		t.Fatalf("unknown club: err = %v", err)
	}

	plain, managed := newWorld(t, 42), userWorld(t, 42, userClub)
	if club, ok := managed.UserClub(); !ok || club != userClub {
		t.Fatalf("UserClub = %d, %v", club, ok)
	}
	if _, ok := plain.UserClub(); ok {
		t.Fatal("a world without a user club reports one")
	}
	a, b := plain.Snapshot(), managed.Snapshot()
	b.UserClub = 0
	if !reflect.DeepEqual(a, b) {
		t.Fatal("choosing a club changed the generated world or its schedule")
	}

	// Without submitted lineups the AI selects for the user club too, and
	// every side is reported as AI-selected. (The season is not the golden
	// one: AI clubs trade with each other in the first window, and bid for
	// a manager's players only when the manager lists them.)
	if playWithLineups(t, plain, nil); resultsFingerprint(plain) != goldenSeasonSeed42 {
		t.Fatal("the AI season differs from the golden one")
	}
	got := playWithLineups(t, managed, nil)
	for _, r := range got {
		for _, m := range r.Matches {
			if m.Selected != [2]SelectedBy{SelectedByAI, SelectedByAI} {
				t.Fatalf("fixture %d selected by %v", m.Fixture, m.Selected)
			}
		}
	}
}

func TestContinueReportsUserFixtures(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	for range 14 {
		ready := readyBatch(t, w)
		if len(ready.UserFixtures) != 1 {
			t.Fatalf("user fixtures %v, want one", ready.UserFixtures)
		}
		f, _ := w.competitions.Fixture(ready.UserFixtures[0])
		if f.Home != team && f.Away != team || f.Round != ready.Rounds[0].Round.Round {
			t.Fatalf("fixture %+v is not the user's in this batch", f)
		}
		if pending, _ := w.Pending(); !reflect.DeepEqual(pending, ready) {
			t.Fatal("Pending differs from Continue")
		}
		resolveNow(t, w)
	}
	if ready := readyBatch(t, newWorld(t, 42)); ready.UserFixtures != nil {
		t.Fatalf("world without a user club reports user fixtures %v", ready.UserFixtures)
	}
}

func TestSubmittedLineupIsPlayed(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	ready := readyBatch(t, w)
	fixture := ready.UserFixtures[0]
	l := changedLineup(t, w, fixture)

	res := submit(t, w, fixture, l)
	if res.Revision != ready.Revision+1 || w.Revision() != res.Revision || res.Fixture != fixture || res.Team != team {
		t.Fatalf("result %+v, ready revision %d", res, ready.Revision)
	}
	if got, ok := w.SubmittedLineup(fixture); !ok || !got.Equal(l) {
		t.Fatal("SubmittedLineup does not return the submission")
	}
	// The revision the batch was reported at is now stale.
	if _, err := w.ResolveRounds(commandFor(ready, w.NextCommandID())); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("resolve at the old revision: err = %v", err)
	}

	plan, err := w.prepareBatch(commandFor(ready, 0).Rounds)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan {
		for _, side := range []matches.Side{matches.Home, matches.Away} {
			in := *p.input.Team(side)
			by := p.selected[side.Index()]
			if in.Team != team {
				ai, _ := w.selectTeam(in.Team, p.input.Rules)
				if want, _ := w.lineupInput(in.Team, ai, p.input.Rules); by != SelectedByAI || !reflect.DeepEqual(in, want) {
					t.Fatalf("fixture %d: opponent team %d not the AI selection", p.fixture.ID, in.Team)
				}
				continue
			}
			if p.fixture.ID != fixture || by != SelectedByManager || in.Tactics != l.Tactics ||
				len(in.Starters) != len(l.Starters) || len(in.Bench) != len(l.Bench) {
				t.Fatalf("fixture %d: user side %+v, selected by %s", p.fixture.ID, in, by)
			}
			for i, s := range l.Starters {
				want, _ := w.matchPlayer(s.Player)
				if want.Role = s.Role; in.Starters[i] != want {
					t.Fatalf("starter %d is %+v, submitted %+v", i, in.Starters[i], s)
				}
			}
			for i, id := range l.Bench {
				if want, _ := w.matchPlayer(id); in.Bench[i] != want {
					t.Fatalf("substitute %d is %+v, submitted %d", i, in.Bench[i], id)
				}
			}
		}
	}

	resolved := resolveNow(t, w)
	for _, m := range resolved.Matches {
		want := [2]SelectedBy{SelectedByAI, SelectedByAI}
		if m.Fixture == fixture {
			want[0], want[1] = SelectedByManager, SelectedByAI
			if m.Away.Team == team {
				want[0], want[1] = SelectedByAI, SelectedByManager
			}
		}
		if m.Selected != want {
			t.Fatalf("fixture %d selected by %v, want %v", m.Fixture, m.Selected, want)
		}
	}
}

// Submitting the AI's own suggestion must play exactly the AI default: the
// only difference is who is recorded as having selected it.
func TestSuggestedLineupReproducesTheAIDefault(t *testing.T) {
	w := userWorld(t, 42, userClub)
	got := playWithLineups(t, w, func(f ids.FixtureID) selection.Lineup {
		l, err := w.SuggestLineup(f)
		if err != nil {
			t.Fatal(err)
		}
		return l
	})
	passive := userWorld(t, 42, userClub) // the same squads, no lineups submitted
	want := playWithLineups(t, passive, nil)
	if resultsFingerprint(w) != resultsFingerprint(passive) {
		t.Fatal("submitting the suggestion changed results")
	}
	for i := range got {
		for j := range got[i].Matches {
			g, x := got[i].Matches[j], want[i].Matches[j]
			g.Selected = x.Selected
			if !reflect.DeepEqual(g, x) {
				t.Fatalf("fixture %d detail differs", g.Fixture)
			}
		}
	}
}

// The user's lineup changes only the user club's matches: every other
// fixture has its own random stream and AI selections, so its detailed
// outcome is identical.
func TestUserLineupChangesOnlyUserMatches(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	got := playWithLineups(t, w, func(f ids.FixtureID) selection.Lineup { return changedLineup(t, w, f) })
	want := playWithLineups(t, userWorld(t, 42, userClub), nil) // the same squads, no lineups submitted
	if len(got) != 14 || len(want) != 14 {
		t.Fatalf("%d and %d batches", len(got), len(want))
	}
	changed := 0
	for i := range got {
		if got[i].Revision != want[i].Revision+Revision(i+1) {
			t.Fatalf("batch %d revision %d; lineup commands must add one each", i, got[i].Revision)
		}
		for j, m := range got[i].Matches {
			x := want[i].Matches[j]
			if m.Home.Team != team && m.Away.Team != team {
				if !reflect.DeepEqual(m, x) {
					t.Fatalf("non-user fixture %d changed", m.Fixture)
				}
				continue
			}
			if m.Score != x.Score || !reflect.DeepEqual(m.Goals, x.Goals) {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatal("the submitted lineups changed no user match")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitLineupRejections(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	playBatches(t, w, 2)
	ready := readyBatch(t, w)
	fixture := ready.UserFixtures[0]
	good := changedLineup(t, w, fixture)

	var otherFixture, laterFixture, earlierFixture ids.FixtureID
	for _, id := range ready.Rounds[0].Fixtures {
		if id != fixture {
			otherFixture = id
		}
	}
	for _, f := range w.competitions.Fixtures(w.leagues[0].season) {
		if f.Home == team || f.Away == team {
			switch {
			case f.Round == 1:
				earlierFixture = f.ID
			case f.Round == 4:
				laterFixture = f.ID
			}
		}
	}
	var foreigner ids.PlayerID
	for _, a := range w.employment.Assignments() {
		if a.Team != team {
			foreigner = a.Player
			break
		}
	}
	squad := w.employment.Squad(team)
	var unused []ids.PlayerID
	for _, id := range squad {
		if !slices.Contains(good.Players(), id) {
			unused = append(unused, id)
		}
	}

	type tc struct {
		mutate func(*SubmitLineup)
		want   error
	}
	cases := map[string]tc{
		"zero ID":          {func(c *SubmitLineup) { c.ID = 0 }, ErrInvalidCommand},
		"stale revision":   {func(c *SubmitLineup) { c.ExpectedRevision-- }, ErrStaleRevision},
		"future revision":  {func(c *SubmitLineup) { c.ExpectedRevision++ }, ErrStaleRevision},
		"other fixture":    {func(c *SubmitLineup) { c.Fixture = otherFixture }, ErrNotUserFixture},
		"unknown fixture":  {func(c *SubmitLineup) { c.Fixture = 999 }, ErrNotUserFixture},
		"zero fixture":     {func(c *SubmitLineup) { c.Fixture = 0 }, ErrNotUserFixture},
		"later fixture":    {func(c *SubmitLineup) { c.Fixture = laterFixture }, ErrFixtureNotPending},
		"played fixture":   {func(c *SubmitLineup) { c.Fixture = earlierFixture }, ErrFixtureNotPending},
		"ten starters":     {func(c *SubmitLineup) { c.Lineup.Starters = c.Lineup.Starters[1:] }, ErrInvalidLineup},
		"two goalkeepers":  {func(c *SubmitLineup) { c.Lineup.Starters[1].Role = matches.Goalkeeper }, ErrInvalidLineup},
		"no goalkeeper":    {func(c *SubmitLineup) { c.Lineup.Starters[0].Role = matches.Defender }, ErrInvalidLineup},
		"invalid role":     {func(c *SubmitLineup) { c.Lineup.Starters[5].Role = 8 }, ErrInvalidLineup},
		"duplicate player": {func(c *SubmitLineup) { c.Lineup.Bench[0] = c.Lineup.Starters[3].Player }, ErrInvalidLineup},
		"other club's player": {
			func(c *SubmitLineup) { c.Lineup.Starters[4].Player = foreigner }, ErrInvalidLineup},
		"unknown player":      {func(c *SubmitLineup) { c.Lineup.Bench[1] = 9999 }, ErrInvalidLineup},
		"no mentality":        {func(c *SubmitLineup) { c.Lineup.Tactics = matches.Tactics{} }, ErrInvalidLineup},
		"bench over MaxBench": {func(c *SubmitLineup) { c.Lineup.Bench = append(c.Lineup.Bench, unused[0]) }, ErrInvalidLineup},
	}
	if len(unused) == 0 || foreigner == 0 || laterFixture == 0 || earlierFixture == 0 || otherFixture == 0 {
		t.Fatal("test setup found no candidates")
	}
	before := w.Snapshot()
	for name, c := range cases {
		cmd := SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: good.Clone()}
		c.mutate(&cmd)
		if _, err := w.SubmitLineup(cmd); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
		if !reflect.DeepEqual(w.Snapshot(), before) {
			t.Fatalf("%s: rejected command changed the world", name)
		}
	}

	plain := newWorld(t, 42)
	r := readyBatch(t, plain)
	_, err := plain.SubmitLineup(SubmitLineup{ID: 1, ExpectedRevision: r.Revision, Fixture: r.Rounds[0].Fixtures[0], Lineup: good})
	if !errors.Is(err, ErrNoUserClub) {
		t.Fatalf("world without a user club: err = %v", err)
	}
	if _, err := plain.SuggestLineup(r.Rounds[0].Fixtures[0]); !errors.Is(err, ErrNoUserClub) {
		t.Fatalf("suggest without a user club: err = %v", err)
	}
	if _, err := w.SuggestLineup(laterFixture); !errors.Is(err, ErrFixtureNotPending) {
		t.Fatalf("suggest for a later fixture: err = %v", err)
	}
}

func TestLineupRetriesAndResubmission(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ready := readyBatch(t, w)
	fixture := ready.UserFixtures[0]
	first := changedLineup(t, w, fixture)
	cmd := SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: first}
	res, err := w.SubmitLineup(cmd)
	if err != nil {
		t.Fatal(err)
	}

	before := w.Snapshot()
	again, err := w.SubmitLineup(SubmitLineup{ID: cmd.ID, ExpectedRevision: cmd.ExpectedRevision, Fixture: fixture, Lineup: first.Clone()})
	if err != nil || again != res || !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatalf("retry: %+v, %v; or the world changed", again, err)
	}
	cmd.Lineup.Starters[0], cmd.Lineup.Starters[1] = cmd.Lineup.Starters[1], cmd.Lineup.Starters[0]
	if _, err := w.SubmitLineup(cmd); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused ID with a different lineup: err = %v", err)
	}
	if _, err := w.ResolveRounds(ResolveRounds{ID: res.Command, ExpectedRevision: w.Revision(), Rounds: commandFor(ready, 0).Rounds}); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("resolve with a lineup command's ID: err = %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("rejected retries changed the world")
	}

	// A resubmission replaces the lineup; the replacement is played.
	second := first.Clone()
	second.Tactics.Mentality = matches.Defensive
	submit(t, w, fixture, second)
	if got, _ := w.SubmittedLineup(fixture); !got.Equal(second) {
		t.Fatal("resubmission did not replace the lineup")
	}
	plan, err := w.prepareBatch(commandFor(ready, 0).Rounds)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan {
		if p.fixture.ID == fixture {
			if s := p.input.Team(sideOf(p.fixture.Home, mustUserTeam(t, w))); s.Tactics.Mentality != matches.Defensive {
				t.Fatalf("played mentality %s, want the resubmitted defensive", s.Tactics.Mentality)
			}
		}
	}
	resolved := resolveNow(t, w)
	if _, err := w.SubmitLineup(SubmitLineup{ID: resolved.Command, ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: second}); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("lineup with a resolve command's ID: err = %v", err)
	}
	if w.NextCommandID() != resolved.Command+1 || len(w.Snapshot().LineupCommands) != 2 {
		t.Fatal("command log does not hold both lineup commands")
	}
}

func sideOf(home, team ids.TeamID) matches.Side {
	if home == team {
		return matches.Home
	}
	return matches.Away
}

func TestLineupSurvivesSaveWhilePending(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playBatches(t, w, 2)
	ready := readyBatch(t, w)
	fixture := ready.UserFixtures[0]
	l := changedLineup(t, w, fixture)
	submitted := submit(t, w, fixture, l)

	loaded := roundTrip(t, w)
	if club, _ := loaded.UserClub(); club != userClub {
		t.Fatal("user club lost")
	}
	if got, ok := loaded.SubmittedLineup(fixture); !ok || !got.Equal(l) {
		t.Fatal("submitted lineup lost")
	}
	if p1, _ := w.Pending(); !reflect.DeepEqual(readyBatch(t, loaded), p1) {
		t.Fatal("pending batch differs after load")
	}
	// A retry of the lineup command is answered from the restored log.
	if again, err := loaded.SubmitLineup(SubmitLineup{ID: submitted.Command, ExpectedRevision: ready.Revision, Fixture: fixture, Lineup: l}); err != nil || again != submitted {
		t.Fatalf("retry after load: %+v, %v", again, err)
	}

	cmd := commandFor(ready, w.NextCommandID())
	cmd.ExpectedRevision = w.Revision()
	want, err := w.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.ResolveRounds(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(loaded.Snapshot(), w.Snapshot()) {
		t.Fatal("the restored lineup produced a different result")
	}
	// Completed matches with lineups keep round-tripping to the season end.
	pick := func(f ids.FixtureID) selection.Lineup { return changedLineup(t, loaded, f) }
	playWithLineups(t, loaded, pick)
	roundTrip(t, loaded)
}

func TestRestoreRejectsInvalidLineupState(t *testing.T) {
	var team ids.TeamID
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		team = mustUserTeam(t, w)
		for range 2 {
			ready := readyBatch(t, w)
			submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
			resolveNow(t, w)
		}
		ready := readyBatch(t, w)
		submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
		return w.Snapshot()
	}
	base := build()
	if len(base.Lineups) != 3 || len(base.LineupCommands) != 3 || len(base.ResolveCommands) != 2 {
		t.Fatalf("setup: %d lineups, %d lineup commands, %d resolve commands", len(base.Lineups), len(base.LineupCommands), len(base.ResolveCommands))
	}
	var foreigner ids.PlayerID
	var otherTeam ids.TeamID
	for _, a := range base.Employment {
		if a.Team != team {
			foreigner, otherTeam = a.Player, a.Team
			break
		}
	}
	laterFixture := func(s *WorldSnapshot) ids.FixtureID {
		for _, f := range s.Competitions.Seasons[0].Fixtures {
			if f.Round == 5 && (f.Home == team || f.Away == team) {
				return f.ID
			}
		}
		t.Fatal("no round-5 fixture")
		return 0
	}
	cases := map[string]func(*WorldSnapshot){
		"unknown user club":         func(s *WorldSnapshot) { s.UserClub = 99 },
		"lineups without user club": func(s *WorldSnapshot) { s.UserClub = 0 },
		"another user club":         func(s *WorldSnapshot) { s.UserClub = userClub + 1 },
		"lineup for another team":   func(s *WorldSnapshot) { s.Lineups[2].Team = otherTeam },
		"lineup for unplayed fixture": func(s *WorldSnapshot) {
			s.Lineups[2].Fixture = laterFixture(s)
		},
		"lineup player not in squad": func(s *WorldSnapshot) { s.Lineups[2].Lineup.Starters[3].Player = foreigner },
		"lineup two goalkeepers":     func(s *WorldSnapshot) { s.Lineups[2].Lineup.Starters[2].Role = matches.Goalkeeper },
		"lineup bench too large": func(s *WorldSnapshot) {
			l := &s.Lineups[2].Lineup
			for _, a := range s.Employment {
				if a.Team == team && !slices.Contains(l.Players(), a.Player) {
					l.Bench = append(l.Bench, a.Player)
				}
			}
		},
		"lineup removed for pending command": func(s *WorldSnapshot) { s.Lineups = s.Lineups[:2] },
		"lineup removed for played fixture":  func(s *WorldSnapshot) { s.Lineups = s.Lineups[1:] },
		"report says AI": func(s *WorldSnapshot) {
			s.ResolveCommands[0].Result.Matches[userMatch(s, 0, team)].Selected = [2]SelectedBy{1, 1}
		},
		"report selection invalid": func(s *WorldSnapshot) {
			s.ResolveCommands[1].Result.Matches[0].Selected[0] = 0
		},
		"lineup command unknown fixture": func(s *WorldSnapshot) {
			s.LineupCommands[0].Request.Fixture, s.LineupCommands[0].Result.Fixture = 999, 999
		},
		"lineup command fixture mismatch": func(s *WorldSnapshot) { s.LineupCommands[0].Result.Fixture = s.LineupCommands[1].Request.Fixture },
		"lineup command for other team":   func(s *WorldSnapshot) { s.LineupCommands[0].Result.Team = otherTeam },
		"lineup command invalid lineup":   func(s *WorldSnapshot) { s.LineupCommands[0].Request.Lineup.Starters = nil },
		"lineup command duplicate ID": func(s *WorldSnapshot) {
			id := s.ResolveCommands[0].Request.ID
			s.LineupCommands[0].Request.ID, s.LineupCommands[0].Result.Command = id, id
		},
		"lineup command future revision": func(s *WorldSnapshot) { s.LineupCommands[2].Result.Revision = s.Revision + 1 },
		"lineup command unplayed fixture": func(s *WorldSnapshot) {
			f := laterFixture(s)
			s.LineupCommands[2].Request.Fixture, s.LineupCommands[2].Result.Fixture = f, f
		},
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		w, err := Restore(snap)
		if err == nil || w != nil {
			t.Errorf("%s: Restore succeeded", name)
			continue
		}
		if !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v, want ErrInvalidSave", name, err)
		}
	}
	if _, err := Restore(base); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// userMatch returns the index of the user team's match in a resolve record.
func userMatch(s *WorldSnapshot, cmd int, team ids.TeamID) int {
	for i, m := range s.ResolveCommands[cmd].Result.Matches {
		if m.Home.Team == team || m.Away.Team == team {
			return i
		}
	}
	return -1
}

// userSide returns the index of team's side in a match report, or -1.
func userSide(m MatchReport, team ids.TeamID) int {
	switch team {
	case m.Home.Team:
		return 0
	case m.Away.Team:
		return 1
	}
	return -1
}

func TestLineupCarriesOverToLaterMatches(t *testing.T) {
	// The manager submits one lineup in the first round and nothing after.
	// Every later match carries it over, exactly as if the manager had
	// submitted the carried lineup each time.
	carried, explicit := userWorld(t, 42, userClub), userWorld(t, 42, userClub)
	team := mustUserTeam(t, carried)
	var last ids.FixtureID
	batches := 0
	for {
		r1, r2 := mustContinue(t, carried, seasonEnd(carried)), mustContinue(t, explicit, seasonEnd(explicit))
		ready, ok := r1.(FixtureRoundReady)
		if _, ok2 := r2.(FixtureRoundReady); ok != ok2 {
			t.Fatal("the worlds diverged before a matchday")
		}
		if !ok {
			break
		}
		batches++
		plays := map[ids.FixtureID]selection.Lineup{}
		for _, f := range ready.UserFixtures {
			m, err := carried.MatchdayLineup(f)
			if err != nil {
				t.Fatal(err)
			}
			if last == 0 {
				suggested, _ := carried.SuggestLineup(f)
				if m.Source != LineupSuggested || !m.Lineup.Equal(suggested) || m.From != 0 {
					t.Fatalf("first matchday: %+v, want the suggestion", m)
				}
				submit(t, carried, f, changedLineup(t, carried, f))
				if m, _ = carried.MatchdayLineup(f); m.Source != LineupFromSubmission {
					t.Fatalf("after submitting: source %s", m.Source)
				}
			} else if m.Source != LineupCarriedOver || m.From != last || len(m.Dropped) != 0 {
				t.Fatalf("fixture %d: source %s from %d, dropped %v; want carried over from %d", f, m.Source, m.From, m.Dropped, last)
			}
			plays[f] = m.Lineup
			submit(t, explicit, f, m.Lineup)
			last = f
		}
		got, want := resolveNow(t, carried), resolveNow(t, explicit)
		if !reflect.DeepEqual(got.Matches, want.Matches) {
			t.Fatalf("batch %d: carrying over differs from submitting the same lineup", batches)
		}
		for _, m := range got.Matches {
			if side := userSide(m, team); side >= 0 && m.Selected[side] != SelectedByManager {
				t.Fatalf("fixture %d: user side selected by %s", m.Fixture, m.Selected[side])
			}
		}
		for f, l := range plays {
			if stored, ok := carried.SubmittedLineup(f); !ok || !stored.Equal(l) {
				t.Fatalf("fixture %d: the lineup played was not stored", f)
			}
		}
	}
	if batches != 14 {
		t.Fatalf("%d batches", batches)
	}
	if err := carried.Validate(); err != nil {
		t.Fatal(err)
	}
	roundTrip(t, carried)
}

func TestCarriedLineupReplacesPlayersWhoLeft(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	ready := mustContinue(t, w, seasonEnd(w)).(FixtureRoundReady)
	first := ready.UserFixtures[0]
	saved := changedLineup(t, w, first)
	submit(t, w, first, saved)
	resolveNow(t, w)

	// Release the first outfield starter the rules allow.
	gone := -1
	for i, s := range saved.Starters {
		if s.Role == matches.Goalkeeper {
			continue
		}
		if _, err := w.ReleasePlayer(ReleasePlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: s.Player}); err == nil {
			gone = i
			break
		}
	}
	if gone < 0 {
		t.Fatal("no starter could be released")
	}

	ready = mustContinue(t, w, seasonEnd(w)).(FixtureRoundReady)
	next := ready.UserFixtures[0]
	m, err := w.MatchdayLineup(next)
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != LineupCarriedOver || m.From != first || !slices.Equal(m.Dropped, []ids.PlayerID{saved.Starters[gone].Player}) {
		t.Fatalf("source %s from %d, dropped %v; want player %d dropped from fixture %d", m.Source, m.From, m.Dropped, saved.Starters[gone].Player, first)
	}
	if m.Lineup.Tactics != saved.Tactics {
		t.Fatal("tactics not carried over")
	}
	for i, s := range m.Lineup.Starters {
		switch {
		case i == gone && (s.Role != saved.Starters[i].Role || s.Player == saved.Starters[i].Player):
			t.Fatalf("slot %d = %+v, want another role %d player", i, s, saved.Starters[i].Role)
		case i != gone && s != saved.Starters[i]:
			t.Fatalf("slot %d moved: %+v, was %+v", i, s, saved.Starters[i])
		}
	}
	res := resolveNow(t, w)
	for _, r := range res.Matches {
		if side := userSide(r, team); side >= 0 && r.Selected[side] != SelectedByManager {
			t.Fatal("the refilled lineup was not played")
		}
	}
	if stored, _ := w.SubmittedLineup(next); !stored.Equal(m.Lineup) {
		t.Fatal("the refilled lineup was not stored")
	}
	roundTrip(t, w)
}

// The match contract carries every player attribute: matches.Ratings has one
// field per players.Attribute, in the same order, and matchPlayer copies them.
func TestMatchPlayerCopiesEveryAttribute(t *testing.T) {
	w := newWorld(t, 42)
	if n := reflect.TypeFor[matches.Ratings]().NumField(); n != players.NumAttributes {
		t.Fatalf("matches.Ratings has %d fields, players have %d attributes", n, players.NumAttributes)
	}
	checked := 0
	for _, id := range w.players.PlayerIDs() {
		if _, ok := w.medical.Condition(id); !ok {
			continue // retired
		}
		c, err := w.matchPlayer(id)
		if err != nil {
			t.Fatal(err)
		}
		p, _ := w.players.Profile(id)
		if c.Role != roleOf(p.Position) {
			t.Fatalf("player %d plays %d, natural %s", id, c.Role, p.Position)
		}
		r := reflect.ValueOf(c.Ratings)
		for a := range players.NumAttributes {
			if got, want := r.Field(int(a)).Uint(), uint64(p.Attributes[a]); got != want {
				t.Fatalf("player %d: %s is %d in the match input, %d in the profile", id, r.Type().Field(int(a)).Name, got, want)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no active players")
	}
}

// SquadEligibility lists the whole squad, and a lineup of the players it
// calls selectable is accepted. A player injured after the preview is
// rejected at submission with the reason.
func TestSquadEligibilityAgreesWithSubmission(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	fixture := readyBatch(t, w).UserFixtures[0]
	got, err := w.SquadEligibility(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var listed []ids.PlayerID
	for _, e := range got {
		listed = append(listed, e.Player)
		if e.Eligibility != EligibleFit || e.DaysOut != 0 {
			t.Fatalf("player %d in a fit squad: %+v", e.Player, e)
		}
	}
	if !slices.Equal(listed, w.employment.Squad(team)) {
		t.Fatalf("eligibility lists %v, squad is %v", listed, w.employment.Squad(team))
	}

	l, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	hurt := l.Starters[3].Player
	injure(t, w, hurt, 5)
	got, err = w.SquadEligibility(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		want := LineupEligibility{Player: e.Player, Eligibility: EligibleFit}
		if e.Player == hurt {
			want = LineupEligibility{Player: hurt, Eligibility: IneligibleInjured, DaysOut: 5}
		}
		if e != want {
			t.Fatalf("eligibility %+v, want %+v", e, want)
		}
	}
	cmd := SubmitLineup{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: l}
	if _, err := w.SubmitLineup(cmd); !errors.Is(err, ErrInvalidLineup) || !strings.Contains(err.Error(), "injured") {
		t.Fatalf("submitting a player injured since the preview: %v", err)
	}
	if l, err = w.SuggestLineup(fixture); err != nil {
		t.Fatal(err)
	}
	selectable := map[ids.PlayerID]bool{}
	for _, e := range got {
		selectable[e.Player] = e.Eligibility.Selectable()
	}
	for _, p := range l.Players() {
		if !selectable[p] {
			t.Fatalf("the AI suggests unselectable player %d", p)
		}
	}
	submit(t, w, fixture, l)

	if _, err := w.SquadEligibility(0); !errors.Is(err, ErrNotUserFixture) {
		t.Fatalf("fixture 0: %v", err)
	}
	resolveNow(t, w)
	if _, err := w.SquadEligibility(fixture); !errors.Is(err, ErrFixtureNotPending) {
		t.Fatalf("a played fixture: %v", err)
	}
}

// When the fit players cannot field a lineup, the injured are selectable
// too, and SquadEligibility says so.
func TestSquadEligibilityShowsTheEmergencyRule(t *testing.T) {
	w := userWorld(t, 42, userClub)
	fixture := readyBatch(t, w).UserFixtures[0]
	var keepers []ids.PlayerID
	for _, id := range w.employment.Squad(mustUserTeam(t, w)) {
		if p, _ := w.players.Profile(id); p.Position == players.Goalkeeper {
			injure(t, w, id, 10)
			keepers = append(keepers, id)
		}
	}
	got, err := w.SquadEligibility(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		want := EligibleFit
		if slices.Contains(keepers, e.Player) {
			want = EligibleInjured
		}
		if e.Eligibility != want || !e.Eligibility.Selectable() {
			t.Fatalf("player %d: %s, want %s", e.Player, e.Eligibility, want)
		}
	}
	l, err := w.SuggestLineup(fixture)
	if err != nil {
		t.Fatal(err)
	}
	submit(t, w, fixture, l)
}

func setPlan(t *testing.T, w *World, l selection.Lineup) TeamPlanSaved {
	t.Helper()
	res, err := w.SetTeamPlan(SetTeamPlan{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Lineup: l})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func mustTeamPlan(t *testing.T, w *World) TeamPlan {
	t.Helper()
	p, err := w.TeamPlan()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// attackingPlan is the plan's starting point made attacking, with the last
// starting forward swapped for the first outfield substitute.
func attackingPlan(t *testing.T, w *World) selection.Lineup {
	t.Helper()
	l := mustTeamPlan(t, w).Lineup
	l.Tactics.Mentality = matches.Attacking
	for i, id := range l.Bench {
		if p, _ := w.players.Profile(id); p.Position != players.Goalkeeper {
			l.Starters[10].Player, l.Bench[i] = id, l.Starters[10].Player
			return l
		}
	}
	t.Fatal("no outfield substitute")
	return l
}

// The manager saves a plan before the first matchday. Every user match
// then plays the plan, exactly as if it had been submitted each time, and
// the plan never becomes a carry-over.
func TestTeamPlanIsPlayedWithoutAMatchday(t *testing.T) {
	planned, explicit := userWorld(t, 42, userClub), userWorld(t, 42, userClub)
	team := mustUserTeam(t, planned)
	if _, ok := planned.Pending(); ok {
		t.Fatal("setup: a matchday is pending")
	}
	start := mustTeamPlan(t, planned)
	if start.Saved || len(start.Unavailable) != 0 || len(start.Squad) != len(planned.employment.Squad(team)) {
		t.Fatalf("unsaved plan: saved %t, unavailable %v, squad of %d", start.Saved, start.Unavailable, len(start.Squad))
	}
	if err := start.Lineup.Validate(); err != nil {
		t.Fatal(err)
	}
	plan := attackingPlan(t, planned)
	rev := planned.Revision()
	res := setPlan(t, planned, plan)
	if res.Revision != rev+1 || res.Team != team {
		t.Fatalf("result %+v", res)
	}
	evs := planned.Events()
	if last := evs[len(evs)-1]; last.Kind != events.KindTeamPlanSaved || last.TeamPlanSaved.Team != team {
		t.Fatalf("last event %s", last.Kind)
	}
	if got := mustTeamPlan(t, planned); !got.Saved || !got.Lineup.Equal(plan) {
		t.Fatal("the saved plan is not returned")
	}
	planned = roundTrip(t, planned)

	for batch := 0; batch < 5; batch++ {
		ready := mustContinue(t, planned, seasonEnd(planned)).(FixtureRoundReady)
		mustContinue(t, explicit, seasonEnd(explicit))
		for _, f := range ready.UserFixtures {
			m, err := planned.MatchdayLineup(f)
			if err != nil {
				t.Fatal(err)
			}
			if m.Source != LineupFromPlan || m.From != 0 {
				t.Fatalf("batch %d: source %s from %d, want the plan", batch, m.Source, m.From)
			}
			if len(m.Dropped) == 0 && !m.Lineup.Equal(plan) {
				t.Fatalf("batch %d: nobody dropped, but the lineup differs from the plan", batch)
			}
			submit(t, explicit, f, m.Lineup)
		}
		got, want := resolveNow(t, planned), resolveNow(t, explicit)
		if !reflect.DeepEqual(got.Matches, want.Matches) {
			t.Fatalf("batch %d: playing the plan differs from submitting it", batch)
		}
		for _, m := range got.Matches {
			if side := userSide(m, team); side >= 0 && m.Selected[side] != SelectedByManager {
				t.Fatalf("fixture %d: user side selected by %s", m.Fixture, m.Selected[side])
			}
		}
	}
	if got := mustTeamPlan(t, planned); !got.Lineup.Equal(plan) {
		t.Fatal("playing matches changed the plan")
	}
	roundTrip(t, planned)
}

// A lineup submitted for a fixture beats the plan, the plan beats a
// carried-over lineup, and saving a plan changes no stored lineup.
func TestTeamPlanPrecedence(t *testing.T) {
	w := userWorld(t, 42, userClub)
	ready := readyBatch(t, w)
	first := ready.UserFixtures[0]
	submitted := changedLineup(t, w, first)
	submit(t, w, first, submitted)
	plan := attackingPlan(t, w)
	plan.Tactics.Mentality = matches.Defensive
	setPlan(t, w, plan)
	if m, _ := w.MatchdayLineup(first); m.Source != LineupFromSubmission || !m.Lineup.Equal(submitted) {
		t.Fatalf("source %s, want the submission", m.Source)
	}
	resolveNow(t, w)
	if stored, _ := w.SubmittedLineup(first); !stored.Equal(submitted) {
		t.Fatal("the plan changed a played lineup")
	}

	ready = readyBatch(t, w)
	next := ready.UserFixtures[0]
	if m, _ := w.MatchdayLineup(next); m.Source != LineupFromPlan || m.Lineup.Tactics.Mentality != matches.Defensive {
		t.Fatalf("source %s, want the plan over the carried-over lineup", m.Source)
	}
	// Saving another plan on matchday changes the pending lineup, but not a
	// lineup submitted for the fixture.
	plan.Tactics.Mentality = matches.Balanced
	setPlan(t, w, plan)
	if m, _ := w.MatchdayLineup(next); m.Source != LineupFromPlan || m.Lineup.Tactics.Mentality != matches.Balanced {
		t.Fatal("the new plan does not apply on matchday")
	}
	submit(t, w, next, submitted)
	setPlan(t, w, attackingPlan(t, w))
	if got, _ := w.SubmittedLineup(next); !got.Equal(submitted) {
		t.Fatal("saving a plan changed a submitted lineup")
	}
	roundTrip(t, w)
}

// A plan keeps players who cannot play today. Each match leaves them out,
// refills their places and cuts the bench, and they return when they can
// play again.
func TestTeamPlanKeepsUnavailablePlayers(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	playBatches(t, w, 1) // later rounds are a week apart: the injury lasts
	plan := mustTeamPlan(t, w).Lineup
	for _, id := range w.employment.Squad(team) { // the whole squad on the bench
		if !slices.Contains(plan.Players(), id) {
			plan.Bench = append(plan.Bench, id)
		}
	}
	hurt, gone := plan.Starters[6].Player, plan.Starters[7].Player
	injure(t, w, hurt, 30)
	setPlan(t, w, plan) // injured players may be planned
	if _, err := w.ReleasePlayer(ReleasePlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: gone}); err != nil {
		t.Fatal(err)
	}
	p := mustTeamPlan(t, w)
	if !p.Lineup.Equal(plan) || !slices.Equal(p.Unavailable, []ids.PlayerID{hurt, gone}) {
		t.Fatalf("unavailable %v, want %d (injured) and %d (released)", p.Unavailable, hurt, gone)
	}

	ready := readyBatch(t, w)
	rules, err := w.leagueRules(team)
	if err != nil {
		t.Fatal(err)
	}
	m, err := w.MatchdayLineup(ready.UserFixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != LineupFromPlan || len(m.Lineup.Bench) != int(rules.MaxBench) {
		t.Fatalf("source %s, bench of %d; want the plan with a bench of %d", m.Source, len(m.Lineup.Bench), rules.MaxBench)
	}
	if !slices.Contains(m.Dropped, hurt) || !slices.Contains(m.Dropped, gone) || slices.Contains(m.Lineup.Players(), hurt) {
		t.Fatalf("dropped %v, want %d and %d among them", m.Dropped, hurt, gone)
	}
	for i, s := range m.Lineup.Starters {
		if i != 6 && i != 7 && s != plan.Starters[i] {
			t.Fatalf("slot %d moved: %+v, was %+v", i, s, plan.Starters[i])
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	w = roundTrip(t, w)
	resolveNow(t, w)
	if stored, _ := w.SubmittedLineup(ready.UserFixtures[0]); !stored.Equal(m.Lineup) {
		t.Fatal("the fitted plan was not stored for its fixture")
	}

	for range 40 { // the injury heals within the season
		if _, injured := w.medical.DaysOut(hurt); !injured {
			break
		}
		readyBatch(t, w)
		resolveNow(t, w)
	}
	if _, injured := w.medical.DaysOut(hurt); injured {
		t.Fatal("setup: the injury never healed")
	}
	if p := mustTeamPlan(t, w); slices.Contains(p.Unavailable, hurt) {
		t.Fatal("a recovered player is still unavailable")
	}
	ready = readyBatch(t, w)
	if m, _ := w.MatchdayLineup(ready.UserFixtures[0]); m.Lineup.Starters[6].Player != hurt {
		t.Fatal("the recovered player did not return to his planned place")
	}
}

func TestSetTeamPlanRejections(t *testing.T) {
	w := userWorld(t, 42, userClub)
	team := mustUserTeam(t, w)
	plan := mustTeamPlan(t, w).Lineup
	var foreigner ids.PlayerID
	for _, a := range w.employment.Snapshot() {
		if a.Team != team {
			foreigner = a.Player
			break
		}
	}
	twoKeepers := plan.Clone()
	twoKeepers.Starters[1].Role = matches.Goalkeeper
	stranger := plan.Clone()
	stranger.Bench = append(stranger.Bench, foreigner)
	id := w.NextCommandID()
	cases := map[string]struct {
		cmd  SetTeamPlan
		want error
	}{
		"zero ID":        {SetTeamPlan{ExpectedRevision: w.Revision(), Lineup: plan}, ErrInvalidCommand},
		"stale revision": {SetTeamPlan{ID: id, ExpectedRevision: w.Revision() + 1, Lineup: plan}, ErrStaleRevision},
		"invalid shape":  {SetTeamPlan{ID: id, ExpectedRevision: w.Revision(), Lineup: twoKeepers}, ErrInvalidLineup},
		"foreign player": {SetTeamPlan{ID: id, ExpectedRevision: w.Revision(), Lineup: stranger}, ErrInvalidLineup},
	}
	before := w.Snapshot()
	for name, c := range cases {
		if _, err := w.SetTeamPlan(c.cmd); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("a rejected plan changed the world")
	}

	cmd := SetTeamPlan{ID: id, ExpectedRevision: w.Revision(), Lineup: plan}
	res := setPlan(t, w, plan)
	if again, err := w.SetTeamPlan(cmd); err != nil || again != res {
		t.Fatalf("retry: %+v, %v", again, err)
	}
	loaded := roundTrip(t, w)
	if again, err := loaded.SetTeamPlan(cmd); err != nil || again != res {
		t.Fatalf("retry after load: %+v, %v", again, err)
	}
	cmd.Lineup = twoKeepers
	if _, err := w.SetTeamPlan(cmd); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused ID: %v", err)
	}

	// A live match keeps its lineup until it is over.
	fixture := readyBatch(t, w).UserFixtures[0]
	playTo(t, w, fixture, 45)
	if _, err := w.SetTeamPlan(SetTeamPlan{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Lineup: plan}); !errors.Is(err, ErrMatchInProgress) {
		t.Fatalf("during a live match: %v", err)
	}

	unmanaged := userWorld(t, 42, 0)
	if _, err := unmanaged.TeamPlan(); !errors.Is(err, ErrNoUserClub) {
		t.Fatalf("TeamPlan without a user club: %v", err)
	}
	if _, err := unmanaged.SetTeamPlan(SetTeamPlan{ID: 1, ExpectedRevision: unmanaged.Revision(), Lineup: plan}); !errors.Is(err, ErrNoUserClub) {
		t.Fatalf("SetTeamPlan without a user club: %v", err)
	}
}

func TestRestoreRejectsInvalidTeamPlans(t *testing.T) {
	var team, otherTeam ids.TeamID
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		team = mustUserTeam(t, w)
		setPlan(t, w, mustTeamPlan(t, w).Lineup)
		setPlan(t, w, attackingPlan(t, w))
		return w.Snapshot()
	}
	base := build()
	if len(base.TeamPlans) != 1 || len(base.TeamPlanCommands) != 2 {
		t.Fatalf("setup: %d plans, %d plan commands", len(base.TeamPlans), len(base.TeamPlanCommands))
	}
	for _, a := range base.Employment {
		if a.Team != team {
			otherTeam = a.Team
			break
		}
	}
	cases := map[string]func(*WorldSnapshot){
		"plan without user club":    func(s *WorldSnapshot) { s.UserClub = 0 },
		"plan of another team":      func(s *WorldSnapshot) { s.TeamPlans[0].Team = otherTeam },
		"two plans":                 func(s *WorldSnapshot) { s.TeamPlans = append(s.TeamPlans, s.TeamPlans[0]) },
		"plan two goalkeepers":      func(s *WorldSnapshot) { s.TeamPlans[0].Lineup.Starters[2].Role = matches.Goalkeeper },
		"plan unregistered player":  func(s *WorldSnapshot) { s.TeamPlans[0].Lineup.Bench[0] = 99999 },
		"plan removed":              func(s *WorldSnapshot) { s.TeamPlans = nil },
		"plan command other team":   func(s *WorldSnapshot) { s.TeamPlanCommands[0].Result.Team = otherTeam },
		"plan command invalid":      func(s *WorldSnapshot) { s.TeamPlanCommands[0].Request.Lineup.Starters = nil },
		"plan command future":       func(s *WorldSnapshot) { s.TeamPlanCommands[1].Result.Revision = s.Revision + 1 },
		"plan command duplicate ID": func(s *WorldSnapshot) { s.TeamPlanCommands[1] = s.TeamPlanCommands[0] },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	if _, err := Restore(base); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// Every report keeps what both sides fielded: the manager's stored lineup on
// his side, and on the AI's side exactly what ProbableLineup showed just
// before the round.
func TestReportsKeepBothPlayedLineups(t *testing.T) {
	w := userWorld(t, 42, userClub)
	mine := mustUserTeam(t, w)
	var reports []MatchReport
	for range 3 {
		ready := readyBatch(t, w)
		submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
		probable := map[ids.TeamID]selection.Lineup{}
		for _, r := range w.competitions.PendingRounds() {
			for _, id := range r.Fixtures {
				f, _ := w.competitions.Fixture(id)
				for _, team := range []ids.TeamID{f.Home, f.Away} {
					if team == mine {
						continue
					}
					l, err := w.ProbableLineup(team)
					if err != nil {
						t.Fatal(err)
					}
					probable[team] = l
				}
			}
		}
		res := resolveNow(t, w)
		for _, m := range res.Matches {
			for i, team := range []ids.TeamID{m.Home.Team, m.Away.Team} {
				want, ok := probable[team]
				if team == mine {
					want, ok = w.SubmittedLineup(m.Fixture)
				}
				if !ok || !m.Lineups[i].Equal(want) {
					t.Fatalf("fixture %d team %d played %+v, expected %+v", m.Fixture, team, m.Lineups[i], want)
				}
			}
			reports = append(reports, m)
		}
	}
	if len(reports) == 0 {
		t.Fatal("no reports")
	}
	loaded := roundTrip(t, w)
	for _, m := range reports {
		for i := range m.Lineups {
			if err := m.Lineups[i].Validate(); err != nil {
				t.Fatalf("fixture %d: %v", m.Fixture, err)
			}
		}
		got, ok := loaded.MatchReport(m.Fixture)
		if !ok || !reflect.DeepEqual(got, m) {
			t.Fatalf("fixture %d report differs after load", m.Fixture)
		}
		// A returned report is a copy.
		got.Lineups[0].Starters[0].Player = 0
		if again, _ := loaded.MatchReport(m.Fixture); again.Lineups[0].Starters[0].Player == 0 {
			t.Fatal("MatchReport exposes internal lineup storage")
		}
	}
}

func TestProbableLineupRejections(t *testing.T) {
	w := userWorld(t, 42, userClub)
	if _, err := w.ProbableLineup(mustUserTeam(t, w)); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("user team: err = %v", err)
	}
	if _, err := w.ProbableLineup(9999); err == nil {
		t.Fatal("unknown team accepted")
	}
	before := w.Snapshot()
	if _, err := w.ProbableLineup(1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("ProbableLineup changed the world")
	}
}

func TestRestoreRejectsInvalidReportLineups(t *testing.T) {
	var team ids.TeamID
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		team = mustUserTeam(t, w)
		for range 2 {
			ready := readyBatch(t, w)
			submit(t, w, ready.UserFixtures[0], changedLineup(t, w, ready.UserFixtures[0]))
			resolveNow(t, w)
		}
		return w.Snapshot()
	}
	base := build()
	var withGoal int
	for i, m := range base.ResolveCommands[0].Result.Matches {
		if len(m.Goals) > 0 {
			withGoal = i
			break
		}
	}
	cases := map[string]func(*WorldSnapshot){
		"no lineup": func(s *WorldSnapshot) { s.ResolveCommands[0].Result.Matches[0].Lineups[1] = selection.Lineup{} },
		"short lineup": func(s *WorldSnapshot) {
			l := &s.ResolveCommands[0].Result.Matches[0].Lineups[0]
			l.Starters = l.Starters[1:]
		},
		"manager side differs": func(s *WorldSnapshot) {
			m := &s.ResolveCommands[1].Result.Matches[userMatch(s, 1, team)]
			m.Lineups[userSide(*m, team)].Tactics.Mentality = matches.Defensive
		},
		"scorer outside lineup": func(s *WorldSnapshot) {
			m := &s.ResolveCommands[0].Result.Matches[withGoal]
			side := m.Goals[0].Side.Index()
			m.Lineups[side].Starters = slices.Clone(m.Lineups[side].Starters)
			for i := range m.Lineups[side].Starters {
				if m.Lineups[side].Starters[i].Player == m.Goals[0].Scorer {
					m.Lineups[side].Starters[i].Player = 999_999
				}
			}
		},
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		w, err := Restore(snap)
		if err == nil || w != nil {
			t.Errorf("%s: Restore succeeded", name)
			continue
		}
		if !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v, want ErrInvalidSave", name, err)
		}
	}
	if _, err := Restore(base); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// A club's lineup decision depends on the club and its state, not on who
// manages it: the human's suggestion and team plan start are the selection
// the AI makes for the same club, from the same knowledge and to the same
// match input.
func TestSelectionIsControllerIndependent(t *testing.T) {
	human, nobody := userWorld(t, 42, userClub3), userWorld(t, 42, 0)
	team := mustUserTeam(t, human)
	rules, err := human.leagueRules(team)
	if err != nil {
		t.Fatal(err)
	}
	squad := human.availableSquad(team)
	if !slices.Equal(squad, nobody.availableSquad(team)) {
		t.Fatal("setup: different available squads")
	}
	known, err := human.knownCandidates(team, squad)
	if err != nil {
		t.Fatal(err)
	}
	if other, _ := nobody.knownCandidates(team, squad); !reflect.DeepEqual(known, other) {
		t.Fatal("the club knows its players differently under a human and the AI")
	}
	want, err := nobody.ProbableLineup(team)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := human.selectTeam(team, rules)
	if err != nil || !mine.Equal(want) {
		t.Fatalf("human club's selection %+v (%v), AI's %+v", mine, err, want)
	}
	if plan, err := human.TeamPlan(); err != nil || plan.Saved || !plan.Lineup.Equal(want) {
		t.Fatalf("team plan start %+v (%v), want the AI's selection", plan, err)
	}
	a, errA := human.lineupInput(team, mine, rules)
	b, errB := nobody.lineupInput(team, want, rules)
	if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("match inputs differ (%v, %v)", errA, errB)
	}
}

// What a club knows decides whom it picks, never how its players play: a
// selection made from overrated knowledge still takes the field with every
// player's actual attributes and condition.
func TestKnowledgeSteersSelectionNotMatchInput(t *testing.T) {
	w := newWorld(t, 42)
	team := ids.TeamID(1)
	rules, err := w.leagueRules(team)
	if err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	known, err := w.knownCandidates(team, w.availableSquad(team))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range known {
		if actual, _ := w.matchPlayer(c.Player); c.Ratings != actual.Ratings || c.Condition != actual.Condition || c.Natural != actual.Role {
			t.Fatalf("player %d: known %+v, actual %+v; today's knowledge is exact", c.Player, c, actual)
		}
	}
	exact, err := ai.SelectTeam(team, known, rules)
	if err != nil {
		t.Fatal(err)
	}
	// The club believes its worst forward on the bench is a world-beater.
	var dud ids.PlayerID
	for _, c := range known {
		starts := slices.ContainsFunc(exact.Starters, func(s ai.Slot) bool { return s.Player == c.Player })
		if c.Natural == matches.Forward && !starts && (dud == 0 || ai.RoleScore(c.Ratings, matches.Forward) < ai.RoleScore(known[slices.IndexFunc(known, func(x ai.Candidate) bool { return x.Player == dud })].Ratings, matches.Forward)) {
			dud = c.Player
		}
	}
	if dud == 0 {
		t.Fatal("setup: every forward starts")
	}
	overrated := slices.Clone(known)
	for i := range overrated {
		if overrated[i].Player == dud {
			v := uint8(100)
			overrated[i].Ratings = matches.Ratings{Goalkeeping: v, Defending: v, Passing: v, Finishing: v, Pace: v, Stamina: v,
				Dribbling: v, Heading: v, Strength: v, Acceleration: v, Positioning: v}
		}
	}
	sel, err := ai.SelectTeam(team, overrated, rules)
	if err != nil {
		t.Fatal(err)
	}
	in, err := w.lineupInput(team, aiLineup(sel), rules)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(in.Starters, func(p matches.PlayerInput) bool { return p.Player == dud })
	if i < 0 {
		t.Fatalf("the overrated forward %d does not start", dud)
	}
	actual, _ := w.matchPlayer(dud)
	if actual.Role = in.Starters[i].Role; in.Starters[i] != actual {
		t.Fatalf("forward %d takes the field as %+v, actually %+v", dud, in.Starters[i], actual)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("selection changed the world")
	}
}
