package app

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

// Stop at the final kickoff, before automatic resolution, to inspect both
// the match commit and the subsequent season-end commit independently.
func beforeCupFinal(t *testing.T) *World {
	t.Helper()
	w := newWorld(t, 42)
	playSeason(t, w)
	playToCupRound(t, w, 3)
	rounds := w.competitions.Rounds(cup1)
	at := rounds[len(rounds)-1].Kickoff
	stopped, err := w.scheduler.RunUntil(at, w.handleCohort)
	if err != nil || !stopped {
		t.Fatalf("final kickoff: stopped %v, err %v", stopped, err)
	}
	w.revision++
	w.publish()
	return w
}

func resolveCupFinal(t *testing.T, w *World) {
	t.Helper()
	if _, err := w.resolveAutomatically(); err != nil {
		t.Fatal(err)
	}
	w.revision++
	w.publish()
	if !w.competitions.SeasonCompleted(cup1) {
		t.Fatal("cup final was not resolved")
	}
}

func cupPrizeEntries(w *World) []finance.Entry {
	return slices.DeleteFunc(w.finance.All(), func(e finance.Entry) bool { return e.Kind != finance.KindPrize })
}

// The final's result commits first. All awards commit with the edition's
// end, once per entrant, including both finalists sharing the same fixture.
func TestCupPrizesPostOnceAtEditionEnd(t *testing.T) {
	w := beforeCupFinal(t)
	loaded := roundTrip(t, w)
	for _, world := range []*World{w, loaded} {
		resolveCupFinal(t, world)
	}
	if !reflect.DeepEqual(w.Snapshot(), loaded.Snapshot()) {
		t.Fatal("reload before the final diverged")
	}
	if len(cupPrizeEntries(w)) != 0 {
		t.Fatal("prizes paid before season end")
	}
	loaded = roundTrip(t, w) // official final, season-end task still pending
	var cause events.Cause
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskSeasonEnd && w.seasonEnds[task.PayloadID] == cup1 {
			cause = taskCause(task.ID)
		}
	}
	before := w.finance.Snapshot()
	for _, world := range []*World{w, loaded} {
		mustContinue(t, world, world.Now())
	}
	if !reflect.DeepEqual(w.Snapshot(), loaded.Snapshot()) {
		t.Fatal("reload after the final diverged")
	}
	entries := cupPrizeEntries(w)
	exits, _ := w.competitions.Exits(cup1)
	if len(entries) != len(exits) || len(entries) != 8 {
		t.Fatalf("%d prizes for %d entrants", len(entries), len(exits))
	}
	stages := map[ids.ClubID]int{}
	for _, exit := range exits {
		stages[w.teamLabel(exit.Team).Club] = exit.Stage
	}
	var total money.Money
	for _, e := range entries {
		if e.Amount != w.cups[0].Prizes[stages[e.Club]] || e.At != w.Now() {
			t.Fatalf("incorrect award %+v", e)
		}
		f, _ := w.competitions.Fixture(e.Fixture)
		if f.Season != cup1 || (w.teamLabel(f.Home).Club != e.Club && w.teamLabel(f.Away).Club != e.Club) {
			t.Fatalf("incorrect prize fixture %+v", e)
		}
		exit := exits[slices.IndexFunc(exits, func(x competitions.Exit) bool { return w.teamLabel(x.Team).Club == e.Club })]
		if f.Round != exit.Round {
			t.Fatalf("prize names round %d, exit in %d", f.Round, exit.Round)
		}
		if balance(t, w, e.Club)-balanceFromEntries(before.Entries, e.Club) != e.Amount {
			t.Fatal("prize balance differs")
		}
		total += e.Amount
	}
	if total != 310_000_000 {
		t.Fatalf("total awards %s", total)
	}
	evs := w.Events()
	ended := slices.IndexFunc(evs, func(e events.Event) bool {
		return e.Kind == events.KindSeasonEnded && e.SeasonEnded.Competition == cup1.Competition && e.SeasonEnded.Season == 1
	})
	if ended < 0 || ended+1 >= len(evs) {
		t.Fatal("missing cup end and prize events")
	}
	award := evs[ended+1]
	if award.Kind != events.KindLedgerPosted || award.Cause != cause || award.Revision != evs[ended].Revision || len(award.LedgerPosted.Entries) != 8 {
		t.Fatalf("prize event %+v", award)
	}
	snap := w.Snapshot()
	mustContinue(t, w, w.Now())
	if !reflect.DeepEqual(w.Snapshot(), snap) {
		t.Fatal("repeated Continue changed the completed edition")
	}
	assertLedgersConsistent(t, w)
	loaded = roundTrip(t, w)
	for _, world := range []*World{w, loaded} {
		mustContinue(t, world, world.Now()+day)
	}
	if !reflect.DeepEqual(w.Snapshot(), loaded.Snapshot()) {
		t.Fatal("reload after prizes diverged")
	}
}

