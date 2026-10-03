package app

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
	"github.com/thewalpa/project-zimble/internal/transfers"
)

// OpponentName returns the name of the opponent club for the user team in fixture.
func (w *World) OpponentName(fixture ids.FixtureID) string {
	f, ok := w.competitions.Fixture(fixture)
	if !ok {
		return ""
	}
	team, hasTeam := w.userTeam()
	if !hasTeam {
		return ""
	}
	opp := f.Home
	if opp == team {
		opp = f.Away
	}
	t, ok := w.registry.Team(opp)
	if !ok {
		return ""
	}
	c, ok := w.registry.Club(t.Club)
	if !ok {
		return ""
	}
	return c.Name
}

// FormationLabel names a lineup's shape by its outfield starters per line,
// defence first: "4-4-2". Read-only.
func FormationLabel(l selection.Lineup) string {
	roles := make([]matches.Role, len(l.Starters))
	for i, s := range l.Starters {
		roles[i] = s.Role
	}
	return shapeLabel(roles)
}

// RolesLabel names the shape of a side's roles in slot order (a live
// match's MatchView.Roles, a formation change's MatchEvent.Roles) like
// FormationLabel. Read-only.
func RolesLabel(roles [matches.StartersPerTeam]matches.Role) string { return shapeLabel(roles[:]) }

func shapeLabel(roles []matches.Role) string {
	var count [matches.Forward + 1]int
	for _, r := range roles {
		if r.Valid() {
			count[r]++
		}
	}
	return fmt.Sprintf("%d-%d-%d", count[matches.Defender], count[matches.Midfielder], count[matches.Forward])
}

// FormationRoles turns a shape such as "4-3-3" (defenders, midfielders,
// forwards) into new roles for a side whose roles are current, in slot
// order, for matches.CommandSetRoles. The goalkeeper's slot is kept. The
// outfield players are taken by their current line (defence first), then
// slot order, and fill the new lines in that order, so as few players as
// possible change role. The numbers must add up to the outfield slots.
// It does not check that anything changed (see matches.CheckRoles).
// Read-only.
func FormationRoles(current [matches.StartersPerTeam]matches.Role, formation string) ([matches.StartersPerTeam]matches.Role, error) {
	next := current
	var want [3]int
	parts := strings.Split(strings.TrimSpace(formation), "-")
	if len(parts) != len(want) {
		return next, fmt.Errorf("formation %q: want defenders-midfielders-forwards, such as 4-4-2", formation)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return next, fmt.Errorf("formation %q: want defenders-midfielders-forwards, such as 4-4-2", formation)
		}
		want[i] = n
	}
	var outfield []int
	for slot, r := range current {
		if r != matches.Goalkeeper {
			outfield = append(outfield, slot)
		}
	}
	if want[0]+want[1]+want[2] != len(outfield) {
		return next, fmt.Errorf("formation %s has %d outfield players; the side has %d", formation, want[0]+want[1]+want[2], len(outfield))
	}
	slices.SortStableFunc(outfield, func(a, b int) int { return cmp.Compare(current[a], current[b]) })
	for i, slot := range outfield {
		switch {
		case i < want[0]:
			next[slot] = matches.Defender
		case i < want[0]+want[1]:
			next[slot] = matches.Midfielder
		default:
			next[slot] = matches.Forward
		}
	}
	return next, nil
}

// Ordinal writes a place in a table or ranking: "1st", "2nd", "11th".
func Ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// NaturalRole is the role a player of the position plays in: a starter in
// any other role plays out of position. Read-only.
func NaturalRole(p players.Position) matches.Role { return roleOf(p) }

// LineupSourceLabel returns the human-readable description of where ml came from,
// e.g. "Your saved lineup for this match", "Carried over from the last match (vs Hollowick Town)",
// or "The assistant's suggestion".
func (w *World) LineupSourceLabel(ml MatchdayLineup) string {
	switch ml.Source {
	case LineupFromSubmission:
		return "Your saved lineup for this match"
	case LineupFromPlan:
		return "Your saved team plan for this match"
	case LineupCarriedOver:
		if opp := w.OpponentName(ml.From); opp != "" {
			return fmt.Sprintf("Carried over from the last match (vs %s)", opp)
		}
		return "Carried over from the last match"
	case LineupSuggested:
		return "The assistant's suggestion"
	default:
		return "The assistant's suggestion"
	}
}

