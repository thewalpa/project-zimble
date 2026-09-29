package app

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/events"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
)

// Injuries. A match can hurt any player who took part in it (medical.Roll:
// more minutes and less condition make it likelier), for a number of recovery
// days. Injured players are not selected: the AI never picks them and the
// manager's lineups may not name them, and a carried-over lineup refills
// their places. An injury does not stop a match already being played.
//
// A club must always be able to field a team, so injuries never leave a squad
// unable to: a rolled injury that would leave fewer than a legal lineup's
// players (a goalkeeper and ten outfield players) is dropped, and a squad
// that is already short (sales, releases and retirements do not look at
// injuries) fields its injured players too.

// canField reports whether players include a legal starting eleven: a natural
// goalkeeper, who alone may start in goal, and ten outfield players.
func (w *World) canField(squad []ids.PlayerID) bool {
	keepers, outfield := 0, 0
	for _, id := range squad {
		if p, ok := w.players.Profile(id); ok && p.Position == players.Goalkeeper {
			keepers++
		} else {
			outfield++
		}
	}
	return keepers >= 1 && outfield >= matches.StartersPerTeam-1
}

// availableSquad returns a team's players in ascending ID order who can be
// selected: its squad without the injured, or the whole squad when the rest
// cannot field a legal lineup. Read-only.
func (w *World) availableSquad(team ids.TeamID) []ids.PlayerID {
	squad := w.employment.Squad(team)
	fit := make([]ids.PlayerID, 0, len(squad))
	for _, id := range squad {
		if _, injured := w.medical.DaysOut(id); !injured {
			fit = append(fit, id)
		}
	}
	if !w.canField(fit) {
		return squad
	}
	return fit
}

// injuryRolls draws the injuries of every planned match from its outcome's
// participants, one stream per fixture, and drops those that would leave a
// team unable to field a lineup (see canField). Read-only.
func (w *World) injuryRolls(plan []plannedMatch, outcomes []matches.MatchOutcome) ([]medical.Injury, error) {
	var out []medical.Injury
	for i, p := range plan {
		var exposures []medical.Exposure
		team := map[ids.PlayerID]ids.TeamID{}
		for _, pt := range outcomes[i].Participants {
			exposures = append(exposures, medical.Exposure{Player: pt.Player, Minutes: pt.Minutes(), Stamina: p.stamina[pt.Player]})
			team[pt.Player] = p.fixture.Home
			if pt.Side == matches.Away {
				team[pt.Player] = p.fixture.Away
			}
		}
		rng := random.Derive(w.seed, "medical/injury", medical.Version, uint64(p.fixture.ID))
		rolled, err := w.medical.Roll(exposures, rng)
		if err != nil {
			return nil, fmt.Errorf("app: fixture %d injuries: %w", p.fixture.ID, err)
		}
		hurt := map[ids.TeamID]map[ids.PlayerID]bool{}
		for _, in := range rolled { // ascending player
			t := team[in.Player]
			if hurt[t] == nil {
				hurt[t] = map[ids.PlayerID]bool{}
			}
			hurt[t][in.Player] = true
			var fit []ids.PlayerID
			for _, id := range w.employment.Squad(t) {
				if _, injured := w.medical.DaysOut(id); !injured && !hurt[t][id] {
					fit = append(fit, id)
				}
			}
			if !w.canField(fit) {
				delete(hurt[t], in.Player)
				continue
			}
			out = append(out, in)
		}
	}
	return out, nil
}

// injuredEvents returns the events for the injuries a match plan starts, in
// player order.
func (w *World) injuredEvents(injuries []medical.Injury) []events.PlayerInjured {
	var out []events.PlayerInjured
	for _, in := range injuries {
		a, _ := w.employment.Assignment(in.Player) // a player in a match is employed
		out = append(out, events.PlayerInjured{Player: in.Player, Club: a.Club, Team: a.Team, Days: in.Days})
	}
	slices.SortFunc(out, func(a, b events.PlayerInjured) int { return cmp.Compare(a.Player, b.Player) })
	return out
}

// Injury returns how many recovery days an injured player still has to miss,
// and whether he is injured. Read-only.
func (w *World) Injury(player ids.PlayerID) (daysOut uint16, injured bool) {
	return w.medical.DaysOut(player)
}