func balanceFromEntries(entries []finance.Entry, club ids.ClubID) money.Money {
	var sum money.Money
	for _, e := range entries {
		if e.Club == club {
			sum += e.Amount
		}
	}
	return sum
}

// Overflow in an award rejects the entire cohort and leaves its task,
// finance IDs, season-end payload, events and revision untouched.
func TestCupPrizeFailureIsAtomicAndRetryable(t *testing.T) {
	w := beforeCupFinal(t)
	resolveCupFinal(t, w)
	clean := roundTrip(t, w)
	original := slices.Clone(w.cups[0].Prizes)
	w.cups[0].Prizes = []money.Money{math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64}
	before := w.Snapshot()
	if _, err := w.Continue(w.Now()); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("failed prize cohort changed the world")
	}
	w.cups[0].Prizes = original
	for _, world := range []*World{w, clean} {
		mustContinue(t, world, world.Now())
	}
	if !reflect.DeepEqual(w.Snapshot(), clean.Snapshot()) {
		t.Fatal("retry differs from clean season end")
	}
	assertLedgersConsistent(t, w)
}

func TestCupPrizeTablesCanOmitStages(t *testing.T) {
	base := beforeCupFinal(t)
	resolveCupFinal(t, base)
	for _, tc := range []struct {
		name  string
		table []money.Money
		count int
	}{
		{"empty", nil, 0},
		{"winner only", []money.Money{100}, 1},
		{"zero stages", []money.Money{100, 0, 0, 0}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := base.Snapshot()
			w, err := Restore(snap)
			if err != nil {
				t.Fatal(err)
			}
			w.cups[0].Prizes = slices.Clone(tc.table)
			mustContinue(t, w, w.Now())
			if len(cupPrizeEntries(w)) != tc.count {
				t.Fatalf("prizes %+v", cupPrizeEntries(w))
			}
			assertLedgersConsistent(t, w)
			roundTrip(t, w)
		})
	}
}