// LineupDroppedMessages returns explanations for any players dropped from a
// carried-over lineup.
func (w *World) LineupDroppedMessages(ml MatchdayLineup) []string {
	if len(ml.Dropped) == 0 || ml.From == 0 {
		return nil
	}
	old, ok := w.SubmittedLineup(ml.From)
	if !ok {
		return nil
	}
	team, hasTeam := w.userTeam()
	inSquad := func(p ids.PlayerID) bool {
		if !hasTeam {
			return false
		}
		a, ok := w.employment.Assignment(p)
		return ok && a.Team == team
	}

	var msgs []string
	for _, p := range ml.Dropped {
		pName, _ := w.PlayerName(p)
		if pName == "" {
			pName = fmt.Sprintf("Player %d", p)
		}
		starterIdx := -1
		for i, s := range old.Starters {
			if s.Player == p {
				starterIdx = i
				break
			}
		}
		if starterIdx >= 0 && starterIdx < len(ml.Lineup.Starters) {
			repl := ml.Lineup.Starters[starterIdx].Player
			replName, _ := w.PlayerName(repl)
			if replName == "" {
				replName = fmt.Sprintf("player %d", repl)
			}
			if !inSquad(p) {
				msgs = append(msgs, fmt.Sprintf("%s has left the club; %s takes his place", pName, replName))
			} else {
				msgs = append(msgs, fmt.Sprintf("%s was replaced by %s in the starting lineup", pName, replName))
			}
		} else {
			if !inSquad(p) {
				msgs = append(msgs, fmt.Sprintf("%s has left the club", pName))
			} else {
				msgs = append(msgs, fmt.Sprintf("%s was dropped to fit the bench limit", pName))
			}
		}
	}
	return msgs
}

// PlayerProfile is the page of one player: the derived SquadPlayer row, where
// he plays now (zero Club for a free agent or a retired player) and whether
// he has retired.
type PlayerProfile struct {
	SquadPlayer
	Club     ids.ClubID
	ClubName string
	Retired  bool
}

// PlayerProfile returns the profile of any player, at any club, free or
// retired, or false for an unknown player. Read-only.
func (w *World) PlayerProfile(id ids.PlayerID) (PlayerProfile, bool) {
	p, ok := w.players.Profile(id)
	if !ok {
		return PlayerProfile{}, false
	}
	out := PlayerProfile{SquadPlayer: w.squadPlayer(id), Retired: p.Retired}
	if a, ok := w.employment.Assignment(id); ok {
		out.Club = a.Club
		if c, ok := w.registry.Club(a.Club); ok {
			out.ClubName = c.Name
		}
	}
	return out, true
}

// PromotionPlaces returns how many places at the top of the league's table go
// up to the division above at the season's end, and how many at the bottom go
// down to the division below (zero when the league has no such link). Read-only.
func (w *World) PromotionPlaces(league ids.CompetitionID) (up, down int) {
	for _, l := range w.Promotions() {
		switch league {
		case l.Lower:
			up = l.Places
		case l.Upper:
			down = l.Places
		}
	}
	return up, down
}

// InPlayoff says whether the team finishing a league season at the position
// plays off at the season's end: for a place in the division above, from the
// top of a linked table, or to stay in this division, from its bottom. These
// zones are PromotionPlaces' places. Read-only.
func (w *World) InPlayoff(ref competitions.SeasonRef, position int) bool {
	up, down := w.PromotionPlaces(ref.Competition)
	if position < 1 {
		return false
	}
	t, ok := w.Table(ref)
	if !ok {
		return false
	}
	return position <= up || (down > 0 && position > len(t.Rows)-down)
}

