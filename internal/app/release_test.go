package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

func releaseCmd(w *World, player ids.PlayerID) ReleasePlayer {
	return ReleasePlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player}
}

// wageRunsLeft counts the weekly wage runs after now and before expires,
// one week at a time.
func wageRunsLeft(now, expires sim.GameInstant) int64 {
	var n int64
	for at := (now/week + 1) * week; at < expires; at += week {
		n++
	}
	return n
}

// longestContract returns the user club's player at pos with the latest
// contract end (ties: lowest ID).
func longestContract(t *testing.T, w *World, pos players.Position) SquadPlayer {
	t.Helper()
	squad, _ := w.Squad(w.userClub)
	var at []SquadPlayer
	for _, p := range squad {
		if p.Position == pos {
			at = append(at, p)
		}
	}
	if len(at) == 0 {
		t.Fatalf("no %s in the squad", pos)
	}
	return slices.MaxFunc(at, func(a, b SquadPlayer) int { return int(a.Contract.Expires - b.Contract.Expires) })
}

// Releasing a player pays the rest of his contract (his wage for every
// weekly run left on it) as one payoff entry, makes him a free agent and
// tells the manager. Retries return the recorded result, the release
// survives a save, and the next wage run no longer pays him.
func TestReleasePaysOffTheContract(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	mustContinue(t, w, 3*week+2*day) // mid-week, in the window, before the season
	p := longestContract(t, w, players.Forward)
	runs := wageRunsLeft(w.Now(), p.Contract.Expires)
	want := p.Contract.WeeklyWage * money.Money(runs)
	if runs < 50 || p.Payoff != want {
		t.Fatalf("player %d: %d runs left, shown payoff %s, want %s", p.Player, runs, p.Payoff, want)
	}
	before, total, bill := balance(t, w, userClub3), totalBalance(w), mustWageBill(t, w)

	cmd := releaseCmd(w, p.Player)
	res, err := w.ReleasePlayer(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if res.Player != p.Player || res.Compensation != want || res.Revision != w.Revision() {
		t.Fatalf("result %+v, want compensation %s", res, want)
	}
	if _, employed := w.employment.Assignment(p.Player); employed || !slices.ContainsFunc(w.FreeAgents(), func(f SquadPlayer) bool { return f.Player == p.Player }) {
		t.Fatal("the released player is not a free agent")
	}
	if b := balance(t, w, userClub3); b != before-want || totalBalance(w) != total-want {
		t.Fatalf("balance %s, want %s", b, before-want)
	}
	entries := w.finance.Entries(userClub3)
	if e := entries[len(entries)-1]; e.Kind != finance.KindPayoff || e.Amount != -want || e.Player != p.Player || e.At != w.Now() {
		t.Fatalf("payoff entry %+v", e)
	}
	evs := w.Events()
	released, ledger := evs[len(evs)-2], evs[len(evs)-1]
	if released.Kind != events.KindPlayerReleased || *released.PlayerReleased != (events.PlayerReleased{Player: p.Player, Club: userClub3, Team: mustUserTeam(t, w), Compensation: want}) ||
		released.Cause != commandCause(cmd.ID) || ledger.Kind != events.KindLedgerPosted || ledger.LedgerPosted.Entries[0].Player != p.Player {
		t.Fatalf("events %+v, %+v", released, ledger)
	}
	if m := lastMessage(w); m.Kind != inbox.KindReleased || m.Player != p.Player || m.Compensation != want || m.PlayerName != p.Name {
		t.Fatalf("inbox %+v", m)
	}
	assertLedgersConsistent(t, w)

	after := snapshot(w)
	if again, err := w.ReleasePlayer(cmd); err != nil || again != res || !reflect.DeepEqual(snapshot(w), after) {
		t.Fatalf("retry: %+v, %v", again, err)
	}
	restored, err := Restore(w.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := restored.ReleasePlayer(cmd); err != nil || again != res {
		t.Fatalf("retry after a save: %+v, %v", again, err)
	}

	mustContinue(t, w, 4*week)
	entries = w.finance.Entries(userClub3)
	if e := entries[len(entries)-1]; e.Kind != finance.KindWages || e.Amount != -(bill-p.Contract.WeeklyWage) {
		t.Fatalf("next wages %+v, bill was %s", e, bill)
	}
}

func mustWageBill(t *testing.T, w *World) money.Money {
	t.Helper()
	bill, err := w.employment.WageBill(w.userClub)
	if err != nil {
		t.Fatal(err)
	}
	return bill
}

// A contract with no wage run left costs nothing to end: no ledger entry.
func TestReleaseCostCountsWageRuns(t *testing.T) {
	c := func(expires sim.GameInstant) (money.Money, error) {
		return releaseCost(employment.Contract{Expires: expires, WeeklyWage: 100}, 3*week)
	}
	for _, tc := range []struct {
		expires sim.GameInstant
		want    money.Money
	}{
		{3 * week, 0},           // expired: nothing due
		{4 * week, 0},           // the run at the expiry no longer pays
		{4*week + 1, 100},       // one run
		{10*week + day, 7_00},   // runs at weeks 4..10
		{3*week + day, 0},       // before the next run
		{5 * week, 100},         // run at week 4 only
		{5*week + 1, 2 * 100},   // weeks 4 and 5
		{52 * week, 48 * 100},   // weeks 4..51
		{53 * week, 49 * 100},   // weeks 4..52
		{2 * week, 0},           // in the past
		{3*week + 1, 0},         // no week boundary before it
		{4*week + day, 1 * 100}, // week 4
	} {
		if got, err := c(tc.expires); err != nil || got != tc.want {
			t.Errorf("expires %d: %s (%v), want %s", tc.expires, got, err, tc.want)
		}
	}
	if _, err := releaseCost(employment.Contract{Expires: 100 * week, WeeklyWage: money.Money(1) << 60}, 0); err == nil {
		t.Error("an overflowing payoff was accepted")
	}
}

// Every refused release is reported and changes nothing.
func TestReleaseRejectionsChangeNothing(t *testing.T) {
	type setup struct {
		w   *World
		cmd ReleasePlayer
	}
	fresh := func(t *testing.T) setup {
		w := userWorld(t, 42, userClub3)
		return setup{w, releaseCmd(w, longestContract(t, w, players.Forward).Player)}
	}
	cases := []struct {
		name string
		want error
		edit func(*testing.T, *setup)
	}{
		{"zero ID", ErrInvalidCommand, func(_ *testing.T, s *setup) { s.cmd.ID = 0 }},
		{"stale revision", ErrStaleRevision, func(_ *testing.T, s *setup) { s.cmd.ExpectedRevision++ }},
		{"no user club", ErrNoUserClub, func(t *testing.T, s *setup) { s.w = newWorld(t, 42) }},
		{"another club's player", ErrNotUserPlayer, func(t *testing.T, s *setup) {
			s.cmd.Player = bestAt(t, s.w, players.Forward, userClub3).Player
		}},
		{"unknown player", ErrNotUserPlayer, func(_ *testing.T, s *setup) { s.cmd.Player = 99_999 }},
		{"free agent", ErrNotUserPlayer, func(t *testing.T, s *setup) {
			s.cmd.Player = release(t, s.w, userClub3, players.Forward)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"squad at its minimum", ErrSquadMinimum, func(t *testing.T, s *setup) {
			release(t, s.w, userClub3, players.Goalkeeper) // 3 -> 2, the minimum
			s.cmd.Player = longestContract(t, s.w, players.Goalkeeper).Player
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
		{"payoff above the balance", ErrCannotAfford, func(t *testing.T, s *setup) {
			drain := balance(t, s.w, userClub3) - 1
			plan, err := s.w.finance.Plan(s.w.Now(), []finance.Posting{{Club: userClub3, Kind: finance.KindWages, Amount: -drain}})
			if err != nil {
				t.Fatal(err)
			}
			s.w.applyFinance(plan)
		}},
		{"a round awaits results", ErrSquadsLocked, func(t *testing.T, s *setup) {
			readyBatch(t, s.w)
			s.cmd.ExpectedRevision = s.w.Revision()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fresh(t)
			c.edit(t, &s)
			before := s.w.Snapshot()
			if _, err := s.w.ReleasePlayer(s.cmd); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !reflect.DeepEqual(s.w.Snapshot(), before) {
				t.Fatal("the refused release changed the world")
			}
		})
	}
}

// A release collapses every open bid for the player in the same commit.
func TestReleaseCollapsesOpenBids(t *testing.T) {
	w, o := bidScenario(t)(t)
	res, err := w.ReleasePlayer(releaseCmd(w, o.Player))
	if err != nil {
		t.Fatal(err)
	}
	if got := offerOf(t, w, o.ID); got.Status != transfers.StatusCollapsed || got.ClosedAt != w.Now() {
		t.Fatalf("offer after the release %+v", got)
	}
	evs := w.Events()
	var kinds []events.Kind
	for _, e := range evs {
		if e.Revision == uint64(res.Revision) {
			kinds = append(kinds, e.Kind)
		}
	}
	if want := []events.Kind{events.KindPlayerReleased, events.KindOfferClosed, events.KindLedgerPosted}; !slices.Equal(kinds, want) {
		t.Fatalf("events %v, want %v", kinds, want)
	}
	assertLedgersConsistent(t, w)
}

// The manager may keep more than the roster count at a position, up to the
// squad limit in all: a full squad of 20 buys a sixth midfielder, then fills
// up to the limit, and the next bid is refused. The sellers, left short at
// the first window (no free agents yet), get youth players at the player
// year, and every AI squad is full after the contract year.
func TestManagerSquadGrowsToTheLimit(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	sellers := map[ids.ClubID]map[players.Position]int{}
	for bids := 0; len(w.employment.Squad(mustUserTeam(t, w))) < w.defs.SquadLimit; bids++ {
		if bids == 2*w.defs.SquadLimit {
			t.Fatal("too many bids failed")
		}
		target := cheapestForSale(t, w)
		res := bidFor(t, w, target.Player, target.Value)
		mustContinue(t, w, w.Now()+day)
		if clubOf(w, target.Player) != userClub3 {
			continue // an earlier bid, or another sale by his club that day, came first
		}
		seller := offerOf(t, w, res.Offer).Seller
		if sellers[seller] == nil {
			sellers[seller] = map[players.Position]int{}
		}
		sellers[seller][target.Position]++
		if err := w.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	counts := w.squadCounts(mustUserTeam(t, w))
	if squadSize(counts) != w.defs.SquadLimit || !slices.ContainsFunc(w.defs.Roster, func(q content.Quota) bool { return counts[q.Position] > q.Count }) {
		t.Fatalf("user squad %v", counts)
	}
	target := cheapestForSale(t, w)
	if _, err := w.MakeTransferOffer(MakeTransferOffer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: target.Player,
		Fee: target.Value, Offer: suggest(t, w, target.Player)}); !errors.Is(err, ErrSquadFull) {
		t.Fatalf("bid at the limit: %v", err)
	}

	playSeason(t, w)
	short := func() int {
		n := 0
		for club := range sellers {
			team, _ := w.registry.SeniorTeam(club)
			c := w.squadCounts(team)
			for _, q := range w.defs.Roster {
				n += max(0, q.Count-c[q.Position])
			}
		}
		return n
	}
	if short() == 0 {
		t.Fatal("the sellers refilled their squads without free agents")
	}
	at := playerYearTask(t, w).DueAt
	mustContinue(t, w, at)
	if n := short(); n != 0 {
		t.Fatalf("%d vacancies left after the player year", n)
	}
	youth := 0
	for _, e := range eventsAt(w, at, events.KindYouthJoined) {
		if sellers[e.YouthJoined.Club] != nil {
			youth++
		}
	}
	if youth == 0 {
		t.Fatal("no youth joined the sellers")
	}
	mustContinue(t, w, w.ContractYearEnd())
	playUntil(t, w, func(w *World) sim.GameInstant { return w.TransferWindow().Closes })
	assertAISquadsFull(t, w)
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// cheapestForSale returns the cheapest player the user club may bid for:
// at another club above its minimum at his position, and biddable (ties:
// lowest ID).
func cheapestForSale(t *testing.T, w *World) SquadPlayer {
	t.Helper()
	var cands []SquadPlayer
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub {
			continue
		}
		team, _ := w.registry.SeniorTeam(c.ID)
		counts := w.squadCounts(team)
		squad, _ := w.Squad(c.ID)
		for _, p := range squad {
			if counts[p.Position] > w.defs.Quota(p.Position).Min && biddable(w, p.Player) {
				cands = append(cands, p)
			}
		}
	}
	if len(cands) == 0 {
		t.Fatal("nobody for sale")
	}
	return slices.MinFunc(cands, func(a, b SquadPlayer) int { return int(a.Value - b.Value) })
}

// assertAISquadsFull checks that every AI squad holds at least its roster
// count at each position, and at most one player more in all: the one an
// upgrade replaced, listed until another club buys him (see hasRoom).
func assertAISquadsFull(t *testing.T, w *World) {
	t.Helper()
	assertAISquadsFullBut(t, w, 0)
}

// assertAISquadsFullBut allows AI clubs to lack up to missing players in
// all, as when the manager signed free agents that clubs held back for
// vacancies (see holdBack).
func assertAISquadsFullBut(t *testing.T, w *World, missing int) {
	t.Helper()
	for _, c := range w.registry.Clubs() {
		if c.ID == w.userClub {
			continue
		}
		team, _ := w.registry.SeniorTeam(c.ID)
		counts := w.squadCounts(team)
		for _, q := range w.defs.Roster {
			if n := q.Count - counts[q.Position]; n > 0 {
				if missing -= n; missing < 0 {
					t.Fatalf("AI club %d has %d %s, want %d", c.ID, counts[q.Position], q.Position, q.Count)
				}
			}
		}
		if n := squadSize(counts); n > w.defs.SquadSize()+1 {
			t.Fatalf("AI club %d has %d players", c.ID, n)
		}
	}
}

// biddable reports whether the user club may still bid for a player in the
// window under way: he has not moved in it, and the club has not bid for
// him in it (a bid may collapse when a sale leaves his club at its minimum).
func biddable(w *World, player ids.PlayerID) bool {
	open, _, _ := w.transferWindow(w.Now())
	return !slices.ContainsFunc(w.transfers.Offers(), func(o transfers.Offer) bool {
		return o.Player == player && o.MadeAt >= open && (o.Buyer == w.userClub || o.Status == transfers.StatusCompleted)
	})
}

// A manager who hoards and churns players for thirty years (every summer
// releasing the two oldest players the rules allow and buying the cheapest
// players on sale up to the squad limit) never breaks the world: every AI
// squad is full after each contract year, every squad stays legal, the
// payoffs match the releases and transfer fees sum to zero.
func TestSquadsSurviveAHoardingManager(t *testing.T) {
	w := userWorld(t, 7, userClub3)
	releases, purchases := 0, 0
	// After each window closes, every AI squad is full and the world valid.
	checkWindow := func(year int) {
		t.Helper()
		playUntil(t, w, func(w *World) sim.GameInstant { return w.TransferWindow().Closes })
		if err := w.Validate(); err != nil {
			t.Fatalf("year %d: %v", year, err)
		}
		assertAISquadsFullBut(t, w, freeAgentReserve)
		var fees money.Money
		for _, e := range w.finance.All() {
			if e.Kind == finance.KindTransfer {
				fees += e.Amount
			}
		}
		if fees != 0 {
			t.Fatalf("year %d: transfer fees sum to %s", year, fees)
		}
	}
	for year := 1; year <= 30; year++ {
		for range 2 {
			if p, ok := oldestReleasable(t, w); ok && p.Payoff <= balance(t, w, userClub3) {
				if _, err := w.ReleasePlayer(releaseCmd(w, p.Player)); err != nil {
					t.Fatalf("year %d: release %d: %v", year, p.Player, err)
				}
				releases++
			}
		}
		for range w.defs.SquadLimit {
			if len(w.employment.Squad(mustUserTeam(t, w))) >= w.defs.SquadLimit || w.Now()+day >= w.TransferWindow().BidsClose {
				break
			}
			target := cheapestForSale(t, w)
			if target.Value > balance(t, w, userClub3)/2 {
				break
			}
			bidFor(t, w, target.Player, target.Value)
			next := w.Now() + day
			playUntil(t, w, func(*World) sim.GameInstant { return next }) // a season may kick off in the window
			if clubOf(w, target.Player) == userClub3 {
				purchases++
			}
		}
		if year > 1 {
			checkWindow(year)
		}
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
	}
	checkWindow(31)
	if releases < 20 || purchases < 30 {
		t.Fatalf("only %d releases and %d purchases in thirty years", releases, purchases)
	}
}

// oldestReleasable returns the user club's oldest player above the minimum
// at his position (ties: lowest ID).
func oldestReleasable(t *testing.T, w *World) (SquadPlayer, bool) {
	t.Helper()
	squad, _ := w.Squad(w.userClub)
	counts := w.squadCounts(mustUserTeam(t, w))
	var cands []SquadPlayer
	for _, p := range squad {
		if counts[p.Position] > w.defs.Quota(p.Position).Min {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		return SquadPlayer{}, false
	}
	return slices.MaxFunc(cands, func(a, b SquadPlayer) int { return a.Age - b.Age }), true
}

// Saves whose releases and payoffs disagree are rejected, also once the
// journal no longer holds the release's events.
func TestRestoreRejectsInvalidReleases(t *testing.T) {
	retention := journalRetention
	t.Cleanup(func() { journalRetention = retention })
	journalRetention = 5
	w := userWorld(t, 42, userClub3)
	mustContinue(t, w, 2*week+day)
	if _, err := w.ReleasePlayer(releaseCmd(w, longestContract(t, w, players.Forward).Player)); err != nil {
		t.Fatal(err)
	}
	recent := w.Snapshot()
	mustContinue(t, w, 6*week)
	trimmed := w.Snapshot()
	if slices.ContainsFunc(trimmed.Events, func(e events.Event) bool { return e.Kind == events.KindPlayerReleased }) {
		t.Fatal("the journal still holds the release")
	}
	payoff := func(s *WorldSnapshot) *finance.Entry {
		for i := range s.Finance.Entries {
			if s.Finance.Entries[i].Kind == finance.KindPayoff {
				return &s.Finance.Entries[i]
			}
		}
		t.Fatal("no payoff entry")
		return nil
	}
	check := func(base WorldSnapshot, cases map[string]func(*WorldSnapshot)) {
		for name, mutate := range cases {
			snap := base
			snap.Finance.Entries = slices.Clone(base.Finance.Entries)
			snap.ReleaseCommands = slices.Clone(base.ReleaseCommands)
			snap.Events = events.CloneAll(base.Events)
			mutate(&snap)
			if r, err := Restore(snap); err == nil || r != nil || !errors.Is(err, ErrInvalidSave) {
				t.Errorf("%s: err = %v", name, err)
			}
		}
		if _, err := Restore(base); err != nil {
			t.Fatalf("unmodified snapshot rejected: %v", err)
		}
	}
	check(trimmed, map[string]func(*WorldSnapshot){
		"payoff edited":       func(s *WorldSnapshot) { payoff(s).Amount-- },
		"payoff other player": func(s *WorldSnapshot) { payoff(s).Player++ },
		"payoff other club":   func(s *WorldSnapshot) { payoff(s).Club = 1 },
		"release record gone": func(s *WorldSnapshot) { s.ReleaseCommands = nil },
		"compensation edited": func(s *WorldSnapshot) { s.ReleaseCommands[0].Result.Compensation++ },
		"negative payoff":     func(s *WorldSnapshot) { s.ReleaseCommands[0].Result.Compensation = -1 },
		"result other player": func(s *WorldSnapshot) { s.ReleaseCommands[0].Result.Player++ },
		"result from later":   func(s *WorldSnapshot) { s.ReleaseCommands[0].Result.Revision = s.Revision + 1 },
	})
	check(recent, map[string]func(*WorldSnapshot){
		"release event edited": func(s *WorldSnapshot) { s.Events[len(s.Events)-2].PlayerReleased.Compensation++ },
		"ledger event player":  func(s *WorldSnapshot) { s.Events[len(s.Events)-1].LedgerPosted.Entries[0].Player++ },
	})
}
