package app

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/finance"
	"github.com/thewalpa/project-zimble/internal/inbox"
	"github.com/thewalpa/project-zimble/internal/players"
)

// finalYear returns a club's players whose contracts end at the next
// contract-year end.
func finalYear(t *testing.T, w *World, club ids.ClubID) []SquadPlayer {
	t.Helper()
	squad, _ := w.Squad(club)
	var out []SquadPlayer
	for _, p := range squad {
		if p.Contract.Expires == w.ContractYearEnd() {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		t.Fatalf("club %d has no player in the final contract year", club)
	}
	return out
}

func renew(t *testing.T, w *World, player ids.PlayerID, offer ContractOffer) ContractRenewed {
	t.Helper()
	res, err := w.RenewContract(RenewContract{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: player, Offer: offer})
	if err != nil {
		t.Fatalf("renew player %d: %v", player, err)
	}
	return res
}

func suggest(t *testing.T, w *World, player ids.PlayerID) ContractOffer {
	t.Helper()
	o, err := w.SuggestContract(player)
	if err != nil {
		t.Fatalf("suggest for player %d: %v", player, err)
	}
	return o
}

// squadAverage is the rounded mean overall of a club's senior squad.
func squadAverage(w *World, club ids.ClubID) int {
	squad, _ := w.Squad(club)
	total := 0
	for _, p := range squad {
		total += p.Overall
	}
	return (2*total + len(squad)) / (2 * len(squad))
}

// assertEveryPlayerOnce checks that the registry kept every earlier player
// and only added new IDs after them, and that every active player is either
// employed or a free agent, exactly once, and no retired player is either.
func assertEveryPlayerOnce(t *testing.T, w *World, registry []ids.PlayerID) {
	t.Helper()
	var current []ids.PlayerID
	for _, p := range w.registry.Players() {
		current = append(current, p.ID)
	}
	if len(current) < len(registry) || !slices.Equal(current[:len(registry)], registry) {
		t.Fatal("the registry removed or renumbered players")
	}
	for i, id := range current {
		if id != ids.PlayerID(i+1) {
			t.Fatalf("player IDs are not allocated in sequence: %d at %d", id, i)
		}
	}
	seen := map[ids.PlayerID]int{}
	for _, a := range w.employment.Assignments() {
		seen[a.Player]++
	}
	for _, p := range w.FreeAgents() {
		seen[p.Player]++
	}
	for _, id := range current {
		p, _ := w.players.Profile(id)
		if want := map[bool]int{true: 0, false: 1}[p.Retired]; seen[id] != want {
			t.Fatalf("player %d (retired %t) is employed or free %d times", id, p.Retired, seen[id])
		}
	}
}

// At the contract-year end, each ending contract is renewed by an AI club
// exactly when ai.Renew accepts the player, on the AI's terms; the others
// expire, and clubs refill their squads from the free agents.
func TestContractYearRenewsReleasesAndSigns(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	end := w.ContractYearEnd()
	mustContinue(t, w, end-day) // the player year
	year, _ := w.yearOf(end)
	var registry []ids.PlayerID
	for _, p := range w.registry.Players() {
		registry = append(registry, p.ID)
	}
	before := map[ids.PlayerID]employment.Assignment{}
	for _, a := range w.employment.Assignments() {
		before[a.Player] = a
	}
	averages := map[ids.ClubID]int{}
	for _, c := range w.registry.Clubs() {
		averages[c.ID] = squadAverage(w, c.ID)
	}
	mustContinue(t, w, end)

	eventsAt := map[events.Kind][]events.Event{}
	for _, e := range w.Events() {
		if e.OccurredAt == end && e.Kind >= events.KindContractRenewed {
			eventsAt[e.Kind] = append(eventsAt[e.Kind], e)
		}
	}
	renewed, left := 0, 0
	for id, old := range before {
		now, employed := w.employment.Assignment(id)
		if old.Contract.Expires != end {
			if !employed || now != old {
				t.Fatalf("player %d's running contract changed: %+v -> %+v", id, old, now)
			}
			continue
		}
		p, _ := w.players.Profile(id)
		age, _ := w.age(id, end)
		if ai.Renew(p.Overall(), averages[old.Club], age) {
			renewed++
			offer, _ := w.aiOffer(id, year)
			want, _ := w.addYears(end, offer.Years)
			if !employed || now.Club != old.Club || now.Team != old.Team || now.Contract != (employment.Contract{Expires: want, WeeklyWage: offer.WeeklyWage}) {
				t.Fatalf("player %d should be renewed on %+v, has %+v", id, offer, now)
			}
			continue
		}
		left++
	}
	if renewed == 0 || left == 0 {
		t.Fatalf("%d renewed, %d left: the policy made no real decisions", renewed, left)
	}
	if len(eventsAt[events.KindContractRenewed]) != renewed || len(eventsAt[events.KindContractExpired]) != left {
		t.Fatalf("%d renewal and %d expiry events for %d renewals and %d departures",
			len(eventsAt[events.KindContractRenewed]), len(eventsAt[events.KindContractExpired]), renewed, left)
	}
	for _, e := range eventsAt[events.KindContractExpired] {
		if p := e.ContractExpired; before[p.Player].Club != p.Club || before[p.Player].Contract.Expires != end {
			t.Fatalf("expiry event %+v for %+v", p, before[p.Player])
		}
	}
	for _, e := range eventsAt[events.KindPlayerSigned] {
		p := e.PlayerSigned
		a, _ := w.employment.Assignment(p.Player)
		offer, _ := w.aiOffer(p.Player, year)
		want, _ := w.addYears(end, offer.Years)
		if a.Club != p.Club || a.Contract.Expires != want || a.Contract.WeeklyWage != offer.WeeklyWage || p.Expires != want {
			t.Fatalf("signing %+v, assignment %+v, offer %+v", p, a, offer)
		}
	}
	// Without a user club, every AI club refills to the full roster except
	// for the vacancies of the best free agents held back for the window.
	short := 0
	for _, row := range w.Summary().ClubRows {
		short += w.defs.SquadSize() - row.Players
	}
	if got := len(w.FreeAgents()); short != got || got != freeAgentReserve || len(eventsAt[events.KindPlayerSigned])+got != left {
		t.Fatalf("%d vacancies, %d free agents, %d signings for %d departures (held back %d)",
			short, got, len(eventsAt[events.KindPlayerSigned]), left, freeAgentReserve)
	}
	assertEveryPlayerOnce(t, w, registry)
	if next := w.ContractYearEnd(); next != mustAddYears(t, w, end, 1) {
		t.Fatalf("next contract-year end %d", next)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func mustAddYears(t *testing.T, w *World, at sim.GameInstant, n int) sim.GameInstant {
	t.Helper()
	got, err := w.addYears(at, n)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Over several contract years player IDs are never reused or added, the
// same career saved before a contract year continues identically, and every
// season is played with legal lineups.
func TestContractYearsOverSeveralSeasons(t *testing.T) {
	w := userWorld(t, 42, userClub)
	var registry []ids.PlayerID
	for _, p := range w.registry.Players() {
		registry = append(registry, p.ID)
	}
	playSeason(t, w)
	saved := roundTrip(t, w)
	for _, x := range []*World{w, saved} {
		for range 2 {
			mustContinue(t, x, x.ContractYearEnd())
			playSeason(t, x) // played with the shrunken user squad
		}
	}
	if !reflect.DeepEqual(snapshot(w), snapshot(saved)) || !reflect.DeepEqual(w.employment.Assignments(), saved.employment.Assignments()) {
		t.Fatal("saving before the contract year changed the career")
	}
	assertEveryPlayerOnce(t, w, registry)
	moved := 0
	for _, e := range w.Events() {
		if e.Kind == events.KindPlayerSigned {
			moved++
		}
	}
	if moved == 0 || len(w.History()) != 4*4+3 { // four leagues' four seasons, three cup editions
		t.Fatalf("%d signings over %d seasons", moved, len(w.History()))
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The manager's renewals keep their players; unrenewed ones leave, and the
// safety net signs free agents only up to the roster minimum. The inbox
// reports each change.
func TestUserContractDecisions(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playSeason(t, w)
	mustContinue(t, w, w.ContractYearEnd()-day) // after the player year, as the CLI stops
	final := finalYear(t, w, userClub)
	if len(final) < 3 {
		t.Fatalf("only %d final-year players", len(final))
	}
	keep := []ids.PlayerID{final[0].Player, final[1].Player}
	for _, id := range keep {
		offer := suggest(t, w, id)
		offer.WeeklyWage += money.Units(100)
		res := renew(t, w, id, offer)
		if want := mustAddYears(t, w, w.ContractYearEnd(), offer.Years); res.Contract != (employment.Contract{Expires: want, WeeklyWage: offer.WeeklyWage}) {
			t.Fatalf("renewal result %+v", res)
		}
	}
	team := mustUserTeam(t, w)
	kept := w.squadCounts(team)
	for _, p := range final[2:] {
		kept[p.Position]--
	}
	end := w.ContractYearEnd()
	mustContinue(t, w, end)

	for _, id := range keep {
		if a, ok := w.employment.Assignment(id); !ok || a.Club != userClub || a.Contract.Expires <= end {
			t.Fatalf("renewed player %d: %+v", id, a)
		}
	}
	counts := w.squadCounts(team)
	for _, q := range w.defs.Roster {
		if want := max(kept[q.Position], q.Min); counts[q.Position] != want {
			t.Fatalf("%d %s after the contract year, want %d (kept %d, minimum %d)", counts[q.Position], q.Position, want, kept[q.Position], q.Min)
		}
	}
	msgs := map[inbox.Kind][]ids.PlayerID{}
	for _, m := range w.Inbox() {
		if m.Kind >= inbox.KindRenewed && m.Kind <= inbox.KindPlayerJoined && m.At >= end-day {
			msgs[m.Kind] = append(msgs[m.Kind], m.Player)
			if m.PlayerName == "" {
				t.Fatalf("message %+v has no player name", m)
			}
		}
	}
	var left []ids.PlayerID
	for _, p := range final[2:] {
		left = append(left, p.Player)
	}
	if !slices.Equal(msgs[inbox.KindRenewed], keep) || !slices.Equal(msgs[inbox.KindPlayerLeft], left) {
		t.Fatalf("inbox renewals %v, departures %v; want %v, %v", msgs[inbox.KindRenewed], msgs[inbox.KindPlayerLeft], keep, left)
	}
	joined := 0
	for _, q := range w.defs.Roster {
		joined += max(0, q.Min-kept[q.Position])
	}
	if len(msgs[inbox.KindPlayerJoined]) != joined || w.Summary().FreeAgents == 0 {
		t.Fatalf("%d joined messages, want %d; %d free agents", len(msgs[inbox.KindPlayerJoined]), joined, w.Summary().FreeAgents)
	}
	playSeason(t, w)
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A renewal changes the wage bill at once, so the next weekly run pays the
// new wage.
func TestRenewedContractIsPaidFromTheNextWeek(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playSeason(t, w)
	mustContinue(t, w, w.Now()+3*day)
	p := finalYear(t, w, userClub)[0]
	offer := suggest(t, w, p.Player)
	offer.WeeklyWage = w.defs.Economy.OfferCeiling(p.Overall)
	oldBill, _ := w.employment.WageBill(userClub)
	renew(t, w, p.Player, offer)
	newBill, _ := w.employment.WageBill(userClub)
	if newBill != oldBill-p.Contract.WeeklyWage+offer.WeeklyWage || newBill == oldBill {
		t.Fatalf("bill %s -> %s after renewing %s at %s", oldBill, newBill, p.Contract.WeeklyWage, offer.WeeklyWage)
	}
	paidBefore := len(w.finance.Entries(userClub))
	nextRun := (w.Now()/week + 1) * week
	mustContinue(t, w, nextRun)
	entries := w.finance.Entries(userClub)
	if last := entries[len(entries)-1]; len(entries) != paidBefore+1 || last.Kind != finance.KindWages || last.At != nextRun || last.Amount != -newBill {
		t.Fatalf("next wage entry %+v, want %s at %d", last, -newBill, nextRun)
	}
	assertLedgersConsistent(t, w)
}

// The manager's defaults are the AI's decisions: a club-3 player the AI
// renews in a world without a user club gets exactly the suggested terms,
// asked for after the player year (the eve of the contract-year end). AI
// clubs trade in that world, so only players it kept at club 3 until the
// contract year and renewed there are compared.
func TestSuggestedTermsAreTheAIDecision(t *testing.T) {
	plain, managed := newWorld(t, 42), userWorld(t, 42, userClub)
	playSeason(t, plain)
	playSeason(t, managed)
	end := managed.ContractYearEnd()
	mustContinue(t, managed, end-sim.GameInstant(sim.Day))
	suggested := map[ids.PlayerID]ContractOffer{}
	for _, p := range finalYear(t, managed, userClub) {
		suggested[p.Player] = suggest(t, managed, p.Player)
	}
	mustContinue(t, plain, end-sim.GameInstant(sim.Day))
	kept := map[ids.PlayerID]bool{}
	for _, p := range finalYear(t, plain, userClub) {
		kept[p.Player] = true
	}
	mustContinue(t, plain, end)
	checked := 0
	for id, offer := range suggested {
		a, _ := plain.employment.Assignment(id)
		if !kept[id] || a.Club != userClub {
			continue // sold by the AI, or not renewed
		}
		if want := mustAddYears(t, plain, end, offer.Years); a.Contract != (employment.Contract{Expires: want, WeeklyWage: offer.WeeklyWage}) {
			t.Fatalf("player %d: AI renewal %+v, suggested %+v", id, a, offer)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("the AI renewed none of the suggested players")
	}
	// A free agent's suggestion is a valid signing.
	mustContinue(t, managed, end)
	fa := managed.FreeAgents()[0]
	if _, err := managed.SignPlayer(SignPlayer{ID: managed.NextCommandID(), ExpectedRevision: managed.Revision(), Player: fa.Player, Offer: suggest(t, managed, fa.Player)}); err != nil {
		t.Fatal(err)
	}
}

// Retries return the recorded result and change nothing, also after a save;
// an ID cannot be reused for other content; a signed player cannot be
// signed twice.
func TestContractCommandRetriesAndSaves(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playSeason(t, w)
	mustContinue(t, w, w.ContractYearEnd())
	p := finalYear(t, w, userClub)[0]
	rq := RenewContract{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: p.Player, Offer: suggest(t, w, p.Player)}
	first, err := w.RenewContract(rq)
	if err != nil {
		t.Fatal(err)
	}
	fa := w.FreeAgents()[0]
	sq := SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: fa.Player, Offer: suggest(t, w, fa.Player)}
	signed, err := w.SignPlayer(sq)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := w.employment.Assignment(fa.Player); !ok || a.Club != userClub || a.Contract != signed.Contract {
		t.Fatalf("signed player %d: %+v", fa.Player, a)
	}
	for _, x := range []*World{w, roundTrip(t, w)} {
		before := snapshot(x)
		emp := x.employment.Assignments()
		if again, err := x.RenewContract(rq); err != nil || again != first {
			t.Fatalf("renew retry = %+v, %v", again, err)
		}
		if again, err := x.SignPlayer(sq); err != nil || again != signed {
			t.Fatalf("sign retry = %+v, %v", again, err)
		}
		other := rq
		other.Offer.WeeklyWage += money.Units(10)
		if _, err := x.RenewContract(other); !errors.Is(err, ErrCommandIDReused) {
			t.Fatalf("reused renew ID: %v", err)
		}
		if _, err := x.SignPlayer(SignPlayer{ID: rq.ID, ExpectedRevision: x.Revision(), Player: sq.Player, Offer: sq.Offer}); !errors.Is(err, ErrCommandIDReused) {
			t.Fatalf("renew ID reused for a signing: %v", err)
		}
		if _, err := x.SignPlayer(SignPlayer{ID: x.NextCommandID(), ExpectedRevision: x.Revision(), Player: sq.Player, Offer: sq.Offer}); !errors.Is(err, ErrNotFreeAgent) {
			t.Fatalf("signing twice: %v", err)
		}
		if !reflect.DeepEqual(snapshot(x), before) || !reflect.DeepEqual(x.employment.Assignments(), emp) {
			t.Fatal("a retry or rejected command changed the world")
		}
	}
}

func TestContractCommandRejections(t *testing.T) {
	w := userWorld(t, 42, userClub)
	playSeason(t, w)
	mine := finalYear(t, w, userClub)[0]
	var later SquadPlayer // a user player not in the final year
	squad, _ := w.Squad(userClub)
	for _, p := range squad {
		if p.Contract.Expires > w.ContractYearEnd() {
			later = p
		}
	}
	theirs := finalYear(t, w, 1)[0]
	good := suggest(t, w, mine.Player)
	e := w.defs.Economy
	with := func(f func(*RenewContract)) RenewContract {
		c := RenewContract{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: mine.Player, Offer: good}
		f(&c)
		return c
	}
	before, emp := snapshot(w), w.employment.Assignments()
	for name, c := range map[string]struct {
		cmd  RenewContract
		want error
	}{
		"zero ID":          {with(func(c *RenewContract) { c.ID = 0 }), ErrInvalidCommand},
		"stale revision":   {with(func(c *RenewContract) { c.ExpectedRevision-- }), ErrStaleRevision},
		"AI club's player": {with(func(c *RenewContract) { c.Player = theirs.Player }), ErrNotUserPlayer},
		"unknown player":   {with(func(c *RenewContract) { c.Player = 999 }), ErrNotUserPlayer},
		"not final year":   {with(func(c *RenewContract) { c.Player, c.Offer = later.Player, ContractOffer{2, later.Demand} }), ErrNotFinalYear},
		"too short":        {with(func(c *RenewContract) { c.Offer.Years = e.ContractYears[0] - 1 }), ErrOfferRejected},
		"too long":         {with(func(c *RenewContract) { c.Offer.Years = e.ContractYears[1] + 1 }), ErrOfferRejected},
		"below demand":     {with(func(c *RenewContract) { c.Offer.WeeklyWage = mine.Demand - 1 }), ErrOfferRejected},
		"above ceiling":    {with(func(c *RenewContract) { c.Offer.WeeklyWage = e.OfferCeiling(mine.Overall) + 1 }), ErrOfferRejected},
	} {
		if _, err := w.RenewContract(c.cmd); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	plain := newWorld(t, 42)
	if _, err := plain.RenewContract(RenewContract{ID: 1, Player: 1, Offer: good}); !errors.Is(err, ErrNoUserClub) {
		t.Errorf("no user club: %v", err)
	}
	if _, err := w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: theirs.Player, Offer: good}); !errors.Is(err, ErrNotFreeAgent) {
		t.Errorf("signing an employed player: %v", err)
	}
	if !reflect.DeepEqual(snapshot(w), before) || !reflect.DeepEqual(w.employment.Assignments(), emp) {
		t.Fatal("rejected commands changed the world")
	}

	// After the contract year there are free agents; signing needs room in
	// the squad and no round awaiting results.
	mustContinue(t, w, w.ContractYearEnd())
	sign := func() error {
		if len(w.FreeAgents()) == 0 { // AI clubs signed every free agent in the window
			release(t, w, userClub+1, players.Defender)
		}
		fa := w.FreeAgents()[0]
		_, err := w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: fa.Player, Offer: suggest(t, w, fa.Player)})
		return err
	}
	limit := w.defs.SquadLimit
	w.defs.SquadLimit = len(w.employment.Squad(mustUserTeam(t, w)))
	if err := sign(); !errors.Is(err, ErrSquadFull) {
		t.Errorf("full squad: %v", err)
	}
	w.defs.SquadLimit = limit
	readyBatch(t, w)
	if err := sign(); !errors.Is(err, ErrSquadsLocked) {
		t.Errorf("signing while a round awaits results: %v", err)
	}
	resolveNow(t, w)
	if err := sign(); err != nil {
		t.Fatalf("signing after the round: %v", err)
	}
}

// A contract year that cannot keep every squad legal fails as a whole: no
// contract changes, no events, and the task stays queued for a retry.
func TestFailedContractYearChangesNothing(t *testing.T) {
	w := newWorld(t, 42)
	playSeason(t, w)
	end := w.ContractYearEnd()
	mustContinue(t, w, end-1)
	before, emp := snapshot(w), w.employment.Assignments()
	w.defs.Roster[0].Min = 4 // more goalkeepers than the world has
	if _, err := w.Continue(end); err == nil {
		t.Fatal("impossible contract year succeeded")
	}
	after := snapshot(w)
	before.Revision, after.Revision = 0, 0 // a failed cohort may still publish the clock
	if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(w.employment.Assignments(), emp) {
		t.Fatal("the failed contract year changed the world")
	}
	w.defs.Roster[0].Min = 2
	mustContinue(t, w, end)
	if w.ContractYearEnd() == end {
		t.Fatal("the retried contract year did not run")
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsInvalidContracts(t *testing.T) {
	var next sim.GameInstant
	build := func() WorldSnapshot {
		w := userWorld(t, 42, userClub)
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
		fa := w.FreeAgents()[0]
		if _, err := w.SignPlayer(SignPlayer{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Player: fa.Player, Offer: suggest(t, w, fa.Player)}); err != nil {
			t.Fatal(err)
		}
		p := finalYear(t, w, userClub)[0]
		renew(t, w, p.Player, suggest(t, w, p.Player))
		next = w.ContractYearEnd()
		return w.Snapshot()
	}
	w := newWorld(t, 42)
	of := func(s *WorldSnapshot, club ids.ClubID, pos players.Position) []int {
		var out []int
		for i, a := range s.Employment {
			if a.Club == club && s.Players[slices.IndexFunc(s.Players, func(p players.Profile) bool { return p.Player == a.Player })].Position == pos {
				out = append(out, i)
			}
		}
		return out
	}
	cases := map[string]func(*WorldSnapshot){
		"contract off the year end": func(s *WorldSnapshot) { s.Employment[0].Contract.Expires += day },
		"contract already ended":    func(s *WorldSnapshot) { s.Employment[0].Contract.Expires = mustAddYears(t, w, next, -1) },
		"contract too long":         func(s *WorldSnapshot) { s.Employment[0].Contract.Expires = mustAddYears(t, w, next, 5) },
		"squad below minimum": func(s *WorldSnapshot) {
			gk := of(s, 1, players.Goalkeeper)
			s.Employment = slices.Delete(s.Employment, gk[0], gk[0]+1)
			gk = of(s, 1, players.Goalkeeper)
			s.Employment = slices.Delete(s.Employment, gk[0], gk[0]+1)
		},
		"squad above the limit": func(s *WorldSnapshot) {
			team := s.Employment[of(s, 1, players.Forward)[0]].Team
			for club := ids.ClubID(2); club <= 7; club++ { // one forward each: club 1 holds 26
				j := of(s, club, players.Forward)[0]
				s.Employment[j].Club, s.Employment[j].Team = 1, team
			}
		},
		"contract-year task missing": func(s *WorldSnapshot) {
			s.Scheduler.Tasks = slices.DeleteFunc(s.Scheduler.Tasks, func(t sim.Task) bool { return t.Kind == taskContractYear })
		},
		"contract-year task late": func(s *WorldSnapshot) {
			for i := range s.Scheduler.Tasks {
				if s.Scheduler.Tasks[i].Kind == taskContractYear {
					s.Scheduler.Tasks[i].DueAt += day
				}
			}
		},
		"renewal record wage edited":  func(s *WorldSnapshot) { s.RenewCommands[0].Result.Contract.WeeklyWage++ },
		"renewal record breaks rules": func(s *WorldSnapshot) { s.RenewCommands[0].Request.Offer.Years = 9 },
		"signing record other team":   func(s *WorldSnapshot) { s.SignCommands[0].Result.Team = 1 },
		"signing event other club": func(s *WorldSnapshot) {
			for i := range s.Events {
				if s.Events[i].Kind == events.KindPlayerSigned {
					s.Events[i].PlayerSigned.Club = 1
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
	snap := build()
	snap.Versions.Contracts++
	if _, err := Restore(snap); !errors.Is(err, ErrIncompatibleSave) {
		t.Errorf("other contracts version: %v", err)
	}
	if _, err := Restore(build()); err != nil {
		t.Fatalf("unmodified snapshot rejected: %v", err)
	}
}

// Every year the contract-year end leaves the best free agents AI clubs
// would sign in the pool, near the average player, for the manager (and,
// after the first half of the window, for the AI clubs).
func TestFreeAgentPoolAtTheWindowsOpen(t *testing.T) {
	w := userWorld(t, 7, 0)
	for year := 1; year <= 8; year++ {
		playSeason(t, w)
		mustContinue(t, w, w.ContractYearEnd())
		pool := w.FreeAgents()
		if len(pool) < freeAgentReserve {
			t.Fatalf("year %d: %d free agents at the window's open", year, len(pool))
		}
		best := 0
		for _, p := range pool {
			best = max(best, p.Overall)
		}
		if avg := averageOverall(w); best < avg-10 {
			t.Fatalf("year %d: best free agent %d, average %d", year, best, avg)
		}
	}
}
