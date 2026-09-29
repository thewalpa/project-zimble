package main

import (
	"errors"
	"fmt"

	"github.com/thewalpa/project-zimble/internal/app"
	"github.com/thewalpa/project-zimble/internal/core/ids"
)

func (s *session) comparePlayers(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: compare PLAYER PLAYER")
	}
	leftID, err := parsePlayer(args[0])
	if err != nil {
		return err
	}
	rightID, err := parsePlayer(args[1])
	if err != nil {
		return err
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
