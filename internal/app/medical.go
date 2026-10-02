package app

import (
	"cmp"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/players"
)

// MedicalPlayer is one squad player the medical view singles out.
type MedicalPlayer struct {
	Player    ids.PlayerID
	Name      string
	Position  players.Position
	Overall   int
	Condition uint8 // 0..100
	DaysOut   uint16
	// FitFrom is the first instant an injured player is fit again if nothing
	// happens in between: the daily recovery task that takes off his last
	// day. It is a forecast, not a stored fact (a later match can injure him
	// again, a sale or release ends it). Zero for a fit player.
	FitFrom sim.GameInstant
}

// PositionAvailability is one position's supply against its roster quota.
type PositionAvailability struct {
	Position players.Position
	Squad    int // players held at the position
	Injured  int
	Fit      int // Squad - Injured
	Count    int // the quota's roster count
	Min      int // the quota's minimum
	// Short is Fit below the roster count: the position is thinner than the
	// club plans for. Critical is Fit below the minimum.
	Short, Critical bool
}

// SquadMedical is the read-only medical picture of a club's senior squad.
type SquadMedical struct {
	Club ids.ClubID
	AsOf sim.GameInstant
	// Injured lists the injured players, the longest layoff first (ID order
	// among equals). Tired lists the fit players whose condition is below
	// TiredCondition, the most tired first.
	Injured, Tired []MedicalPlayer
	// Positions follow the content's roster order.
	Positions []PositionAvailability
	Fit       int // players without an injury
	// CanField reports whether the fit players alone include a legal lineup.
	// When false, selection fields the injured too (see availableSquad).
	CanField bool
}

// SquadMedical returns the medical view of a club's senior squad, or false
// for an unknown club. It reads the same stores as the squad and lineup
// queries and changes nothing.
func (w *World) SquadMedical(club ids.ClubID) (SquadMedical, bool) {
	team, ok := w.registry.SeniorTeam(club)
	if !ok {
		return SquadMedical{}, false
	}
	out := SquadMedical{Club: club, AsOf: w.Now()}
	squad := w.employment.Squad(team)
	held := map[players.Position][2]int{} // position -> squad, injured
	var fit []ids.PlayerID
	for _, id := range squad {
		o := w.playerObservation(id)
		h := held[o.Position]
		h[0]++
		row := MedicalPlayer{
			Player: id, Name: o.Name, Position: o.Position, Overall: o.Overall,
			Condition: o.Condition, DaysOut: o.DaysOut,
		}
		switch {
		case o.DaysOut > 0:
			h[1]++
			row.FitFrom = w.fitFrom(o.DaysOut)
			out.Injured = append(out.Injured, row)
		default:
			fit = append(fit, id)
			if o.Condition < TiredCondition {
				out.Tired = append(out.Tired, row)
			}
		}
		held[o.Position] = h
	}
	slices.SortStableFunc(out.Injured, func(a, b MedicalPlayer) int { return cmp.Compare(b.DaysOut, a.DaysOut) })
	slices.SortStableFunc(out.Tired, func(a, b MedicalPlayer) int { return cmp.Compare(a.Condition, b.Condition) })
	for _, q := range w.defs.Roster {
		h := held[q.Position]
		fitHere := h[0] - h[1]
		out.Positions = append(out.Positions, PositionAvailability{
			Position: q.Position, Squad: h[0], Injured: h[1], Fit: fitHere, Count: q.Count, Min: q.Min,
			Short: fitHere < q.Count, Critical: fitHere < q.Min,
		})
	}
	out.Fit = len(fit)
	out.CanField = w.canField(fit)
	return out, true
}

// fitFrom is the instant the days-th recovery task from now runs: the first
// is the pending one, and each later one is a day on.
func (w *World) fitFrom(days uint16) sim.GameInstant {
	next := (w.Now()/sim.GameInstant(sim.Day) + 1) * sim.GameInstant(sim.Day)
	for _, t := range w.scheduler.Pending() {
		if t.Kind == taskRecovery {
			next = t.DueAt
			break
		}
	}
	return next + sim.GameInstant(days-1)*sim.GameInstant(sim.Day)
}
