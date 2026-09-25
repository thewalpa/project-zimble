package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
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

	// Without submitted lineups the AI selects for the user club too, so the
	// season is the golden one, and every side is reported as AI-selected.
	want := playWithLineups(t, plain, nil)
	got := playWithLineups(t, managed, nil)
	if !reflect.DeepEqual(got, want) || resultsFingerprint(managed) != goldenSeasonSeed42 {
		t.Fatal("a user club without lineups changed the season")
	}
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
				if by != SelectedByAI || !reflect.DeepEqual(in, ai) {
					t.Fatalf("fixture %d: opponent team %d not the AI selection", p.fixture.ID, in.Team)
				}
				continue
			}
			if p.fixture.ID != fixture || by != SelectedByManager || in.Tactics != l.Tactics ||
				len(in.Starters) != len(l.Starters) || len(in.Bench) != len(l.Bench) {
				t.Fatalf("fixture %d: user side %+v, selected by %s", p.fixture.ID, in, by)
			}
			for i, s := range l.Starters {
				c, _ := w.candidate(s.Player)
				if in.Starters[i] != (matches.PlayerInput{Player: s.Player, Role: s.Role, Ratings: c.Ratings, Condition: c.Condition}) {
					t.Fatalf("starter %d is %+v, submitted %+v", i, in.Starters[i], s)
				}
			}
			for i, id := range l.Bench {
				if c, _ := w.candidate(id); in.Bench[i] != (matches.PlayerInput{Player: id, Role: c.Natural, Ratings: c.Ratings, Condition: c.Condition}) {
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
	if resultsFingerprint(w) != goldenSeasonSeed42 {
		t.Fatal("submitting the suggestion changed results")
	}
	want := playWithLineups(t, newWorld(t, 42), nil)
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
	want := playWithLineups(t, newWorld(t, 42), nil)
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