// SeasonMove says where the team that finished a league season at the
// position went next season: promoted to the division above or relegated to
// the division below. Movement is decided once the season's play-offs are
// played (competitions.ApplyPlayoffs); until the next seasons exist — or for
// a play-off place that lost its tie the other way — both are false. A team
// can do both over chained links. Read-only.
func (w *World) SeasonMove(ref competitions.SeasonRef, position int) (promoted, relegated bool) {
	t, ok := w.Table(ref)
	if !ok || position < 1 || position > len(t.Rows) {
		return false, false
	}
	team := t.Rows[position-1].Team
	movedTo := func(comp ids.CompetitionID) bool {
		entrants, exists := w.competitions.Entrants(competitions.SeasonRef{Competition: comp, Season: ref.Season + 1})
		return exists && slices.Contains(entrants, team)
	}
	for _, p := range w.Promotions() {
		var up, down ids.CompetitionID
		switch ref.Competition {
		case p.Lower:
			up = p.Upper
		case p.Upper:
			down = p.Lower
		default:
			continue
		}
		if movedTo(up) {
			promoted = true
		}
		if movedTo(down) {
			relegated = true
		}
	}
	return promoted, relegated
}

// AgendaKind says what an agenda item asks of the manager. The values order
// the items that fall on the same instant.
type AgendaKind uint8

const (
	AgendaMatchday    AgendaKind = 1 // a match waits for the manager's lineup, now
	AgendaBidToAnswer AgendaKind = 2 // a bid for one of the club's players waits for an answer
	AgendaContract    AgendaKind = 3 // a contract in its final year ends
	AgendaFixture     AgendaKind = 4 // an upcoming match
	AgendaBidPending  AgendaKind = 5 // the club's own bid waits for the seller's answer
	AgendaWindow      AgendaKind = 6 // the transfer window opens, or its last bidding day
)

// AgendaItem is one thing the manager may need to act on. Text is the same
// sentence in every client. Fixture, Player and Offer name what the item is
// about, when it is about one.
type AgendaItem struct {
	Kind    AgendaKind
	At      sim.GameInstant // the kickoff, deadline or date it falls on
	Now     bool            // the manager must act before play can go on
	Text    string
	Fixture ids.FixtureID
	Player  ids.PlayerID
	Offer   ids.OfferID
}

// agendaFixtures is how many upcoming matches the agenda lists.
const agendaFixtures = 3

