package app

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
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
	var count [matches.Forward + 1]int
	for _, s := range l.Starters {
		if s.Role.Valid() {
			count[s.Role]++
		}
	}
	return fmt.Sprintf("%d-%d-%d", count[matches.Defender], count[matches.Midfielder], count[matches.Forward])
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

// SeasonMove says whether finishing a league season at the position gets a
// club promoted to the division above or relegated to the division below.
// Read-only.
func (w *World) SeasonMove(ref competitions.SeasonRef, position int) (promoted, relegated bool) {
	up, down := w.PromotionPlaces(ref.Competition)
	if position < 1 {
		return false, false
	}
	t, ok := w.Table(ref)
	return position <= up, ok && down > 0 && position > len(t.Rows)-down
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
// An item the manager must act on before play can go on has Now set. It
// combines existing queries and changes nothing.
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
	squad, _ := w.Squad(w.userClub)
	for _, p := range squad {
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
