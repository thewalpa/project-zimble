package app

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
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

// LineupSourceLabel returns the human-readable description of where ml came from,
// e.g. "Your saved lineup for this match", "Carried over from the last match (vs Hollowick Town)",
// or "The assistant's suggestion".
func (w *World) LineupSourceLabel(ml MatchdayLineup) string {
	switch ml.Source {
	case LineupFromSubmission:
		return "Your saved lineup for this match"
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
