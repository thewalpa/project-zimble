package app

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/selection"
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
