package app

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/selection"
)

// The promotion views follow the pinned links: the first divisions mark
// their bottom places as play-off places, the second divisions their top
// places, and a cup or an unlinked league none. Until the play-offs decide,
// no place has a movement.
func TestPromotionViews(t *testing.T) {
	w := newWorld(t, 42)
	for _, l := range w.Promotions() {
		if up, down := w.PromotionPlaces(l.Upper); up != 0 || down != l.Places {
			t.Fatalf("upper league %d: up %d, down %d, want 0 and %d", l.Upper, up, down, l.Places)
		}
		if up, down := w.PromotionPlaces(l.Lower); up != l.Places || down != 0 {
			t.Fatalf("lower league %d: up %d, down %d, want %d and 0", l.Lower, up, down, l.Places)
		}
		upper := competitions.SeasonRef{Competition: l.Upper, Season: 1}
		lower := competitions.SeasonRef{Competition: l.Lower, Season: 1}
		table, _ := w.Table(upper)
		n := len(table.Rows)
		for pos := 1; pos <= n; pos++ {
			if got := w.InPlayoff(upper, pos); got != (pos > n-l.Places) {
				t.Fatalf("upper position %d: in play-off %v", pos, got)
			}
			if promoted, relegated := w.SeasonMove(upper, pos); promoted || relegated {
				t.Fatalf("upper position %d moved before the play-offs: promoted %v, relegated %v", pos, promoted, relegated)
			}
			if got := w.InPlayoff(lower, pos); got != (pos <= l.Places) {
				t.Fatalf("lower position %d: in play-off %v", pos, got)
			}
			if promoted, relegated := w.SeasonMove(lower, pos); promoted || relegated {
				t.Fatalf("lower position %d moved before the play-offs: promoted %v, relegated %v", pos, promoted, relegated)
			}
		}
	}
	if up, down := w.PromotionPlaces(3); up != 0 || down != 0 { // the Continental Cup
		t.Fatalf("cup: up %d, down %d", up, down)
	}
	if w.InPlayoff(competitions.SeasonRef{Competition: 1, Season: 1}, 0) {
		t.Fatal("position 0 is in a play-off")
	}
	if p, r := w.SeasonMove(competitions.SeasonRef{Competition: 1, Season: 1}, 0); p || r {
		t.Fatal("position 0 moved")
	}
}

// A formation counts outfield starters per line, defence first, whatever the
// slot order.
func TestFormationLabel(t *testing.T) {
	roles := []matches.Role{matches.Forward, matches.Defender, matches.Goalkeeper, matches.Midfielder, matches.Defender,
		matches.Forward, matches.Midfielder, matches.Defender, matches.Midfielder, matches.Forward, matches.Defender}
	var l selection.Lineup
	for i, r := range roles {
		l.Starters = append(l.Starters, selection.Slot{Player: ids.PlayerID(i + 1), Role: r})
	}
	if got := FormationLabel(l); got != "4-3-3" {
		t.Fatalf("formation %q, want 4-3-3", got)
	}
	if got := FormationLabel(selection.Lineup{}); got != "0-0-0" {
		t.Fatalf("empty formation %q", got)
	}
}

func agendaOf(w *World, kind AgendaKind) []AgendaItem {
	var out []AgendaItem
	for _, it := range w.Agenda() {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// The agenda is ordered by date, has no user club to plan for in a headless
// world, and lists the matchday as the one item the manager must act on.
func TestAgendaOrdersWhatIsAhead(t *testing.T) {
	if got := newWorld(t, 42).Agenda(); got != nil {
		t.Fatalf("world without a user club has an agenda: %+v", got)
	}
	w := userWorld(t, 42, userClub3)
	items := w.Agenda()
	if len(agendaOf(w, AgendaFixture)) == 0 || len(agendaOf(w, AgendaWindow)) != 1 {
		t.Fatalf("kickoff agenda %+v", items)
	}
	for i, it := range items {
		if it.Text == "" || it.Now {
			t.Fatalf("item %d before the first matchday: %+v", i, it)
		}
		if i > 0 && it.At < items[i-1].At {
			t.Fatalf("agenda not in date order at %d: %+v", i, items)
		}
	}
	if n := len(agendaOf(w, AgendaFixture)); n > agendaFixtures {
		t.Fatalf("%d upcoming matches listed", n)
	}

	mustContinue(t, w, w.ContractYearEnd()) // stops at the first matchday
	match := agendaOf(w, AgendaMatchday)
	if len(match) != 1 || !match[0].Now {
		t.Fatalf("matchday agenda %+v", w.Agenda())
	}
	ready, _ := w.Pending()
	if match[0].Fixture != ready.UserFixtures[0] {
		t.Fatalf("matchday item is fixture %d, pending %v", match[0].Fixture, ready.UserFixtures)
	}
	if w.Agenda()[0].Kind != AgendaMatchday {
		t.Fatalf("the matchday is not first: %+v", w.Agenda())
	}
}

// Contracts in their final year appear once each, and an agenda read changes
// nothing.
func TestAgendaListsExpiringContractsAndBids(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	before := w.Revision()
	want := 0
	squad, _ := w.Squad(userClub3)
	for _, p := range squad {
		if p.Contract.Expires == w.ContractYearEnd() {
			want++
		}
	}
	got := agendaOf(w, AgendaContract)
	if len(got) != want {
		t.Fatalf("%d contract items, %d final-year players", len(got), want)
	}
	for _, it := range got {
		if it.Player == 0 || it.At != w.ContractYearEnd() {
			t.Fatalf("contract item %+v", it)
		}
	}
	if w.Revision() != before {
		t.Fatal("Agenda changed the revision")
	}

	w, offer := bidScenario(t)(t)
	bids := agendaOf(w, AgendaBidToAnswer)
	if len(bids) != 1 || !bids[0].Now || bids[0].Offer != offer.ID || bids[0].At != offer.Deadline {
		t.Fatalf("bid items %+v for offer %+v", bids, offer)
	}
	if len(agendaOf(w, AgendaBidPending)) != 0 {
		t.Fatalf("the seller sees its own pending bid: %+v", w.Agenda())
	}
}

// Statistics rows: none without statistics, possession adds up to 100 and
// every row has a label.
func TestStatLines(t *testing.T) {
	if rows := StatLines(matches.MatchStats{}); rows != nil {
		t.Fatalf("unavailable statistics gave rows %v", rows)
	}
	s := matches.MatchStats{Available: true, Teams: [2]matches.TeamStats{
		{Shots: 9, ShotsOnTarget: 4, Passes: 301, PassesCompleted: 250, PossessionPermille: 505},
		{Shots: 3, ShotsOnTarget: 1, PossessionPermille: 495},
	}}
	rows := StatLines(s)
	if len(rows) != 7 || rows[0] != (StatLine{"Possession", "51%", "49%"}) || rows[3] != (StatLine{"Passes", "301 (83%)", "0"}) {
		t.Fatalf("rows %v", rows)
	}
	kickoff := StatLines(matches.MatchStats{Available: true})
	if kickoff[0] != (StatLine{"Possession", "0%", "0%"}) {
		t.Fatalf("possession before anyone had the ball: %v", kickoff[0])
	}
}