// Agenda lists what the user club has ahead in the order it falls due: the
// matchday, bids to answer, the next matches, contracts ending at the next
// contract-year end, the club's own bids and the transfer window's dates.
// Player facts come from the user club's ObservePlayers. An item the
// manager must act on before play can go on has Now set. It combines
// existing queries and changes nothing.
func (w *World) Agenda() []AgendaItem {
	if w.userClub == 0 {
		return nil
	}
	cal := w.calendar
	var out []AgendaItem

	pending := map[ids.FixtureID]bool{}
	if ready, ok := w.Pending(); ok {
		for _, id := range ready.UserFixtures {
			pending[id] = true
		}
	}
	var fixtures []FixtureInfo
	consider := func(f FixtureLine) {
		if f.Played || (f.Home.Club != w.userClub && f.Away.Club != w.userClub) {
			return
		}
		if info, ok := w.FixtureInfo(f.ID); ok {
			fixtures = append(fixtures, info)
		}
	}
	for _, sc := range w.Schedules() {
		for _, r := range sc.Rounds {
			for _, f := range r.Fixtures {
				consider(f)
			}
		}
	}
	for _, c := range w.Cups() {
		for _, r := range c.Rounds {
			for _, f := range r.Ties {
				consider(f)
			}
		}
	}
	for _, p := range w.Playoffs() {
		for _, r := range p.Rounds {
			for _, f := range r.Ties {
				consider(f)
			}
		}
	}
	slices.SortFunc(fixtures, func(a, b FixtureInfo) int {
		return cmp.Or(cmp.Compare(a.Kickoff, b.Kickoff), cmp.Compare(a.ID, b.ID))
	})
	upcoming := 0
	for _, info := range fixtures {
		against := info.Away.ClubName + " (home)"
		if info.Away.Club == w.userClub {
			against = info.Home.ClubName + " (away)"
		}
		match := fmt.Sprintf("%s %s v %s", info.CompetitionName, info.RoundName, against)
		if info.Playoff {
			match = fmt.Sprintf("%s v %s", info.CompetitionName, against) // one round: no round name
		}
		switch {
		case pending[info.ID]:
			out = append(out, AgendaItem{Kind: AgendaMatchday, At: info.Kickoff, Now: true, Fixture: info.ID,
				Text: fmt.Sprintf("Matchday: %s. Set the lineup, then play.", match)})
		case upcoming < agendaFixtures:
			upcoming++
			out = append(out, AgendaItem{Kind: AgendaFixture, At: info.Kickoff, Fixture: info.ID,
				Text: fmt.Sprintf("%s, %s", match, cal.Format(info.Kickoff))})
		}
	}

	for _, o := range w.Offers() {
		if o.Status != transfers.StatusOpen {
			continue
		}
		switch w.userClub {
		case o.Seller:
			out = append(out, AgendaItem{Kind: AgendaBidToAnswer, At: o.Deadline, Now: true, Offer: o.ID, Player: o.Player,
				Text: fmt.Sprintf("%s bid %s for %s. Answer by %s.", o.BuyerName, o.Fee, o.PlayerName, cal.Format(o.Deadline))})
		case o.Buyer:
			out = append(out, AgendaItem{Kind: AgendaBidPending, At: o.Deadline, Offer: o.ID, Player: o.Player,
				Text: fmt.Sprintf("Your bid of %s for %s waits for %s, due %s.", o.Fee, o.PlayerName, o.SellerName, cal.Format(o.Deadline))})
		}
	}

	end := w.ContractYearEnd()
	team, _ := w.registry.SeniorTeam(w.userClub)
	known, _ := w.ObservePlayers(w.userClub, w.employment.Squad(team)) // the user club is registered
	for _, p := range known.Players {
		if p.Contract.Expires == end {
			out = append(out, AgendaItem{Kind: AgendaContract, At: end, Player: p.Player,
				Text: fmt.Sprintf("%s (%s, %d): the contract ends %s. He asks %s a week to stay.", p.Name, p.Position, p.Age, cal.Format(end), p.Demand)})
		}
	}

	if win := w.TransferWindow(); win.Open {
		out = append(out, AgendaItem{Kind: AgendaWindow, At: win.BidsClose,
			Text: fmt.Sprintf("Transfer window: the last day to bid is %s; it closes %s.", cal.Format(win.BidsClose), cal.Format(win.Closes))})
	} else {
		out = append(out, AgendaItem{Kind: AgendaWindow, At: win.Opens,
			Text: fmt.Sprintf("Transfer window: opens %s.", cal.Format(win.Opens))})
	}

	slices.SortStableFunc(out, func(a, b AgendaItem) int {
		return cmp.Or(cmp.Compare(a.At, b.At), cmp.Compare(a.Kind, b.Kind))
	})
	return out
}

// StatLine is one row of a match statistics table: a label and the home
// and away values, formatted for display.
type StatLine struct {
	Label      string
	Home, Away string
}

// StatLines formats match statistics as table rows, possession first, with
// the same labels in every client. Possession is shown in whole percent
// that add up to 100; passes carry their completion rate. Without
// statistics (Available false) there are no rows: show nothing, never
// zeros. Read-only.
func StatLines(s matches.MatchStats) []StatLine {
	if !s.Available {
		return nil
	}
	h, a := s.Teams[0], s.Teams[1]
	var possession [2]int
	if total := int(h.PossessionPermille) + int(a.PossessionPermille); total > 0 {
		possession[0] = (int(h.PossessionPermille)*100 + total/2) / total
		possession[1] = 100 - possession[0]
	}
	passes := func(t matches.TeamStats) string {
		if t.Passes == 0 {
			return "0"
		}
		return fmt.Sprintf("%d (%d%%)", t.Passes, (int(t.PassesCompleted)*100+int(t.Passes)/2)/int(t.Passes))
	}
	row := func(label string, f func(matches.TeamStats) uint16) StatLine {
		return StatLine{Label: label, Home: fmt.Sprint(f(h)), Away: fmt.Sprint(f(a))}
	}
	return []StatLine{
		{Label: "Possession", Home: fmt.Sprintf("%d%%", possession[0]), Away: fmt.Sprintf("%d%%", possession[1])},
		row("Shots", func(t matches.TeamStats) uint16 { return t.Shots }),
		row("On target", func(t matches.TeamStats) uint16 { return t.ShotsOnTarget }),
		{Label: "Passes", Home: passes(h), Away: passes(a)},
		row("Tackles", func(t matches.TeamStats) uint16 { return t.Tackles }),
		row("Saves", func(t matches.TeamStats) uint16 { return t.Saves }),
		row("Offsides", func(t matches.TeamStats) uint16 { return t.Offsides }),
	}
}

