package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func (s *session) comparePlayers(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: compare PLAYER PLAYER")
	}
	leftID, err := parsePlayer(args[0])
	if err != nil {
		leftID, err = s.ownPlayerByName(args[0])
		if err != nil {
			return err
		}
	}
	rightID, err := parsePlayer(args[1])
	if err != nil {
		rightID, err = s.ownPlayerByName(args[1])
		if err != nil {
			return err
		}
	}
	left, ok := s.w.PlayerProfile(leftID)
	if !ok {
		return fmt.Errorf("no player %d", leftID)
	}
	right, ok := s.w.PlayerProfile(rightID)
	if !ok {
		return fmt.Errorf("no player %d", rightID)
	}
	label := func(p app.PlayerProfile) string { return fmt.Sprintf("%s (#%d)", p.Name, p.Player) }
	status := func(p app.PlayerProfile) string {
		if p.Retired {
			return "retired"
		}
		if p.Club == ids.ClubID(0) {
			return "free agent"
		}
		return p.ClubName
	}
	weekly := func(p app.PlayerProfile) string {
		if p.Retired || p.Club == 0 {
			return "n/a"
		}
		return p.Contract.WeeklyWage.String()
	}
	asking := func(p app.PlayerProfile) string {
		if p.Retired || p.Club == 0 {
			return "n/a"
		}
		return p.Value.String()
	}
	demand := func(p app.PlayerProfile) string {
		if p.Retired || p.Club != 0 {
			return "n/a"
		}
		return p.Demand.String()
	}
	condition := func(p app.PlayerProfile) string {
		if p.Retired {
			return "n/a"
		}
		return fitness(p.SquadPlayer)
	}
	s.printf("\n%-20s %-28s | %-28s\n", "", label(left), label(right))
	s.printf("%-20s %-28s | %-28s\n", "Club", status(left), status(right))
	s.printf("%-20s %-28s | %-28s\n", "Position", left.Position, right.Position)
	s.printf("%-20s %-28d | %-28d\n", "Age", left.Age, right.Age)
	s.printf("%-20s %-28d | %-28d\n", "Overall", left.Overall, right.Overall)
	s.printf("%-20s %-28s | %-28s\n", "Condition", condition(left), condition(right))
	s.printf("%-20s %-28s | %-28s\n", "Weekly wage", weekly(left), weekly(right))
	s.printf("%-20s %-28s | %-28s\n", "Asking price", asking(left), asking(right))
	s.printf("%-20s %-28s | %-28s\n", "Wage demand", demand(left), demand(right))
	for i, name := range []string{"GK", "DEF", "PAS", "FIN", "PAC", "STA", "DRI", "HEA", "STR", "ACC", "PSN"} {
		s.printf("%-20s %-28d | %-28d\n", name, left.Attributes[i], right.Attributes[i])
	}
	return nil
}

func (s *session) ownPlayerByName(name string) (ids.PlayerID, error) {
	players, _ := s.w.Squad(s.club())
	find := func(match func(app.SquadPlayer) bool) (ids.PlayerID, bool) {
		var found ids.PlayerID
		for _, player := range players {
			if !match(player) {
				continue
			}
			if found != 0 {
				return 0, false
			}
			found = player.Player
		}
		return found, true
	}
	needle := strings.TrimSpace(name)
	if found, unique := find(func(player app.SquadPlayer) bool {
		return strings.EqualFold(strings.TrimSpace(player.Name), needle)
	}); found != 0 {
		return found, nil
	} else if !unique {
		return 0, fmt.Errorf("%q matches more than one player in your squad; use a player ID", name)
	}
	if found, unique := find(func(player app.SquadPlayer) bool {
		parts := strings.Fields(player.Name)
		return len(parts) > 0 && strings.EqualFold(parts[0], needle)
	}); found != 0 {
		return found, nil
	} else if !unique {
		return 0, fmt.Errorf("%q matches more than one player in your squad; use a full name or player ID", name)
	}
	return 0, fmt.Errorf("%q is not a player ID or a name in your squad", name)
}

// compareArgs preserves quoted full names while splitting the two compare inputs.
func compareArgs(raw string) ([]string, error) {
	var args []string
	for len(raw) > 0 {
		raw = strings.TrimLeft(raw, " \t")
		if raw == "" {
			break
		}
		quote := byte(0)
		if raw[0] == '\'' || raw[0] == '"' {
			quote, raw = raw[0], raw[1:]
		}
		var token strings.Builder
		for len(raw) > 0 {
			if quote != 0 && raw[0] == quote {
				raw = raw[1:]
				quote = 0
				break
			}
			if quote == 0 && (raw[0] == ' ' || raw[0] == '\t') {
				break
			}
			token.WriteByte(raw[0])
			raw = raw[1:]
		}
		if quote != 0 {
			return nil, errors.New("unterminated quoted player name")
		}
		args = append(args, token.String())
	}
	return args, nil
}
