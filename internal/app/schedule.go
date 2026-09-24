package app

import (
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// TeamLabel names a team by its club.
type TeamLabel struct {
	Team      ids.TeamID
	Club      ids.ClubID
	ClubName  string
	ShortName string
}

// FixtureLine is one fixture with display names resolved. Score is the
// official result when Played is true.
type FixtureLine struct {
	ID     ids.FixtureID
	Home   TeamLabel
	Away   TeamLabel
	Played bool
	Score  [2]uint16
}

// RoundSchedule holds one round's kickoff, status and fixtures in fixture ID
// order.
type RoundSchedule struct {
	Round    competitions.Round
	Kickoff  sim.GameInstant
	Civil    sim.CivilTime
	Status   competitions.RoundStatus
	Fixtures []FixtureLine
}

// Schedule is a derived view of one competition season's fixtures.
type Schedule struct {
	Competition     ids.CompetitionID
	CompetitionName string
	Season          competitions.Season
	ScheduleVersion int
	Entrants        int
	Rounds          []RoundSchedule // ascending round
}

// Schedules returns each league's current season calendar, fixtures and
// official results, in competition ID order. It is a read-only query built
// from competitions state; it never inspects or consumes scheduled tasks.
func (w *World) Schedules() []Schedule {
	out := make([]Schedule, 0, len(w.leagues))
	for _, l := range w.leagues {
		out = append(out, w.schedule(l))
	}
	return out
}

func (w *World) schedule(l leagueEntry) Schedule {
	entrants, _ := w.competitions.Entrants(l.season)
	s := Schedule{
		Competition:     l.def.ID,
		CompetitionName: l.def.Name,
		Season:          l.season.Season,
		ScheduleVersion: competitions.ScheduleVersion,
		Entrants:        len(entrants),
	}
	for _, r := range w.competitions.Rounds(l.season) {
		civil, _ := w.calendar.Civil(r.Kickoff) // kickoffs are validated instants
		s.Rounds = append(s.Rounds, RoundSchedule{Round: r.Ref.Round, Kickoff: r.Kickoff, Civil: civil, Status: r.Status})
	}
	for _, f := range w.competitions.Fixtures(l.season) {
		r := &s.Rounds[f.Round-1]
		line := FixtureLine{ID: f.ID, Home: w.teamLabel(f.Home), Away: w.teamLabel(f.Away)}
		if res, ok := w.competitions.Result(f.ID); ok {
			line.Played, line.Score = true, [2]uint16{res.HomeGoals, res.AwayGoals}
		}
		r.Fixtures = append(r.Fixtures, line)
	}
	return s
}

// TableRow is one derived standings row with display names.
type TableRow struct {
	competitions.Standing
	Label TeamLabel
}

// Table is a league's derived standings.
type Table struct {
	Competition     ids.CompetitionID
	CompetitionName string
	Season          competitions.Season
	RoundsCompleted int
	Rounds          int
	Complete        bool       // every round has official results
	Rows            []TableRow // ranked
}

// Tables returns each league's standings, derived from official results, in
// competition ID order. Read-only.
func (w *World) Tables() []Table {
	out := make([]Table, 0, len(w.leagues))
	for _, l := range w.leagues {
		t := Table{
			Competition: l.def.ID, CompetitionName: l.def.Name, Season: l.season.Season,
			Complete: w.competitions.SeasonCompleted(l.season),
		}
		for _, r := range w.competitions.Rounds(l.season) {
			t.Rounds++
			if r.Status == competitions.RoundCompleted {
				t.RoundsCompleted++
			}
		}
		for _, st := range w.competitions.Standings(l.season) {
			t.Rows = append(t.Rows, TableRow{Standing: st, Label: w.teamLabel(st.Team)})
		}
		out = append(out, t)
	}
	return out
}

func (w *World) teamLabel(id ids.TeamID) TeamLabel {
	l := TeamLabel{Team: id}
	if t, ok := w.registry.Team(id); ok {
		l.Club = t.Club
		if c, ok := w.registry.Club(t.Club); ok {
			l.ClubName, l.ShortName = c.Name, c.ShortName
		}
	}
	return l
}
