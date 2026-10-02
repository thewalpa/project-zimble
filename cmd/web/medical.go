package main

import (
	"errors"
	"net/http"

	"github.com/thewalpa/project-zimble/cmd/internal/present"
	"github.com/thewalpa/project-zimble/internal/app"
)

type medicalRow struct {
	app.MedicalPlayer
	present.MedicalCells
}

type medicalPosition struct {
	app.PositionAvailability
	Supply string // "short", "critical: ..." or ""
}

type medicalView struct {
	Injured   []medicalRow // the longest layoff first
	Tired     []app.MedicalPlayer
	Positions []medicalPosition
	Fit       int
	Emergency bool
	Forecast  string
	Note      string
}

// medical shows the manager's squad's injured with their forecast return, the
// tired, and each position's supply, from app's SquadMedical.
func (s *server) medical(*http.Request) (string, any, error) {
	m, ok := s.w.SquadMedical(s.club())
	if !ok {
		return "", nil, errors.New("your club has no squad")
	}
	v := medicalView{Tired: m.Tired, Fit: m.Fit, Emergency: !m.CanField, Forecast: present.MedicalForecastNote, Note: present.MedicalEmergencyNote}
	for _, p := range m.Positions {
		v.Positions = append(v.Positions, medicalPosition{PositionAvailability: p, Supply: present.Supply(p)})
	}
	for _, p := range m.Injured {
		v.Injured = append(v.Injured, medicalRow{MedicalPlayer: p, MedicalCells: present.Medical(s.w.Calendar(), m.CanField, p)})
	}
	return "medical", v, nil
}