// PlayoffDivisions names the two leagues a promotion play-off decides
// between: each tie's winner plays in the upper next season, its loser in
// the lower. ok is false for any other competition. Read-only.
func (w *World) PlayoffDivisions(comp ids.CompetitionID) (upper, lower string, ok bool) {
	l, ok := w.playoffLink(comp)
	if !ok {
		return "", "", false
	}
	return w.competitionName(l.Upper), w.competitionName(l.Lower), true
}

// PlayoffTitle names a promotion play-off with its divisions, e.g.
// "Promotion Play-off (Founders League / Founders Second Division)", since
// every play-off has the same competition name. Empty for any other
// competition. Read-only.
func (w *World) PlayoffTitle(comp ids.CompetitionID) string {
	upper, lower, ok := w.PlayoffDivisions(comp)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s (%s / %s)", w.competitionName(comp), upper, lower)
}

// TableMark is what a league place means for the team's next season.
type TableMark uint8

const (
	MarkNone        TableMark = 0
	MarkPlayoffUp   TableMark = 1 // plays off for a place in the division above
	MarkPlayoffDown TableMark = 2 // plays off to stay in this division
	MarkPromoted    TableMark = 3 // decided: plays in the division above next season
	MarkRelegated   TableMark = 4 // decided: plays in the division below next season
)

// TableMark marks the team finishing a league season at the position:
// promoted or relegated once the play-offs have decided (SeasonMove), and
// otherwise a play-off place (InPlayoff) at the top or the bottom of the
// table, which a place keeps after winning its tie the other way. Read-only.
func (w *World) TableMark(ref competitions.SeasonRef, position int) TableMark {
	switch promoted, relegated := w.SeasonMove(ref, position); {
	case promoted:
		return MarkPromoted
	case relegated:
		return MarkRelegated
	}
	if !w.InPlayoff(ref, position) {
		return MarkNone
	}
	if up, _ := w.PromotionPlaces(ref.Competition); position <= up {
		return MarkPlayoffUp
	}
	return MarkPlayoffDown
}

// ObservedSquad combines squad membership and negotiation terms with the
// observing club's player knowledge. False means either club is unknown.
// Manager clients pass UserClub as observer, even when viewing another club.
func (w *World) ObservedSquad(observer, club ids.ClubID) ([]SquadPlayer, bool) {
	squad, ok := w.Squad(club)
	if !ok {
		return nil, false
	}
	return w.observedPlayerRows(observer, squad)
}

// ObservedFreeAgents lists the available free agents as known by observer.
// False means the observing club is unknown (including zero).
func (w *World) ObservedFreeAgents(observer ids.ClubID) ([]SquadPlayer, bool) {
	return w.observedPlayerRows(observer, w.FreeAgents())
}

// ObservedPlayerProfile combines observer's knowledge with the player's
// negotiation terms and club label. False means observer or player is unknown.
// Free agents and retired players can be observed too.
func (w *World) ObservedPlayerProfile(observer ids.ClubID, id ids.PlayerID) (PlayerProfile, bool) {
	profile, ok := w.PlayerProfile(id)
	if !ok {
		return PlayerProfile{}, false
	}
	known, err := w.ObservePlayers(observer, []ids.PlayerID{id})
	if err != nil {
		return PlayerProfile{}, false
	}
	p := known.Players[0]
	profile.SquadPlayer = observedPlayerRow(p, profile.SquadPlayer)
	profile.Club, profile.Retired = p.Club, p.Retired
	profile.ClubName = ""
	if label, ok := w.ClubLabel(p.Club); ok {
		profile.ClubName = label.ClubName
	}
	return profile, true
}

