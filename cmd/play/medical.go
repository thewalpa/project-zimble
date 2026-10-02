package main

import (
	"errors"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
)

// medical shows the squad's injured with their forecast return, the tired,
// and each position's fit players against its roster quota.
func (s *session) medical(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: medical")
	}
	m, ok := s.w.SquadMedical(s.club())
	if !ok {
		return errors.New("your club has no squad")
	}
	cal := s.w.Calendar()
	if !m.CanField {
		s.printf("\n%s\n", present.MedicalEmergencyNote)
	}
	s.printf("\n%-3s %5s %8s %4s %7s  %s\n", "POS", "SQUAD", "INJURED", "FIT", "ROSTER", "")
	for _, p := range m.Positions {
		s.printf("%-3s %5d %8d %4d %4d/%-2d  %s\n", p.Position, p.Squad, p.Injured, p.Fit, p.Count, p.Min, present.Supply(p))
	}
	if len(m.Injured) == 0 {
		s.printf("\nNo injuries.\n")
	} else {
		s.printf("\n%4s  %-3s %-24s %4s  %-48s %s\n", "ID", "POS", "INJURED", "COND", "INJURY", "LINEUP")
		for _, p := range m.Injured {
			c := present.Medical(cal, m.CanField, p)
			s.printf("%4d  %-3s %-24s %3d%%  %-48s %s\n", p.Player, p.Position, p.Name, p.Condition, c.Injury, c.Eligibility)
		}
	}
	if len(m.Tired) > 0 {
		s.printf("\n%4s  %-3s %-24s %4s\n", "ID", "POS", "TIRED", "COND")
		for _, p := range m.Tired {
			s.printf("%4d  %-3s %-24s %3d%%\n", p.Player, p.Position, p.Name, p.Condition)
		}
	}
	s.printf("\n%d fit. %s\n", m.Fit, present.MedicalForecastNote)
	return nil
}
