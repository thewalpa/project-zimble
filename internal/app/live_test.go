package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/medical"
)

func playTo(t *testing.T, w *World, fixture ids.FixtureID, minute uint16) LiveMatch {
	t.Helper()
	res, err := w.PlayMatch(PlayMatch{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: minute})
	if err != nil {
		t.Fatalf("play to %d: %v", minute, err)
	}
	return res.Live
}

func decide(t *testing.T, w *World, fixture ids.FixtureID, c matches.MatchCommand) LiveMatch {
	t.Helper()
	res, err := w.MatchDecision(MatchDecision{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Command: c})
	if err != nil {
		t.Fatalf("decision %+v: %v", c, err)
	}
	return res.Live
}

// liveReady returns a managed world at round 2's kickoff and its user fixture.
func liveReady(t *testing.T) (*World, ids.FixtureID) {
	t.Helper()
	w := userWorld(t, 42, userClub)
	playBatches(t, w, 1)
	return w, readyBatch(t, w).UserFixtures[0]
}

// forwardSub substitutes the manager's last starting forward with the first
// bench forward.
func forwardSub(l LiveMatch) matches.MatchCommand {
	team := l.Teams[l.Side.Index()]
	c := matches.MatchCommand{Kind: matches.CommandSubstitute, Side: l.Side, Out: team.Starters[10].Player}
	for _, p := range team.Bench {
		if p.Role == matches.Forward {
			c.In = p.Player
			break
		}
	}
	return c
}

// comparable strips what legitimately differs between runs with a different
// number of commands: command IDs and revisions.
func comparable(r RoundsResolved) RoundsResolved {
	r = cloneResolved(r)
	r.Command, r.Revision = 0, 0
	return r
}