// ObservedTransferList keeps listing terms on the transfer query and reads
// player facts through observer's knowledge. False means observer is unknown.
func (w *World) ObservedTransferList(observer ids.ClubID) ([]ListedPlayer, bool) {
	listed := w.TransferList()
	rows := make([]SquadPlayer, len(listed))
	for i, p := range listed {
		rows[i] = p.SquadPlayer
	}
	observed, ok := w.observedPlayerRows(observer, rows)
	if !ok {
		return nil, false
	}
	for i := range listed {
		listed[i].SquadPlayer = observed[i]
	}
	return listed, true
}

// observedPlayerRows preserves membership, ordering and negotiation terms;
// every player fact in the returned rows comes from the observation query.
func (w *World) observedPlayerRows(observer ids.ClubID, rows []SquadPlayer) ([]SquadPlayer, bool) {
	requested := make([]ids.PlayerID, len(rows))
	for i, p := range rows {
		requested[i] = p.Player
	}
	known, err := w.ObservePlayers(observer, requested)
	if err != nil {
		return nil, false
	}
	byID := make(map[ids.PlayerID]PlayerObservation, len(known.Players))
	for _, p := range known.Players {
		byID[p.Player] = p
	}
	out := slices.Clone(rows)
	for i, p := range rows {
		out[i] = observedPlayerRow(byID[p.Player], p)
	}
	return out, true
}

func observedPlayerRow(p PlayerObservation, terms SquadPlayer) SquadPlayer {
	return SquadPlayer{
		Player: p.Player, Name: p.Name, Nationality: p.Nationality, Age: p.Age,
		Position: p.Position, Attributes: p.Attributes, Overall: p.Overall,
		Condition: p.Condition, DaysOut: p.DaysOut, Contract: p.Contract, Demand: p.Demand,
		Value: terms.Value, Payoff: terms.Payoff, Listed: terms.Listed,
	}
}

// ResultScore is a fixture's official score, formatted for a result list.
// It carries no replay detail; a report query supplies what is available.
type ResultScore struct {
	Fixture ids.FixtureID
	Text    string
}

// AutomaticBatch is the presentation of one batch Continue played while
// advancing to a target or to the manager's next fixture.
type AutomaticBatch struct {
	Summary string
	Scores  []ResultScore
}

// AutomaticResults formats every auto-resolved batch in a Continue result,
// oldest first, using the same wording for both manager clients. It reads
// only the detached result and Calendar; no history is stored or rebuilt.
func (w *World) AutomaticResults(result ContinueResult) []AutomaticBatch {
	var batches []BatchResolved
	switch r := result.(type) {
	case ReachedTarget:
		batches = r.Resolved
	case FixtureRoundReady:
		batches = r.Resolved
	case SeasonReviewReady:
		batches = r.Resolved
	}
	count := func(n int, noun, plural string) string {
		if n != 1 {
			noun = plural
		}
		return fmt.Sprintf("%d %s", n, noun)
	}
	var out []AutomaticBatch
	for _, b := range batches {
		row := AutomaticBatch{Summary: fmt.Sprintf("Automatically played: %s; %s, %s.",
			w.Calendar().Format(b.At), count(len(b.Rounds), "round", "rounds"), count(len(b.Matches), "match", "matches"))}
		for _, m := range b.Matches {
			text := fmt.Sprintf("%s %d-%d %s", m.Home.ClubName, m.Score[0], m.Score[1], m.Away.ClubName)
			if m.Shootout != [2]uint16{} {
				text += fmt.Sprintf(" (%d-%d on penalties)", m.Shootout[0], m.Shootout[1])
			}
			row.Scores = append(row.Scores, ResultScore{Fixture: m.Fixture, Text: text})
		}
		out = append(out, row)
	}
	return out
}