func TestRestoreRejectsInvalidCupPrizes(t *testing.T) {
	w := beforeCupFinal(t)
	resolveCupFinal(t, w)
	pending := roundTrip(t, w)
	mustContinue(t, w, w.Now())
	find := func(s *WorldSnapshot) int {
		for i, e := range s.Finance.Entries {
			if e.Kind == finance.KindPrize {
				return i
			}
		}
		t.Fatal("no prize")
		return -1
	}
	for name, mutate := range map[string]func(*WorldSnapshot){
		"wrong amount": func(s *WorldSnapshot) { s.Finance.Entries[find(s)].Amount++ },
		"missing":      func(s *WorldSnapshot) { i := find(s); s.Finance.Entries = slices.Delete(s.Finance.Entries, i, i+1) },
		"duplicate": func(s *WorldSnapshot) {
			e := s.Finance.Entries[find(s)]
			s.Finance.LastEntry++
			e.ID = s.Finance.LastEntry
			s.Finance.Entries = append(s.Finance.Entries, e)
		},
		"wrong club":    func(s *WorldSnapshot) { e := &s.Finance.Entries[find(s)]; e.Club = 32 },
		"wrong fixture": func(s *WorldSnapshot) { s.Finance.Entries[find(s)].Fixture = 1 },
		"wrong time": func(s *WorldSnapshot) {
			// Keep valid ledger ordering and avoid a future-entry rejection.
			s.Scheduler.Now++
			for i := range s.Finance.Entries {
				if s.Finance.Entries[i].Kind == finance.KindPrize {
					s.Finance.Entries[i].At++
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			snap := w.Snapshot()
			mutate(&snap)
			if got, err := Restore(snap); got != nil || !errors.Is(err, ErrInvalidSave) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// Isolate the finance check from journal checks: exact amount/reference
	// validation must hold even when journal trimming has dropped the event.
	for _, e := range cupPrizeEntries(w) {
		plan, err := pending.finance.Plan(pending.Now(), []finance.Posting{{Club: e.Club, Kind: finance.KindPrize, Amount: e.Amount, Fixture: e.Fixture}})
		if err != nil {
			t.Fatal(err)
		}
		pending.applyFinance(plan)
		break
	}
	if len(pending.validateFinance()) == 0 {
		t.Fatal("prize paid while season-end task pending accepted")
	}
	if got, err := Restore(pending.Snapshot()); got != nil || !errors.Is(err, ErrInvalidSave) {
		t.Fatalf("early prize restore: %v", err)
	}
	snap := w.Snapshot()
	snap.Finance.Entries[find(&snap)].Amount++
	edited, err := finance.New(snap.Finance)
	if err != nil {
		t.Fatal(err)
	}
	w.finance = edited
	if len(w.validateFinance()) == 0 {
		t.Fatal("edited prize accepted independently of journal")
	}
}

// Two disjoint cups end in one cohort. An overflowing award in the second
// edition must not pay the first, and each ledger event cites its own task.
func TestCupPrizeCohortAndRepeatedEditions(t *testing.T) {
	defs := content.Default()
	cups := content.DefaultCups()
	second := cups[0].Clone()
	second.ID, second.Name = 6, "Lower Divisions Cup"
	second.Qualifiers = []content.Qualifier{{League: 4, Places: 4}, {League: 5, Places: 4}}
	cups = append(cups, second)
	generated, err := worldgen.Generate(defs, 42)
	if err != nil {
		t.Fatal(err)
	}
	w, err := load(defs, content.DefaultLeagues(), cups, content.DefaultPromotions(), DefaultEpoch(), generated)
	if err != nil {
		t.Fatal(err)
	}
	playSeason(t, w)
	playToCupRound(t, w, 3)
	rounds := w.competitions.Rounds(cup1)
	stopped, err := w.scheduler.RunUntil(rounds[len(rounds)-1].Kickoff, w.handleCohort)
	if err != nil || !stopped {
		t.Fatalf("final kickoffs: %v, %v", stopped, err)
	}
	w.revision++
	w.publish()
	resolveCupFinal(t, w)
	clean := roundTrip(t, w)
	original := slices.Clone(w.cups[1].Prizes)
	w.cups[1].Prizes = []money.Money{math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64}
	before := w.Snapshot()
	if _, err := w.Continue(w.Now()); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("later cup failure changed the cohort")
	}
	w.cups[1].Prizes = original
	causes := map[ids.CompetitionID]events.Cause{}
	for _, task := range w.scheduler.Pending() {
		if task.Kind == taskSeasonEnd && task.DueAt == w.Now() {
			causes[w.seasonEnds[task.PayloadID].Competition] = taskCause(task.ID)
		}
	}
	for _, world := range []*World{w, clean} {
		mustContinue(t, world, world.Now())
	}
	if !reflect.DeepEqual(w.Snapshot(), clean.Snapshot()) {
		t.Fatal("cohort retry differs from clean run")
	}
	if len(cupPrizeEntries(w)) != 16 || len(causes) != 2 {
		t.Fatalf("prizes %d, ending cups %d", len(cupPrizeEntries(w)), len(causes))
	}
	ledgerEvents := 0
	for _, ev := range w.Events() {
		if ev.Kind != events.KindLedgerPosted || finance.Kind(ev.LedgerPosted.Entries[0].Kind) != finance.KindPrize {
			continue
		}
		f, _ := w.competitions.Fixture(ev.LedgerPosted.Entries[0].Fixture)
		if ev.Cause != causes[f.Season.Competition] || len(ev.LedgerPosted.Entries) != 8 {
			t.Fatalf("wrong cup prize event %+v", ev)
		}
		for _, e := range ev.LedgerPosted.Entries {
			fixture, _ := w.competitions.Fixture(e.Fixture)
			if fixture.Season != f.Season {
				t.Fatal("one event mixed awards from different editions")
			}
		}
		ledgerEvents++
	}
	if ledgerEvents != 2 {
		t.Fatalf("%d prize events", ledgerEvents)
	}
	// Finish year 2, then years 3 and 4; drawing a new cup does not pay it.
	playSeason(t, w)
	for year := 3; year <= 4; year++ {
		playSeason(t, w)
		assertLedgersConsistent(t, w)
	}
	entries := cupPrizeEntries(w)
	var total money.Money
	for _, e := range entries {
		total += e.Amount
	}
	if len(entries) != 48 || total != money.Units(18_600_000) {
		t.Fatalf("four-year prizes %d, total %s", len(entries), total)
	}
	for _, c := range w.cups {
		ref := competitions.SeasonRef{Competition: c.ID, Season: 4}
		if _, ok := w.competitions.Entrants(ref); !ok {
			t.Fatalf("next edition %s not drawn", ref)
		}
		for _, e := range entries {
			f, _ := w.competitions.Fixture(e.Fixture)
			if f.Season == ref {
				t.Fatalf("unplayed edition %s was paid", ref)
			}
		}
	}
	roundTrip(t, w)
}