// Playing the match live in any chunks without decisions gives exactly the
// match ResolveRounds plays directly.
func TestLiveMatchWithoutDecisionsEqualsDirectResolution(t *testing.T) {
	direct, fixture := liveReady(t)
	want := resolveNow(t, direct)

	w, _ := liveReady(t)
	var views []LiveMatch
	for _, m := range []uint16{20, 45, 70, 90} {
		views = append(views, playTo(t, w, fixture, m))
	}
	if views[1].Status != matches.MatchDecisionRequired || views[3].Status != matches.MatchFinished || views[2].Position.Minute != 70 {
		t.Fatalf("statuses %v, %v; minute %d", views[1].Status, views[3].Status, views[2].Position.Minute)
	}
	if l, ok := w.LiveMatch(); !ok || !reflect.DeepEqual(l, views[3]) {
		t.Fatal("LiveMatch query differs from the last step")
	}
	got := resolveNow(t, w)
	if !reflect.DeepEqual(comparable(got), comparable(want)) || !reflect.DeepEqual(conditions(w), conditions(direct)) {
		t.Fatal("a live match without decisions differs from direct resolution")
	}
	// The live view's goals are the final report's goals, as they happened.
	var report MatchReport
	for _, m := range got.Matches {
		if m.Fixture == fixture {
			report = m
		}
	}
	var liveGoals []matches.Goal
	for _, e := range views[3].Events {
		if e.Kind == matches.EventGoal {
			liveGoals = append(liveGoals, matches.Goal{Minute: e.Minute, Side: e.Side, Scorer: e.Player})
		}
	}
	if !slices.Equal(liveGoals, report.Goals) || views[3].View.Score != report.Score {
		t.Fatal("live events differ from the final result")
	}
	if _, ok := w.LiveMatch(); ok || w.live != nil {
		t.Fatal("the live match remained after resolution")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A half-time substitution changes only the manager's match, and the
// players' minutes reach their condition.
func TestHalfTimeSubstitution(t *testing.T) {
	plain, fixture := liveReady(t)
	want := resolveNow(t, plain)

	w, _ := liveReady(t)
	before := conditions(w)
	ht := playTo(t, w, fixture, 60) // stops at half time
	if ht.Position.Minute != 45 || ht.Status != matches.MatchDecisionRequired {
		t.Fatalf("play to 60 from kickoff stopped at %d (%v)", ht.Position.Minute, ht.Status)
	}
	sub := forwardSub(ht)
	after := decide(t, w, fixture, sub)
	if after.View.SubstitutionsUsed[after.Side.Index()] != 1 || !slices.Contains(after.View.OnPitch[after.Side.Index()][:], sub.In) {
		t.Fatalf("view after substitution %+v", after.View)
	}
	last := after.Events[len(after.Events)-1]
	if last.Kind != matches.EventSubstitution || last.Minute != 45 || last.Player != sub.In || last.Other != sub.Out {
		t.Fatalf("substitution event %+v", last)
	}
	got := resolveNow(t, w)
	for i, m := range got.Matches {
		if m.Fixture != fixture && !reflect.DeepEqual(m, want.Matches[i]) {
			t.Fatalf("fixture %d changed", m.Fixture)
		}
	}
	params := medical.DefaultParams()
	for _, id := range []ids.PlayerID{sub.Out, sub.In} {
		wantC := max(before[id]-params.Drain(45, stamina(w, id)), int(params.MinCondition))
		if c := conditions(w)[id]; c != wantC {
			t.Fatalf("player %d condition %d, want %d after 45 minutes", id, c, wantC)
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMentalityChangeInTheSecondHalf(t *testing.T) {
	w, fixture := liveReady(t)
	playTo(t, w, fixture, 45)
	l := playTo(t, w, fixture, 60)
	l = decide(t, w, fixture, matches.MatchCommand{Kind: matches.CommandSetMentality, Side: l.Side, Mentality: matches.Attacking})
	if l.View.Mentality[l.Side.Index()] != matches.Attacking {
		t.Fatalf("mentality %v", l.View.Mentality)
	}
	last := l.Events[len(l.Events)-1]
	if last.Kind != matches.EventMentalityChange || last.Minute != 60 {
		t.Fatalf("event %+v", last)
	}
	resolveNow(t, w)
}

func TestLiveCommandRejections(t *testing.T) {
	w, fixture := liveReady(t)
	ready, _ := w.Pending()
	var other ids.FixtureID
	for _, id := range ready.Rounds[0].Fixtures {
		if id != fixture {
			other = id
		}
	}
	ht := playTo(t, w, fixture, 45)
	opp := ht.Side.Opponent()
	oppTeam := ht.Teams[opp.Index()]
	before := w.Snapshot()

	plays := map[string]struct {
		cmd  PlayMatch
		want error
	}{
		"zero ID":         {PlayMatch{ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: 60}, ErrInvalidCommand},
		"stale revision":  {PlayMatch{ID: 99, ExpectedRevision: w.Revision() - 1, Fixture: fixture, ToMinute: 60}, ErrStaleRevision},
		"other fixture":   {PlayMatch{ID: 99, ExpectedRevision: w.Revision(), Fixture: other, ToMinute: 60}, ErrNotUserFixture},
		"back in time":    {PlayMatch{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: 30}, ErrInvalidCommand},
		"same minute":     {PlayMatch{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: 45}, ErrInvalidCommand},
		"after full time": {PlayMatch{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: 91}, ErrInvalidCommand},
	}
	for name, c := range plays {
		if _, err := w.PlayMatch(c.cmd); !errors.Is(err, c.want) {
			t.Errorf("play %s: err = %v, want %v", name, err, c.want)
		}
	}
	sub := forwardSub(ht)
	decisions := map[string]struct {
		cmd  matches.MatchCommand
		fix  ids.FixtureID
		want error
	}{
		"opponent side":   {matches.MatchCommand{Kind: matches.CommandSubstitute, Side: opp, Out: oppTeam.Starters[10].Player, In: oppTeam.Bench[1].Player}, fixture, ErrMatchDecision},
		"player not on":   {matches.MatchCommand{Kind: matches.CommandSubstitute, Side: ht.Side, Out: sub.In, In: sub.Out}, fixture, ErrMatchDecision},
		"same mentality":  {matches.MatchCommand{Kind: matches.CommandSetMentality, Side: ht.Side, Mentality: ht.View.Mentality[ht.Side.Index()]}, fixture, ErrMatchDecision},
		"no live match":   {sub, other, ErrNoLiveMatch},
		"unknown kind":    {matches.MatchCommand{Kind: 9, Side: ht.Side}, fixture, ErrMatchDecision},
		"goalkeeper swap": {matches.MatchCommand{Kind: matches.CommandSubstitute, Side: ht.Side, Out: ht.Teams[ht.Side.Index()].Starters[0].Player, In: sub.In}, fixture, ErrMatchDecision},
	}
	for name, c := range decisions {
		_, err := w.MatchDecision(MatchDecision{ID: 99, ExpectedRevision: w.Revision(), Fixture: c.fix, Command: c.cmd})
		if !errors.Is(err, c.want) {
			t.Errorf("decision %s: err = %v, want %v", name, err, c.want)
		}
	}
	lineup, _ := w.SuggestLineup(fixture)
	if _, err := w.SubmitLineup(SubmitLineup{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture, Lineup: lineup}); !errors.Is(err, ErrMatchInProgress) {
		t.Errorf("lineup during a live match: err = %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("rejected live commands changed the world")
	}

	// Substitutions run out; after full time nothing can be decided.
	team := ht.Teams[ht.Side.Index()]
	used := 0
	for i, p := range team.Bench {
		if p.Role == matches.Goalkeeper || used == int(ht.Rules.MaxSubstitutions) {
			continue
		}
		decide(t, w, fixture, matches.MatchCommand{Kind: matches.CommandSubstitute, Side: ht.Side, Out: team.Starters[10-i%4].Player, In: p.Player})
		used++
	}
	last := team.Bench[len(team.Bench)-1]
	if _, err := w.MatchDecision(MatchDecision{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture,
		Command: matches.MatchCommand{Kind: matches.CommandSubstitute, Side: ht.Side, Out: team.Starters[5].Player, In: last.Player}}); !errors.Is(err, ErrMatchDecision) {
		t.Errorf("substitution beyond the limit: err = %v", err)
	}
	playTo(t, w, fixture, 90)
	if _, err := w.MatchDecision(MatchDecision{ID: 99, ExpectedRevision: w.Revision(), Fixture: fixture,
		Command: matches.MatchCommand{Kind: matches.CommandSetMentality, Side: ht.Side, Mentality: matches.Defensive}}); !errors.Is(err, ErrMatchDecision) {
		t.Errorf("decision after full time: err = %v", err)
	}
	resolveNow(t, w)

	fresh := userWorld(t, 42, userClub)
	if _, err := fresh.PlayMatch(PlayMatch{ID: 1, Fixture: 3, ToMinute: 45}); err == nil {
		t.Error("played a match before its kickoff")
	}
}

func TestLiveCommandRetries(t *testing.T) {
	w, fixture := liveReady(t)
	cmd := PlayMatch{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, ToMinute: 45}
	first, err := w.PlayMatch(cmd)
	if err != nil {
		t.Fatal(err)
	}
	dec := MatchDecision{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Command: forwardSub(first.Live)}
	decided, err := w.MatchDecision(dec)
	if err != nil {
		t.Fatal(err)
	}
	before := w.Snapshot()
	if again, err := w.PlayMatch(cmd); err != nil || !reflect.DeepEqual(again, first) {
		t.Fatalf("play retry: %v", err)
	}
	if again, err := w.MatchDecision(dec); err != nil || !reflect.DeepEqual(again, decided) {
		t.Fatalf("decision retry: %v", err)
	}
	cmd.ToMinute = 50
	if _, err := w.PlayMatch(cmd); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("reused play ID: err = %v", err)
	}
	if _, err := w.MatchDecision(MatchDecision{ID: cmd.ID, ExpectedRevision: dec.ExpectedRevision, Fixture: fixture, Command: dec.Command}); !errors.Is(err, ErrCommandIDReused) {
		t.Fatalf("play ID used for a decision: err = %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("retries changed the world")
	}
}

// A save at half time, after a substitution, continues identically.
// shifted is the manager's current roles with the first midfielder moved
// to the attack: a 4-3-3 from a 4-4-2.
func shifted(l LiveMatch) matches.MatchCommand {
	roles := l.View.Roles[l.Side.Index()]
	i := slices.Index(roles[:], matches.Midfielder)
	roles[i] = matches.Forward
	return matches.MatchCommand{Kind: matches.CommandSetRoles, Side: l.Side, Roles: roles}
}

// A formation change is a decision like the others: it shows in the view and
// the events, survives a save and a load, replays to the same match, and
// the match resolves with it in its report.
func TestFormationChangeInTheSecondHalf(t *testing.T) {
	w, fixture := liveReady(t)
	playTo(t, w, fixture, 45)
	l := playTo(t, w, fixture, 60)
	side := l.Side.Index()
	start := l.View.Roles[side]
	cmd := shifted(l)
	l = decide(t, w, fixture, cmd)
	if l.View.Roles[side] != cmd.Roles || l.View.Roles[side] == start || l.View.Roles[1-side] == cmd.Roles {
		t.Fatalf("roles %v after the change, started %v", l.View.Roles, start)
	}
	last := l.Events[len(l.Events)-1]
	if last.Kind != matches.EventFormationChange || last.Minute != 60 || last.Side != l.Side || last.Roles != cmd.Roles {
		t.Fatalf("event %+v", last)
	}
	if l.View.SubstitutionsUsed[side] != 0 {
		t.Fatalf("a formation change used a substitution: %v", l.View.SubstitutionsUsed)
	}

	loaded := roundTrip(t, w)
	if a, ok := loaded.LiveMatch(); !ok || !reflect.DeepEqual(a, l) {
		t.Fatal("the live match differs after load")
	}
	playTo(t, w, fixture, 75)
	playTo(t, loaded, fixture, 75)
	x, y := resolveNow(t, w), resolveNow(t, loaded)
	if !reflect.DeepEqual(x, y) || !reflect.DeepEqual(loaded.Snapshot(), w.Snapshot()) {
		t.Fatal("finishing after a load differs")
	}
	for _, m := range x.Matches {
		if m.Fixture == fixture && !slices.ContainsFunc(m.Events, func(e matches.MatchEvent) bool { return e.Kind == matches.EventFormationChange }) {
			t.Fatal("the report lost the formation change")
		}
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFormationRejections(t *testing.T) {
	w, fixture := liveReady(t)
	l := playTo(t, w, fixture, 30)
	side := l.Side.Index()
	revision := w.Revision()
	try := func(name string, c matches.MatchCommand) {
		t.Helper()
		_, err := w.MatchDecision(MatchDecision{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Fixture: fixture, Command: c})
		if !errors.Is(err, ErrMatchDecision) {
			t.Errorf("%s: err = %v, want %v", name, err, ErrMatchDecision)
		}
	}
	good := shifted(l)
	other := good
	other.Side = l.Side.Opponent()
	try("the other side", other)
	same := good
	same.Roles = l.View.Roles[side]
	try("unchanged roles", same)
	noKeeper := good
	noKeeper.Roles[slices.Index(l.View.Roles[side][:], matches.Goalkeeper)] = matches.Defender
	try("no goalkeeper", noKeeper)
	badRole := good
	badRole.Roles[3] = 9
	try("unknown role", badRole)
	if w.Revision() != revision {
		t.Fatal("a rejected formation changed the world")
	}
}

func TestSaveDuringALiveMatch(t *testing.T) {
	w, fixture := liveReady(t)
	ht := playTo(t, w, fixture, 45)
	decide(t, w, fixture, forwardSub(ht))
	loaded := roundTrip(t, w)
	a, _ := w.LiveMatch()
	b, ok := loaded.LiveMatch()
	if !ok || !reflect.DeepEqual(a, b) {
		t.Fatal("the live match differs after load")
	}
	playTo(t, w, fixture, 75)
	playTo(t, loaded, fixture, 75)
	x, y := resolveNow(t, w), resolveNow(t, loaded)
	if !reflect.DeepEqual(x, y) || !reflect.DeepEqual(loaded.Snapshot(), w.Snapshot()) {
		t.Fatal("finishing after a load differs")
	}
}

// A submitted lineup is the one that kicks off live.
func TestLiveMatchUsesTheSubmittedLineup(t *testing.T) {
	w, fixture := liveReady(t)
	l := changedLineup(t, w, fixture)
	submit(t, w, fixture, l)
	live := playTo(t, w, fixture, 10)
	team := live.Teams[live.Side.Index()]
	for i, s := range l.Starters {
		if team.Starters[i].Player != s.Player || team.Starters[i].Role != s.Role {
			t.Fatalf("slot %d is %+v, submitted %+v", i, team.Starters[i], s)
		}
	}
	if live.View.Mentality[live.Side.Index()] != l.Tactics.Mentality {
		t.Fatal("live match ignores the submitted mentality")
	}
}

func TestRestoreRejectsInvalidLiveMatch(t *testing.T) {
	build := func() WorldSnapshot {
		w, fixture := liveReady(t)
		ht := playTo(t, w, fixture, 45)
		decide(t, w, fixture, forwardSub(ht))
		playTo(t, w, fixture, 60)
		return w.Snapshot()
	}
	cases := map[string]func(*WorldSnapshot){
		"other fixture":          func(s *WorldSnapshot) { s.Live.Fixture = 1 },
		"no stops":               func(s *WorldSnapshot) { s.Live.Stops = nil },
		"minutes not increasing": func(s *WorldSnapshot) { s.Live.Stops[1].Minute = 45 },
		"stop skips half time":   func(s *WorldSnapshot) { s.Live.Stops = s.Live.Stops[1:]; s.Live.Stops[0].Commands = nil },
		"decision for opponent": func(s *WorldSnapshot) {
			c := &s.Live.Stops[0].Commands[0]
			c.Side = c.Side.Opponent()
		},
		"invalid substitution": func(s *WorldSnapshot) {
			c := &s.Live.Stops[0].Commands[0]
			c.Out, c.In = c.In, c.Out
		},
		"play record for other fixture": func(s *WorldSnapshot) { s.PlayCommands[0].Request.Fixture = 1 },
		"decision record future":        func(s *WorldSnapshot) { s.DecisionCommands[0].Result.Revision = s.Revision + 1 },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// A completed report keeps every match event in order, the live match's
// substitution included, through the command result, the MatchReport query
// and a save, and never hands out its stored slices.
func TestMatchReportKeepsEvents(t *testing.T) {
	w, fixture := liveReady(t)
	ht := playTo(t, w, fixture, 45)
	sub := forwardSub(ht)
	decide(t, w, fixture, sub)
	final := playTo(t, w, fixture, 90)
	res := resolveNow(t, w)

	var report MatchReport
	for _, m := range res.Matches {
		if m.Fixture == fixture {
			report = m
		} else if len(m.Events) < 2 { // at least the two period ends
			t.Fatalf("fixture %d report has events %v", m.Fixture, m.Events)
		}
	}
	if !reflect.DeepEqual(report.Events, final.Events) {
		t.Fatalf("report events %v, the live match had %v", report.Events, final.Events)
	}
	if !slices.ContainsFunc(report.Events, func(e matches.MatchEvent) bool {
		return e.Kind == matches.EventSubstitution && e.Minute == 45 && e.Player == sub.In && e.Other == sub.Out
	}) {
		t.Fatalf("no substitution in %v", report.Events)
	}

	got, ok := w.MatchReport(fixture)
	if !ok || !reflect.DeepEqual(got, report) {
		t.Fatal("MatchReport differs from the command result")
	}
	got.Events[0].Minute = 99
	res.Matches[0].Events[0].Minute = 99
	if again, _ := w.MatchReport(fixture); !reflect.DeepEqual(again, report) {
		t.Fatal("a returned report aliases stored events")
	}
	if again, _ := w.MatchReport(res.Matches[0].Fixture); again.Events[0].Minute == 99 {
		t.Fatal("the command result aliases stored events")
	}

	loaded := roundTrip(t, w)
	if after, ok := loaded.MatchReport(fixture); !ok || !reflect.DeepEqual(after, report) {
		t.Fatal("the report's events differ after load")
	}
}

func TestRestoreRejectsInvalidReportEvents(t *testing.T) {
	build := func() WorldSnapshot {
		w, _ := liveReady(t)
		return w.Snapshot()
	}
	// The first report of the latest resolve with a goal.
	report := func(s *WorldSnapshot) *MatchReport {
		ms := s.ResolveCommands[len(s.ResolveCommands)-1].Result.Matches
		for i := range ms {
			if len(ms[i].Goals) > 0 {
				return &ms[i]
			}
		}
		t.Fatal("no goals in the batch")
		return nil
	}
	goal := func(m *MatchReport) *matches.MatchEvent {
		i := slices.IndexFunc(m.Events, func(e matches.MatchEvent) bool { return e.Kind == matches.EventGoal })
		return &m.Events[i]
	}
	cases := map[string]func(*WorldSnapshot){
		"no events":        func(s *WorldSnapshot) { report(s).Events = nil },
		"sequence gap":     func(s *WorldSnapshot) { m := report(s); m.Events[len(m.Events)-1].Seq++ },
		"unknown kind":     func(s *WorldSnapshot) { report(s).Events[0].Kind = 9 },
		"goal by another":  func(s *WorldSnapshot) { m := report(s); goal(m).Player++ },
		"goal for nobody":  func(s *WorldSnapshot) { m := report(s); goal(m).Side = 0 },
		"after full time":  func(s *WorldSnapshot) { m := report(s); m.Events[len(m.Events)-1].Minute = 91 },
		"no half time end": func(s *WorldSnapshot) { m := report(s); m.Events = slices.DeleteFunc(m.Events, isHalfTime) },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatal(err)
	}
}

// A report's formation change must name a possible formation.
func TestRestoreRejectsInvalidFormationEvents(t *testing.T) {
	w, fixture := liveReady(t)
	playTo(t, w, fixture, 45)
	l := playTo(t, w, fixture, 60)
	decide(t, w, fixture, shifted(l))
	resolveNow(t, w)
	build := func() WorldSnapshot { return w.Snapshot() }
	change := func(s *WorldSnapshot) *matches.MatchEvent {
		ms := s.ResolveCommands[len(s.ResolveCommands)-1].Result.Matches
		for i := range ms {
			for j := range ms[i].Events {
				if ms[i].Events[j].Kind == matches.EventFormationChange {
					return &ms[i].Events[j]
				}
			}
		}
		t.Fatal("no formation change in the batch")
		return nil
	}
	cases := map[string]func(*matches.MatchEvent){
		"unknown role": func(e *matches.MatchEvent) { e.Roles[3] = 9 },
		"no keeper":    func(e *matches.MatchEvent) { e.Roles[0] = matches.Defender },
		"two keepers":  func(e *matches.MatchEvent) { e.Roles[5] = matches.Goalkeeper },
	}
	for name, mutate := range cases {
		snap := build()
		mutate(change(&snap))
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatal(err)
	}
}

func isHalfTime(e matches.MatchEvent) bool {
	return e.Kind == matches.EventPeriodEnd && e.Period == matches.FirstHalf
}

// The live match's frames cover the segment since the previous stop, are
// thinned to the asked interval, and reading them changes nothing.
func TestLiveFrames(t *testing.T) {
	w := engineWorld(t, 42, "tick")
	playBatches(t, w, 1)
	fixture := readyBatch(t, w).UserFixtures[0]
	if _, err := w.LiveFrames(0); !errors.Is(err, ErrNoLiveMatch) {
		t.Fatalf("frames before the match: %v", err)
	}
	const minute = 60_000
	segment := func(every uint32, from, to uint32) []matches.Frame {
		t.Helper()
		before := w.Snapshot()
		frames, err := w.LiveFrames(every)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(w.Snapshot(), before) {
			t.Fatal("reading frames changed the world")
		}
		if len(frames) == 0 || frames[0].Millis <= from || frames[len(frames)-1].Millis != to {
			t.Fatalf("%d frames, want them in (%d, %d] ms ending at %d", len(frames), from, to, to)
		}
		for i := 1; i < len(frames); i++ {
			if every > 0 && frames[i].Millis/every == frames[i-1].Millis/every {
				t.Fatalf("two frames in one %d ms interval: %d, %d", every, frames[i-1].Millis, frames[i].Millis)
			}
		}
		return frames
	}
	playTo(t, w, fixture, 30)
	all := segment(0, 0, 30*minute)
	if thin := segment(1000, 0, 30*minute); len(thin) != 30*60+1 || len(all) <= len(thin) {
		t.Fatalf("%d frames, %d at one a second over 30 minutes (one per second and full time)", len(all), len(thin))
	}
	ht := playTo(t, w, fixture, 60) // stops at half time
	segment(1000, 30*minute, 45*minute)
	decide(t, w, fixture, forwardSub(ht))
	segment(1000, 30*minute, 45*minute) // a decision plays no football

	// The frames never change the match: it resolves as if never shown.
	plain := engineWorld(t, 42, "tick")
	playBatches(t, plain, 1)
	readyBatch(t, plain)
	playTo(t, plain, fixture, 60)
	decide(t, plain, fixture, forwardSub(ht))
	if got, want := resolveNow(t, w), resolveNow(t, plain); !reflect.DeepEqual(comparable(got), comparable(want)) {
		t.Fatal("reading frames changed the result")
	}
}

func TestLiveFramesNeedAnEngineWithFrames(t *testing.T) {
	w, fixture := liveReady(t)
	playTo(t, w, fixture, 30)
	if _, err := w.LiveFrames(0); !errors.Is(err, matches.ErrUnsupported) {
		t.Fatalf("frames from the simple engine: %v", err)
	}
}

// A career on an engine with statistics keeps them in every report and
// shows them live; the live match's final statistics are the report's. A
// career on an engine without them marks them unavailable.
func TestMatchReportStats(t *testing.T) {
	w := engineWorld(t, 42, "tick")
	playBatches(t, w, 1)
	fixture := readyBatch(t, w).UserFixtures[0]
	half := playTo(t, w, fixture, 45)
	if err := half.View.Stats.Validate(); err != nil || !half.View.Stats.Available || half.View.Stats.Teams[0].Passes == 0 {
		t.Fatalf("live stats at half time %+v: %v", half.View.Stats, err)
	}
	final := playTo(t, w, fixture, 90)
	for _, m := range resolveNow(t, w).Matches {
		if err := m.Stats.Validate(); err != nil || !m.Stats.Available {
			t.Fatalf("fixture %d stats %+v: %v", m.Fixture, m.Stats, err)
		}
		if m.Fixture == fixture && m.Stats != final.View.Stats {
			t.Fatalf("report stats %+v, the live match ended with %+v", m.Stats, final.View.Stats)
		}
	}
	report, _ := w.MatchReport(fixture)
	if after, ok := roundTrip(t, w).MatchReport(fixture); !ok || after.Stats != report.Stats {
		t.Fatal("the report's statistics differ after load")
	}

	plain, fixture := liveReady(t)
	if live := playTo(t, plain, fixture, 90); live.View.Stats != (matches.MatchStats{}) {
		t.Fatalf("simple engine live stats %+v", live.View.Stats)
	}
	for _, m := range resolveNow(t, plain).Matches {
		if m.Stats != (matches.MatchStats{}) {
			t.Fatalf("simple engine report stats %+v", m.Stats)
		}
	}
}

func TestRestoreRejectsInvalidReportStats(t *testing.T) {
	build := func(engine string) WorldSnapshot {
		w := engineWorld(t, 42, engine)
		playBatches(t, w, 1)
		return w.Snapshot()
	}
	stats := func(s *WorldSnapshot) *matches.MatchStats {
		return &s.ResolveCommands[len(s.ResolveCommands)-1].Result.Matches[0].Stats
	}
	cases := map[string]struct {
		engine string
		mutate func(*WorldSnapshot)
	}{
		"inconsistent":         {"tick", func(s *WorldSnapshot) { st := stats(s); st.Teams[0].ShotsOnTarget = st.Teams[0].Shots + 1 }},
		"missing":              {"tick", func(s *WorldSnapshot) { *stats(s) = matches.MatchStats{} }},
		"from a simple career": {"simple", func(s *WorldSnapshot) { *stats(s) = matches.MatchStats{Available: true} }},
	}
	for name, c := range cases {
		snap := build(c.engine)
		c.mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: Restore = %v", name, err)
		}
	}
	if _, err := Restore(build("tick")); err != nil {
		t.Fatal(err)
	}

	// A recorded live step's view.
	w := engineWorld(t, 42, "tick")
	playBatches(t, w, 1)
	playTo(t, w, readyBatch(t, w).UserFixtures[0], 30)
	snap := w.Snapshot()
	snap.PlayCommands[len(snap.PlayCommands)-1].Result.Live.View.Stats.Teams[1].Saves = 99
	if _, err := Restore(snap); !errors.Is(err, ErrInvalidSave) {
		t.Errorf("live view with inconsistent stats: Restore = %v", err)
	}
}