// CareerTotals returns the appearances and goals summed over spells, for a
// total under a career of more than one spell. Read-only.
func CareerTotals(spells []CareerSpell) (appearances, goals int) {
	for _, s := range spells {
		appearances += int(s.Appearances)
		goals += int(s.Goals)
	}
	return appearances, goals
}

// LivePreview is the manager's match played on, without recording anything,
// from its latest stop to a minute.
type LivePreview struct {
	Live   LiveMatch // as the match would stand at the minute
	Played uint16    // the minute of the latest recorded stop: 0 before kickoff
	// Frames are the positional frames from the latest stop to the minute,
	// when asked for and the engine draws them.
	Frames []matches.Frame
}

// PreviewLive plays the manager's match in fixture on from its latest stop
// (kickoff when it has none) to toMinute, past half time if need be, with no
// further decisions, and records nothing: it changes no state, revision or
// command log. Engines play a match the same however its minutes are split
// into advances, so recording a stop at any minute up to toMinute later
// (PlayMatch) shows exactly what the preview showed up to it. Frames are
// kept as LiveFrames keeps them, one in each everyMillis, when frames is set
// and the engine has PositionalFrames. toMinute may equal the latest stop.
func (w *World) PreviewLive(fixture ids.FixtureID, toMinute uint16, frames bool, everyMillis uint32) (LivePreview, error) {
	if _, _, err := w.pendingUserFixture(fixture); err != nil {
		return LivePreview{}, err
	}
	var stops []LiveStop
	if w.live != nil {
		if w.live.fixture != fixture {
			return LivePreview{}, fmt.Errorf("%w: fixture %d", ErrMatchInProgress, w.live.fixture)
		}
		stops = w.live.stops
	}
	p, err := w.livePlan(fixture)
	if err != nil {
		return LivePreview{}, err
	}
	r, err := w.replay(p, stops)
	if err != nil {
		return LivePreview{}, err
	}
	played := r.step.Position.Minute
	if toMinute < played || toMinute > matches.RegulationMinutes {
		return LivePreview{}, fmt.Errorf("%w: preview to minute %d from %d", ErrInvalidCommand, toMinute, played)
	}
	frames = frames && w.engine.Capabilities().PositionalFrames
	var all []matches.Frame
	for first := true; first || (r.step.Position.Minute < toMinute && r.step.Status != matches.MatchFinished); first = false {
		if err := r.session.Advance(matches.AdvanceRequest{ToMinute: toMinute, Frames: frames}, &r.step); err != nil {
			return LivePreview{}, fmt.Errorf("app: preview fixture %d: %w", fixture, err)
		}
		r.events = append(r.events, r.step.Events...)
		all = append(all, r.step.Frames...)
	}
	out := LivePreview{Live: w.view(r), Played: played}
	for _, f := range all {
		if everyMillis <= 1 || len(out.Frames) == 0 || f.Millis/everyMillis != out.Frames[len(out.Frames)-1].Millis/everyMillis {
			out.Frames = append(out.Frames, f)
		}
	}
	return out, nil
}

// ClubSeason is how a club fared in one league season or cup edition it
// entered: a league's final place and record, or the stage a cup run ended
// at. Season with a league is in progress until Complete.
type ClubSeason struct {
	Season   competitions.SeasonRef
	Name     string
	League   bool // false: a cup edition
	Complete bool
	// League: the club's row of the table (Standing.Rank is the place) and
	// where it went next season.
	Standing  competitions.Standing
	Promoted  bool
	Relegated bool
	// Cup: the name of the last round the club played in ("quarter-final")
	// and whether it won the edition.
	Stage    string
	Champion bool
}

