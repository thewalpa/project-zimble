package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/thewalpa/project-zimble/internal/app"
)

// showClub shows another club: the lineup it would field if it played today
// (a forecast, since squads change by kickoff) and its senior players.
func (s *session) showClub(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: club CLUB (a club ID or short name; see table)")
	}
	var found *app.ClubSummary
	rows := s.w.Summary().ClubRows
	for i, c := range rows {
		if n, err := strconv.ParseUint(args[0], 10, 64); (err == nil && uint64(c.ID) == n) || strings.EqualFold(c.ShortName, args[0]) {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("%q is not a club (see table)", args[0])
	}
	if found.ID == s.club() {
		return errors.New("that is your club: type squad, lineup or teamplan")
	}
	s.printf("\n%s (%s), %s: %d players, average %d.\n", found.Name, found.ShortName, found.Nation, found.Players, found.AverageOverall)
	squad := s.squadOf(found.ID)
	if l, err := s.w.ProbableLineup(found.SeniorTeam); err == nil {
		s.printf("Probable lineup if it played today (the squad changes by kickoff):\n")
		s.showPitch(l, squad, "")
	}
	players, _ := s.w.ObservedSquad(s.club(), found.ID)
	s.printf("%4s  %-3s %-24s %-10s %3s %5s %-12s\n", "ID", "POS", "NAME", "NATION", "AGE", "OVR", "COND")
	for _, p := range players {
		s.printf("%4d  %-3s %-24s %-10s %3d %5d %-12s\n", p.Player, p.Position, p.Name, p.Nationality, p.Age, p.Overall, fitness(p))
	}
	return nil
}
