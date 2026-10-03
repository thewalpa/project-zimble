package app

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
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

// Table marks: play-off places before the ties, then the decided movement;
// a play-off place that won its tie the other way keeps its play-off mark.
// Play-off names carry their divisions.
func TestTableMarksAndPlayoffNames(t *testing.T) {
	w := newWorld(t, 42)
	playLeagues(t, w)
	count := func() map[TableMark]int {
		marks := map[TableMark]int{}
		for _, l := range w.Promotions() {
			for _, comp := range []ids.CompetitionID{l.Upper, l.Lower} {
				ref := competitions.SeasonRef{Competition: comp, Season: 1}
				table, _ := w.Table(ref)
				for pos := 1; pos <= len(table.Rows); pos++ {
					m := w.TableMark(ref, pos)
					if (m != MarkNone) != w.InPlayoff(ref, pos) {
						t.Fatalf("%s position %d: mark %d outside the play-off places", ref, pos, m)
					}
					marks[m]++
				}
			}
		}
		return marks
	}
	places := 0
	for _, l := range w.Promotions() {
		places += l.Places
	}
	if m := count(); m[MarkPlayoffUp] != places || m[MarkPlayoffDown] != places || m[MarkPromoted]+m[MarkRelegated] != 0 {
		t.Fatalf("marks before the play-offs: %v", m)
	}
	playPlayoffs(t, w)
	if m := count(); m[MarkPromoted] != m[MarkRelegated] || m[MarkPromoted]+m[MarkPlayoffUp] != places || m[MarkRelegated]+m[MarkPlayoffDown] != places {
		t.Fatalf("marks after the play-offs: %v", m)
	}

	for _, p := range w.Playoffs() {
		upper, lower, ok := w.PlayoffDivisions(p.Competition)
		if !ok || upper == "" || lower == "" || w.PlayoffTitle(p.Competition) != fmt.Sprintf("%s (%s / %s)", p.Name, upper, lower) {
			t.Fatalf("play-off %d: divisions %q, %q, %v; title %q", p.Competition, upper, lower, ok, w.PlayoffTitle(p.Competition))
		}
	}
	if _, _, ok := w.PlayoffDivisions(1); ok || w.PlayoffTitle(1) != "" {
		t.Fatal("a league is named as a play-off")
	}
}

// Manager views preserve today's exact facts and negotiated prices, including
// a custom listing, but require a real observing club and never change saves.
func TestObservedPlayerViews(t *testing.T) {
	w := userWorld(t, 42, userClub3)
	squad, _ := w.Squad(userClub3)
	asking := squad[0].Value + 12300
	if _, err := w.ListPlayer(listCmd(w, squad[0].Player, asking)); err != nil {
		t.Fatal(err)
	}
	release(t, w, userClub3, players.Forward)
	before := w.Snapshot()
	for _, observer := range []ids.ClubID{1, userClub3} {
		for _, club := range []ids.ClubID{1, userClub3} {
			want, _ := w.Squad(club)
			got, ok := w.ObservedSquad(observer, club)
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("observer %d squad %d: knowledge or negotiation terms changed", observer, club)
			}
			for _, row := range got {
				want, _ := w.PlayerProfile(row.Player)
				got, ok := w.ObservedPlayerProfile(observer, row.Player)
				if !ok || got != want {
					t.Fatalf("observer %d player %d: profile changed", observer, row.Player)
				}
			}
			got[0].Attributes[0] = 1
			got[0].Name = "changed copy"
		}
		agents, ok := w.ObservedFreeAgents(observer)
		if !ok || len(agents) == 0 || !reflect.DeepEqual(agents, w.FreeAgents()) {
			t.Fatal("free-agent knowledge changed")
		}
		for _, row := range agents {
			want, _ := w.PlayerProfile(row.Player)
			got, ok := w.ObservedPlayerProfile(observer, row.Player)
			if !ok || got != want || got.Club != 0 || got.Retired {
				t.Fatalf("free-agent profile changed: %+v", got)
			}
		}
		listed, ok := w.ObservedTransferList(observer)
		if !ok || !reflect.DeepEqual(listed, w.TransferList()) || len(listed) == 0 || listed[0].Value != asking {
			t.Fatal("observations lost custom listing terms")
		}
		listed[0].Value = 0
		listed[0].Attributes[0] = 1
	}
	for _, observer := range []ids.ClubID{0, 99999} {
		if rows, ok := w.ObservedSquad(observer, userClub3); ok || rows != nil {
			t.Fatal("invalid observer received a squad")
		}
		if rows, ok := w.ObservedFreeAgents(observer); ok || rows != nil {
			t.Fatal("invalid observer received free agents")
		}
		if rows, ok := w.ObservedTransferList(observer); ok || rows != nil {
			t.Fatal("invalid observer received transfer listings")
		}
		if p, ok := w.ObservedPlayerProfile(observer, squad[0].Player); ok || p != (PlayerProfile{}) {
			t.Fatal("invalid observer received a player profile")
		}
	}
	if rows, ok := w.ObservedSquad(userClub3, 99999); ok || rows != nil {
		t.Fatal("unknown squad was observed")
	}
	for _, id := range []ids.PlayerID{0, 99999} {
		if p, ok := w.ObservedPlayerProfile(userClub3, id); ok || p != (PlayerProfile{}) {
			t.Fatal("unknown player was observed")
		}
	}
	if !reflect.DeepEqual(w.Snapshot(), before) {
		t.Fatal("reading or editing observed views changed the saved world")
	}
}

