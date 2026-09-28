package app

import (
	"cmp"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
)

// playerYearTask returns the queued player-year task.
func playerYearTask(t *testing.T, w *World) sim.Task {
	t.Helper()
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskPlayerYear {
			return task
		}
	}
	t.Fatal("no player-year task queued")
	return sim.Task{}
}

// eventsAt returns the events of one kind that occurred at an instant.
func eventsAt(w *World, at sim.GameInstant, kind events.Kind) []events.Event {
	var out []events.Event
	for _, e := range w.Events() {
		if e.OccurredAt == at && e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// The first player year, on the eve of the contract-year end, applies the
// rules exactly: every active player retires when players.Retires says so
// and otherwise develops by players.Develop; each club replaces its own
// retirees with youth players at the same positions, with new IDs in order;
// squads keep their shape; the events describe every change.
func TestPlayerYear(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	at := playerYearTask(t, w).DueAt
	end := w.ContractYearEnd()
	if at != end-day {
		t.Fatalf("player year due %s, want the eve of %s", w.calendar.Format(at), w.calendar.Format(end))
	}
	year, _ := w.yearOf(at)
	before := w.players.Snapshot()
	jobs := map[ids.PlayerID]employment.Assignment{}
	for _, a := range w.employment.Assignments() {
		jobs[a.Player] = a
	}
	shape := map[ids.ClubID]map[players.Position]int{}
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		shape[c.ID] = w.squadCounts(team)
	}
	last := w.registry.LastPlayer()
	mustContinue(t, w, at)

	var retired []events.PlayerRetired // expected, in ID order
	for _, p := range before {
		age, _ := w.age(p.Player, at)
		_, employed := jobs[p.Player]
		now, _ := w.players.Profile(p.Player)
		if players.Retires(w.seed, p.Player, age, year, employed) {
			if !now.Retired || now.Attributes != p.Attributes {
				t.Fatalf("player %d (%d) should have retired unchanged: %+v", p.Player, age, now)
			}
			if _, ok := w.employment.Assignment(p.Player); ok {
				t.Fatalf("retired player %d is still employed", p.Player)
			}
			if _, ok := w.medical.Condition(p.Player); ok {
				t.Fatalf("retired player %d has a condition record", p.Player)
			}
			a := jobs[p.Player]
			retired = append(retired, events.PlayerRetired{Player: p.Player, Club: a.Club, Team: a.Team, Age: uint8(age)})
			continue
		}
		if now.Retired || now.Attributes != players.Develop(w.seed, p, age, year) {
			t.Fatalf("player %d (%d) developed to %+v, want %v", p.Player, age, now, players.Develop(w.seed, p, age, year))
		}
	}
	if len(retired) == 0 {
		t.Fatal("nobody retired in the first player year")
	}
	var got []events.PlayerRetired
	for _, e := range eventsAt(w, at, events.KindPlayerRetired) {
		got = append(got, *e.PlayerRetired)
	}
	if !reflect.DeepEqual(got, retired) {
		t.Fatalf("retirement events %+v, want %+v", got, retired)
	}
	if dev := eventsAt(w, at, events.KindPlayersDeveloped); len(dev) != 1 || len(dev[0].PlayersDeveloped.Players) != len(before)-len(retired) {
		t.Fatalf("development events %+v", dev)
	}

	// Youth: club by club, each retiree's replacement at the same position.
	next := last
	joined := eventsAt(w, at, events.KindYouthJoined)
	if len(joined) != len(retired) {
		t.Fatalf("%d youth players for %d retirements (all employed)", len(joined), len(retired))
	}
	i := 0
	for _, c := range w.registry.Clubs() {
		for _, r := range retired {
			if r.Club != c.ID {
				continue
			}
			next++
			old, _ := w.players.Profile(r.Player)
			youth, _ := w.players.Profile(next)
			a, _ := w.employment.Assignment(next)
			age, _ := w.age(next, at)
			cond, _ := w.medical.Condition(next)
			wantContract := employment.Contract{Expires: mustAddYears(t, w, end, w.defs.Youth.ContractYears), WeeklyWage: w.defs.Economy.Demand(youth.Overall())}
			switch {
			case youth.Position != old.Position || youth.Retired:
				t.Fatalf("youth %d %+v does not replace %+v", next, youth, old)
			case a.Club != c.ID || a.Contract != wantContract:
				t.Fatalf("youth %d employed %+v, want club %d on %+v", next, a, c.ID, wantContract)
			case age < w.defs.Youth.Ages[0] || age > w.defs.Youth.Ages[1] || cond != medical.MaxCondition:
				t.Fatalf("youth %d is %d with condition %d", next, age, cond)
			case *joined[i].YouthJoined != (events.YouthJoined{Player: next, Club: c.ID, Team: a.Team, Expires: a.Contract.Expires, WeeklyWage: a.Contract.WeeklyWage}):
				t.Fatalf("youth event %+v", joined[i].YouthJoined)
			}
			i++
		}
	}
	if w.registry.LastPlayer() != next || len(w.registry.Players()) != int(next) {
		t.Fatalf("allocator at %d with %d players, want %d", w.registry.LastPlayer(), len(w.registry.Players()), next)
	}
	for _, c := range w.registry.Clubs() {
		team, _ := w.registry.SeniorTeam(c.ID)
		if got := w.squadCounts(team); !reflect.DeepEqual(got, shape[c.ID]) {
			t.Fatalf("club %d squad %v, was %v", c.ID, got, shape[c.ID])
		}
	}
	if task := playerYearTask(t, w); task.DueAt != mustAddYears(t, w, end, 1)-day {
		t.Fatalf("next player year due %s", w.calendar.Format(task.DueAt))
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The yearly step is keyed by (seed, player, year): continuing in one call,
// in uneven chunks or through saves gives the same world over several
// years, with the same new IDs.
func TestPlayerYearsDoNotDependOnChunkingOrSaves(t *testing.T) {
	one, many, saved := newWorld(t, 42), newWorld(t, 42), newWorld(t, 42)
	for range 3 {
		for _, w := range []*World{one, many, saved} {
			playSeason(t, w)
		}
		target := one.ContractYearEnd() + 3*day
		mustContinue(t, one, target)
		for step := many.Now() + 13*day + 7; step < target; step += 29*day + 5 {
			mustContinue(t, many, step)
		}
		mustContinue(t, many, target)
		mustContinue(t, saved, playerYearTask(t, saved).DueAt-1)
		saved = roundTrip(t, saved)
		mustContinue(t, saved, playerYearTask(t, saved).DueAt)
		saved = roundTrip(t, saved)
		mustContinue(t, saved, target)
	}
	a := one.Snapshot()
	for _, w := range []*World{many, saved} {
		b := w.Snapshot()
		if !reflect.DeepEqual(a.Registry, b.Registry) || !reflect.DeepEqual(a.Players, b.Players) ||
			!reflect.DeepEqual(a.Employment, b.Employment) || !reflect.DeepEqual(a.Medical, b.Medical) {
			t.Fatal("chunking or saving changed the player years")
		}
	}
	if a.Registry.LastPlayer <= 160 {
		t.Fatal("no youth players joined in three years")
	}
}

// A retired player never plays, signs or recovers again: they are no free
// agent, the manager cannot sign them, and every later match is played by
// active players only (medical records exist only for them, and exposure of
// anyone else fails the resolution).
func TestRetiredPlayersNeverReturn(t *testing.T) {
	w := userWorld(t, 42, userClub)
	var retired []ids.PlayerID
	freeRetired := 0
	for range 3 {
		playSeason(t, w)
		// The manager renews no one, so free agents build up; old ones
		// retire at the player year. AI clubs sign free agents to fill the
		// vacancies their transfers leave, so an AI club also lets its
		// oldest player go on the eve (a vacancy youth replaces him).
		at := playerYearTask(t, w).DueAt
		mustContinue(t, w, at-1)
		oldest := SquadPlayer{}
		for _, c := range w.registry.Clubs() {
			squad, _ := w.Squad(c.ID)
			counts := w.squadCounts(w.competitionsTeam(c.ID))
			for _, p := range squad {
				if c.ID != userClub && counts[p.Position] > w.defs.Quota(p.Position).Min && p.Age > oldest.Age {
					oldest = p
				}
			}
		}
		if age, _ := w.age(oldest.Player, at); age < players.FreeAgentRetirementAge {
			t.Fatalf("the oldest player is %d", age)
		}
		plan, err := w.employment.Plan(employment.Changes{Departures: []ids.PlayerID{oldest.Player}})
		if err != nil {
			t.Fatal(err)
		}
		w.applyEmployment(plan)
		mustContinue(t, w, at)
		for _, p := range w.FreeAgents() {
			if age, _ := w.age(p.Player, at); age >= players.FreeAgentRetirementAge {
				t.Fatalf("free agent %d is %d after the player year", p.Player, age)
			}
		}
		for _, e := range eventsAt(w, at, events.KindPlayerRetired) {
			if e.PlayerRetired.Club == 0 {
				freeRetired++
			}
		}
		mustContinue(t, w, w.ContractYearEnd())
		for _, p := range w.players.Snapshot() {
			if p.Retired && !slices.Contains(retired, p.Player) {
				retired = append(retired, p.Player)
			}
		}
		for _, id := range retired {
			p, _ := w.players.Profile(id)
			_, employed := w.employment.Assignment(id)
			_, condition := w.medical.Condition(id)
			if !p.Retired || employed || condition || slices.ContainsFunc(w.FreeAgents(), func(f SquadPlayer) bool { return f.Player == id }) {
				t.Fatalf("retired player %d returned (retired %t, employed %t, condition %t)", id, p.Retired, employed, condition)
			}
		}
	}
	if len(retired) < 10 || freeRetired == 0 {
		t.Fatalf("only %d retirements in three years, %d of free agents", len(retired), freeRetired)
	}
	id := retired[0]
	offer := ContractOffer{Years: 1, WeeklyWage: w.defs.Economy.Demand(50)}
	if _, err := w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: id, Offer: offer}); !errors.Is(err, ErrNotFreeAgent) {
		t.Fatalf("signing retired player %d: %v", id, err)
	}
	if _, err := w.SuggestContract(id); !errors.Is(err, ErrNotFreeAgent) {
		t.Fatalf("suggesting terms for retired player %d: %v", id, err)
	}
	goals := 0
	for _, r := range playSeason(t, w) {
		for _, m := range r.Matches {
			for _, g := range m.Goals {
				goals++
				if slices.Contains(retired, g.Scorer) {
					t.Fatalf("retired player %d scored in fixture %d", g.Scorer, m.Fixture)
				}
			}
		}
	}
	if goals == 0 {
		t.Fatal("a season without goals checks nothing")
	}
}

// Over fifteen years every squad stays legal (Validate checks the roster
// limits), the AI keeps its squads full, the active population stays
// balanced and young, and the average ability stays near the generated
// world's. With a manager who never renews anyone, the safety net keeps the
// user's squad legal too.
func TestSquadsStayLegalAndBalancedOverTheYears(t *testing.T) {
	for _, club := range []ids.ClubID{0, userClub} {
		w := userWorld(t, 7, club)
		start := averageOverall(w)
		for year := 1; year <= 15; year++ {
			playSeason(t, w)
			mustContinue(t, w, w.ContractYearEnd())
			if err := w.Validate(); err != nil {
				t.Fatalf("club %d year %d: %v", club, year, err)
			}
			assertAISquadsFull(t, w)
			s := w.Summary()
			if s.Players < 300 || s.Players > 320+s.Clubs+s.FreeAgents {
				t.Fatalf("club %d year %d: %d active players, %d free agents", club, year, s.Players, s.FreeAgents)
			}
			if avg := averageOverall(w); avg < start-6 || avg > start+6 {
				t.Fatalf("club %d year %d: average overall %d, started at %d", club, year, avg, start)
			}
		}
		if s := w.Summary(); s.Retired < 100 {
			t.Fatalf("club %d: only %d retirements in fifteen years", club, s.Retired)
		}
		ages := 0
		for _, a := range w.employment.Assignments() {
			ages += w.squadPlayer(a.Player).Age
		}
		if avg := ages / len(w.employment.Assignments()); avg < 22 || avg > 28 {
			t.Fatalf("club %d: average age %d", club, avg)
		}
	}
}

// averageOverall is the rounded mean overall of every employed player.
func averageOverall(w *World) int {
	total, n := 0, 0
	for _, a := range w.employment.Assignments() {
		p, _ := w.players.Profile(a.Player)
		total += p.Overall()
		n++
	}
	return (2*total + n) / (2 * n)
}

// A player year that fails (here: a youth player cannot be generated)
// changes nothing, keeps its task queued and succeeds when retried, exactly
// as if it had never failed.
func TestFailedPlayerYearChangesNothing(t *testing.T) {
	w, clean := newWorld(t, 42), newWorld(t, 42)
	playSeason(t, w)
	playSeason(t, clean)
	at := playerYearTask(t, w).DueAt
	mustContinue(t, w, at-1)
	before := snapshot(w)
	modules := func() []any {
		return []any{w.registry.Snapshot(), w.players.Snapshot(), w.employment.Snapshot(), w.medical.Snapshot()}
	}
	was := modules()
	profiles := w.defs.Profiles
	w.defs.Profiles = nil // no position can produce a youth player
	if _, err := w.Continue(at); err == nil {
		t.Fatal("the broken player year succeeded")
	}
	after := snapshot(w)
	before.Revision, after.Revision = 0, 0 // a failed cohort may still publish the clock
	if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(modules(), was) {
		t.Fatal("the failed player year changed the world")
	}
	w.defs.Profiles = profiles
	mustContinue(t, w, at)
	mustContinue(t, clean, at)
	if !reflect.DeepEqual(w.players.Snapshot(), clean.players.Snapshot()) || !reflect.DeepEqual(w.registry.Snapshot(), clean.registry.Snapshot()) {
		t.Fatal("the retried player year differs from one that never failed")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The manager hears about their own players: retirements, youth arrivals
// and one development summary. Squad ages advance a year each year.
func TestManagerSeesThePlayerYear(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playSeason(t, w)
	squad, _ := w.Squad(userClub)
	ages := map[ids.PlayerID]int{}
	for _, p := range squad {
		age, _ := w.age(p.Player, w.Now())
		if p.Age != age || p.Age < w.defs.Ages[0] {
			t.Fatalf("player %d shown as %d, is %d", p.Player, p.Age, age)
		}
		ages[p.Player] = p.Age
	}
	at := playerYearTask(t, w).DueAt
	mustContinue(t, w, at)
	for id, age := range ages {
		if p := w.squadPlayer(id); p.Age != age+1 && p.Age != age {
			t.Fatalf("player %d aged from %d to %d in seven months", id, age, p.Age)
		}
	}
	kinds := map[inbox.Kind]int{}
	for _, m := range w.Inbox() {
		if m.At != at {
			continue
		}
		kinds[m.Kind]++
		if m.Kind == inbox.KindDeveloped && m.Improved+m.Declined == 0 {
			t.Fatalf("development summary %+v", m)
		}
		if (m.Kind == inbox.KindRetired || m.Kind == inbox.KindYouthJoined) && m.PlayerName == "" {
			t.Fatalf("message %+v has no player name", m)
		}
	}
	team := mustUserTeam(t, w)
	mine := 0
	for _, e := range eventsAt(w, at, events.KindPlayerRetired) {
		if e.PlayerRetired.Team == team {
			mine++
		}
	}
	if kinds[inbox.KindDeveloped] != 1 || kinds[inbox.KindRetired] != mine || kinds[inbox.KindYouthJoined] != mine {
		t.Fatalf("inbox %v for %d retirements", kinds, mine)
	}
}

func TestRestoreRejectsInvalidLifecycle(t *testing.T) {
	var retiredID, activeID ids.PlayerID
	build := func() WorldSnapshot {
		w := newWorld(t, 42)
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
		for _, p := range w.players.Snapshot() {
			if p.Retired && retiredID == 0 {
				retiredID = p.Player
			}
		}
		activeID = w.employment.Assignments()[0].Player
		return w.Snapshot()
	}
	player := func(s *WorldSnapshot, id ids.PlayerID) *registry.Player {
		return &s.Registry.Players[slices.IndexFunc(s.Registry.Players, func(p registry.Player) bool { return p.ID == id })]
	}
	w := newWorld(t, 42)
	cases := map[string]func(*WorldSnapshot){
		"retired player employed": func(s *WorldSnapshot) {
			// Replace an employed player of the same position, so squad
			// sizes stay legal.
			pos := s.Players[slices.IndexFunc(s.Players, func(p players.Profile) bool { return p.Player == retiredID })].Position
			for i, a := range s.Employment {
				if s.Players[slices.IndexFunc(s.Players, func(p players.Profile) bool { return p.Player == a.Player })].Position == pos {
					s.Employment[i].Player = retiredID
					break
				}
			}
			slices.SortFunc(s.Employment, func(x, y employment.Assignment) int { return cmp.Compare(x.Player, y.Player) })
		},
		"retired player recovering": func(s *WorldSnapshot) {
			s.Medical = append(s.Medical, medical.Record{Player: retiredID, Condition: medical.MaxCondition})
			slices.SortFunc(s.Medical, func(x, y medical.Record) int { return cmp.Compare(x.Player, y.Player) })
		},
		"active player without condition": func(s *WorldSnapshot) {
			s.Medical = slices.DeleteFunc(s.Medical, func(r medical.Record) bool { return r.Player == activeID })
		},
		"active player too old": func(s *WorldSnapshot) {
			player(s, activeID).Born = -mustAddYears(t, w, 0, players.RetirementAge+2)
		},
		"player born in the future": func(s *WorldSnapshot) { player(s, activeID).Born = s.Scheduler.Now + day },
		"allocator below the IDs":   func(s *WorldSnapshot) { s.Registry.LastPlayer-- },
		"player-year task missing": func(s *WorldSnapshot) {
			s.Scheduler.Tasks = slices.DeleteFunc(s.Scheduler.Tasks, func(t sim.Task) bool { return t.Kind == taskPlayerYear })
		},
		"player-year task off the eve": func(s *WorldSnapshot) {
			for i := range s.Scheduler.Tasks {
				if s.Scheduler.Tasks[i].Kind == taskPlayerYear {
					s.Scheduler.Tasks[i].DueAt += day
				}
			}
		},
		"retirement event for an active player": func(s *WorldSnapshot) {
			for i := range s.Events {
				if s.Events[i].Kind == events.KindPlayerRetired {
					s.Events[i].PlayerRetired.Player = activeID
					return
				}
			}
		},
		"youth event for another club": func(s *WorldSnapshot) {
			for i := range s.Events {
				if e := s.Events[i].YouthJoined; e != nil {
					e.Team = e.Team%8 + 1
					return
				}
			}
		},
	}
	for name, mutate := range cases {
		snap := build()
		mutate(&snap)
		if w, err := Restore(snap); err == nil || w != nil || !errors.Is(err, ErrInvalidSave) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*Versions){
		"development": func(v *Versions) { v.Development++ },
		"youth":       func(v *Versions) { v.Youth++ },
	} {
		snap := build()
		mutate(&snap.Versions)
		if _, err := Restore(snap); !errors.Is(err, ErrIncompatibleSave) {
			t.Errorf("%s version: err = %v", name, err)
		}
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("the unmodified save: %v", err)
	}
}