// ClubSeasons lists the league seasons and cup editions a club has entered,
// oldest first, from the retained seasons and their results. Promotion
// play-offs are not listed. Read-only; nothing is stored.
func (w *World) ClubSeasons(club ids.ClubID) []ClubSeason {
	var out []ClubSeason
	for _, h := range w.History() {
		if _, ok := w.Playoff(h.Season); ok {
			continue
		}
		rec := ClubSeason{Season: h.Season, Name: h.CompetitionName, Complete: h.Complete}
		if h.Format == competitions.FormatKnockout {
			c, ok := w.Cup(h.Season)
			if !ok {
				continue
			}
			for _, r := range c.Rounds {
				for _, t := range r.Ties {
					if t.Home.Club == club || t.Away.Club == club {
						rec.Stage = r.Name
					}
				}
			}
			if rec.Stage == "" {
				continue
			}
			rec.Champion = c.Champion != nil && c.Champion.Club == club
			out = append(out, rec)
			continue
		}
		t, ok := w.Table(h.Season)
		if !ok {
			continue
		}
		for _, row := range t.Rows {
			if row.Label.Club == club {
				rec.League, rec.Standing = true, row.Standing
				rec.Champion = h.Complete && row.Rank == 1
				rec.Promoted, rec.Relegated = w.SeasonMove(h.Season, row.Rank)
				out = append(out, rec)
			}
		}
	}
	return out
}

// ClubMoveKind says how a player came to or left a club.
type ClubMoveKind uint8

const (
	MoveBought   ClubMoveKind = 1 // transferred in for a fee
	MoveSold     ClubMoveKind = 2 // transferred out for a fee
	MoveSigned   ClubMoveKind = 3 // free agent signed
	MoveYouth    ClubMoveKind = 4 // joined from the youth ranks
	MoveReleased ClubMoveKind = 5 // released from his contract
	MoveExpired  ClubMoveKind = 6 // contract ran out
	MoveRetired  ClubMoveKind = 7 // retired while at the club
)

// ClubMove is one change of a club's players. Other is the other club of a
// transfer; Fee the transfer fee, or what a release cost the club.
type ClubMove struct {
	At         sim.GameInstant
	Kind       ClubMoveKind
	Player     ids.PlayerID
	PlayerName string
	Other      ids.ClubID
	OtherName  string
	Fee        money.Money
}

// ClubMoves lists every player who joined or left a club since the career
// began, oldest first, read from the retained event journal: transfers, free
// signings, youth intake, releases, expiries and retirements. Renewals are
// not moves. Read-only.
func (w *World) ClubMoves(club ids.ClubID) []ClubMove {
	var out []ClubMove
	add := func(at sim.GameInstant, kind ClubMoveKind, player ids.PlayerID, other ids.ClubID, fee money.Money) {
		m := ClubMove{At: at, Kind: kind, Player: player, Other: other, Fee: fee}
		m.PlayerName, _ = w.PlayerName(player)
		if other != 0 {
			l, _ := w.ClubLabel(other)
			m.OtherName = l.ClubName
		}
		out = append(out, m)
	}
	for _, e := range w.journal {
		switch {
		case e.TransferCompleted != nil && e.TransferCompleted.Buyer == club:
			add(e.OccurredAt, MoveBought, e.TransferCompleted.Player, e.TransferCompleted.Seller, e.TransferCompleted.Fee)
		case e.TransferCompleted != nil && e.TransferCompleted.Seller == club:
			add(e.OccurredAt, MoveSold, e.TransferCompleted.Player, e.TransferCompleted.Buyer, e.TransferCompleted.Fee)
		case e.PlayerSigned != nil && e.PlayerSigned.Club == club:
			add(e.OccurredAt, MoveSigned, e.PlayerSigned.Player, 0, 0)
		case e.YouthJoined != nil && e.YouthJoined.Club == club:
			add(e.OccurredAt, MoveYouth, e.YouthJoined.Player, 0, 0)
		case e.PlayerReleased != nil && e.PlayerReleased.Club == club:
			add(e.OccurredAt, MoveReleased, e.PlayerReleased.Player, 0, e.PlayerReleased.Compensation)
		case e.ContractExpired != nil && e.ContractExpired.Club == club:
			add(e.OccurredAt, MoveExpired, e.ContractExpired.Player, 0, 0)
		case e.PlayerRetired != nil && e.PlayerRetired.Club == club:
			add(e.OccurredAt, MoveRetired, e.PlayerRetired.Player, 0, 0)
		}
	}
	return out
}