// A preview shows exactly what recording the stops later shows, past half
// time and after a decision, with the frames LiveFrames gives, and records
// nothing.
func TestPreviewLive(t *testing.T) {
	w := engineWorld(t, 42, "tick")
	playBatches(t, w, 1)
	fixture := readyBatch(t, w).UserFixtures[0]
	preview := func(to uint16) LivePreview {
		t.Helper()
		before := w.Snapshot()
		p, err := w.PreviewLive(fixture, to, true, 1000)
		if err != nil {
			t.Fatalf("preview to %d: %v", to, err)
		}
		if !reflect.DeepEqual(w.Snapshot(), before) {
			t.Fatal("a preview changed the world")
		}
		return p
	}
	if p := preview(0); p.Played != 0 || p.Live.Position.Minute != 0 || len(p.Live.Events) != 0 || p.Live.View.OnPitch[0][0] == 0 || len(p.Frames) != 0 {
		t.Fatalf("kickoff preview %+v", p.Live.Position)
	}
	full := preview(90)
	if full.Live.Status != matches.MatchFinished || full.Frames[len(full.Frames)-1].Millis != 90*60_000 {
		t.Fatalf("a preview to full time stopped at %d", full.Live.Position.Minute)
	}

	ht := preview(45)
	if got := playTo(t, w, fixture, 30); got.Position.Minute != 30 {
		t.Fatal("did not stop at 30")
	}
	frames, err := w.LiveFrames(1000)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ht.Frames[:len(frames)], frames) {
		t.Fatal("the preview's frames differ from the recorded play's")
	}
	if got := playTo(t, w, fixture, 45); !reflect.DeepEqual(got, ht.Live) {
		t.Fatal("playing to half time in two steps differs from its preview")
	}
	sub := forwardSub(ht.Live)
	decide(t, w, fixture, sub)
	later := preview(70)
	if later.Played != 45 || later.Frames[0].Millis <= 45*60_000 {
		t.Fatalf("preview from %d starts at %d ms", later.Played, later.Frames[0].Millis)
	}
	if got := playTo(t, w, fixture, 70); !reflect.DeepEqual(got, later.Live) {
		t.Fatal("playing on after a decision differs from its preview")
	}

	for _, to := range []uint16{60, 91} {
		if _, err := w.PreviewLive(fixture, to, false, 0); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("preview to %d from 70: %v", to, err)
		}
	}
	if _, err := w.PreviewLive(fixture+1, 80, false, 0); err == nil {
		t.Fatal("previewed another fixture")
	}
}

// A club's seasons list the league and the cups it entered; its moves list
// who joined and left. Both agree with the other views and change nothing.
func TestClubSeasonsAndMoves(t *testing.T) {
	w := newWorld(t, 42)
	club := w.Summary().ClubRows[0].ID
	if got := w.ClubSeasons(club); len(got) != 1 || !got[0].League || got[0].Complete || got[0].Standing.Played != 0 {
		t.Fatalf("seasons before play = %+v, want the league season in progress", got)
	}
	if got := w.ClubMoves(club); len(got) != 0 {
		t.Fatalf("moves before play = %+v", got)
	}
	playSeason(t, w)
	rev := w.Revision()
	seasons := w.ClubSeasons(club)
	var league *ClubSeason
	for i, s := range seasons {
		if s.League && s.Season.Season == 1 {
			league = &seasons[i]
		}
		if s.Name == "" || (!s.League && s.Stage == "") {
			t.Fatalf("season without a name or stage: %+v", s)
		}
	}
	if league == nil || !league.Complete || league.Standing.Rank < 1 || league.Standing.Played == 0 {
		t.Fatalf("league season 1 = %+v", league)
	}
	table, _ := w.Table(league.Season)
	if got := table.Rows[league.Standing.Rank-1].Label.Club; got != club {
		t.Fatalf("the club's place %d is held by club %d", league.Standing.Rank, got)
	}
	for _, m := range w.ClubMoves(club) {
		if m.PlayerName == "" || (m.Kind == MoveBought || m.Kind == MoveSold) && (m.OtherName == "" || m.Fee <= 0) {
			t.Fatalf("incomplete move %+v", m)
		}
	}
	if w.Revision() != rev {
		t.Fatal("the views changed the world")
	}
}

// FormationRoles keeps the goalkeeper's slot and as many roles as it can,
// takes players by line then slot, and refuses shapes that do not fit.
func TestFormationRoles(t *testing.T) {
	gk, df, mf, fw := matches.Goalkeeper, matches.Defender, matches.Midfielder, matches.Forward
	// A 4-4-2 after a substitution put a forward in a defender's slot.
	current := [matches.StartersPerTeam]matches.Role{gk, df, fw, df, df, mf, mf, mf, mf, fw, df}
	if got := RolesLabel(current); got != "4-4-2" {
		t.Fatalf("label %s", got)
	}
	next, err := FormationRoles(current, "4-3-3")
	if err != nil {
		t.Fatal(err)
	}
	want := [matches.StartersPerTeam]matches.Role{gk, df, fw, df, df, mf, mf, mf, fw, fw, df}
	if next != want {
		t.Fatalf("4-3-3: %v, want %v", next, want)
	}
	if err := matches.CheckRoles(current, next); err != nil {
		t.Fatal(err)
	}
	if same, err := FormationRoles(current, " 4-4-2 "); err != nil || same != current {
		t.Fatalf("4-4-2: %v, %v", same, err)
	}
	for _, bad := range []string{"", "4-4", "4-4-3", "4-x-2", "-1-6-5", "4-4-2-0"} {
		if got, err := FormationRoles(current, bad); err == nil || got != current {
			t.Errorf("%q: %v, %v", bad, got, err)
		}
	}
}
