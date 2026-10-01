package app

import (
	"fmt"
	"slices"

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
// official result when Played is true; Shootout the penalties of a knockout
// match level after regulation.
type FixtureLine struct {
	ID       ids.FixtureID
	Home     TeamLabel
	Away     TeamLabel
	Played   bool
	Score    [2]uint16
	Shootout [2]uint16
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
		r.Fixtures = append(r.Fixtures, w.fixtureLine(f))
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

// Tables returns each league's current-season standings, derived from
// official results, in competition ID order. Read-only. Table gives any
// season's.
func (w *World) Tables() []Table {
	out := make([]Table, 0, len(w.leagues))
	for _, l := range w.leagues {
		out = append(out, w.table(l.def.Name, l.season))
	}
	return out
}

func (w *World) table(name string, ref competitions.SeasonRef) Table {
	t := Table{
		Competition: ref.Competition, CompetitionName: name, Season: ref.Season,
		Complete: w.competitions.SeasonCompleted(ref),
	}
	for _, r := range w.competitions.Rounds(ref) {
		t.Rounds++
		if r.Status == competitions.RoundCompleted {
			t.RoundsCompleted++
		}
	}
	for _, st := range w.competitions.Standings(ref) {
		t.Rows = append(t.Rows, TableRow{Standing: st, Label: w.teamLabel(st.Team)})
	}
	return t
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

// ClubLabel returns a club's senior team label, or false if the club is unknown.
// Read-only.
func (w *World) ClubLabel(club ids.ClubID) (TeamLabel, bool) {
	team, ok := w.registry.SeniorTeam(club)
	if !ok {
		return TeamLabel{}, false
	}
	return w.teamLabel(team), true
}

func (w *World) fixtureLine(f competitions.Fixture) FixtureLine {
	line := FixtureLine{ID: f.ID, Home: w.teamLabel(f.Home), Away: w.teamLabel(f.Away)}
	if res, ok := w.competitions.Result(f.ID); ok {
		line.Played, line.Score = true, [2]uint16{res.HomeGoals, res.AwayGoals}
		line.Shootout = [2]uint16{res.HomePenalties, res.AwayPenalties}
	}
	return line
}

// RoundName names a round for display, in lower case: "round N" for a
// league, and "final", "semi-final", "quarter-final" or "round N" for a cup,
// counted back from its last round.
func (w *World) RoundName(ref competitions.RoundRef) string {
	if format, _ := w.competitions.Format(ref.Season); format == competitions.FormatKnockout {
		switch len(w.competitions.Rounds(ref.Season)) - int(ref.Round) {
		case 0:
			return "final"
		case 1:
			return "semi-final"
		case 2:
			return "quarter-final"
		}
	}
	return fmt.Sprintf("round %d", ref.Round)
}

// cupStage describes how far the team ranked at position (1-based) got in a
// completed cup edition (competitions.Exits): "winner", or the name of the
// round it lost in. Empty for a position the edition does not have.
func (w *World) cupStage(ref competitions.SeasonRef, position int) string {
	exits, ok := w.competitions.Exits(ref)
	if !ok || position < 1 || position > len(exits) {
		return ""
	}
	if e := exits[position-1]; e.Stage > 0 {
		return w.RoundName(competitions.RoundRef{Season: ref, Round: e.Round})
	}
	return "winner"
}

// FixtureInfo is one fixture of any competition, with display names.
type FixtureInfo struct {
	FixtureLine
	Round           competitions.RoundRef
	CompetitionName string
	Cup             bool
	Playoff         bool
	RoundName       string
	Kickoff         sim.GameInstant
}

// FixtureInfo describes a fixture of any league season, cup edition or
// promotion play-off. Read-only.
func (w *World) FixtureInfo(id ids.FixtureID) (FixtureInfo, bool) {
	f, ok := w.competitions.Fixture(id)
	if !ok {
		return FixtureInfo{}, false
	}
	ref := competitions.RoundRef{Season: f.Season, Round: f.Round}
	_, cup := w.cupIndex(f.Season.Competition)
	_, playoff := w.playoffLink(f.Season.Competition)
	return FixtureInfo{
		FixtureLine: w.fixtureLine(f), Round: ref, CompetitionName: w.competitionName(f.Season.Competition),
		Cup: cup, Playoff: playoff, RoundName: w.RoundName(ref), Kickoff: f.Kickoff,
	}, true
}

// CupRound is one round of a cup edition or a promotion play-off's ties.
// Ties are empty until the previous round is complete.
type CupRound struct {
	Round   competitions.Round
	Name    string // see RoundName
	Kickoff sim.GameInstant
	Status  competitions.RoundStatus
	Ties    []FixtureLine
}

// CupEdition is a derived view of one cup edition's bracket.
type CupEdition struct {
	Competition ids.CompetitionID
	Name        string
	Edition     competitions.Season
	Entrants    []TeamLabel // bracket order
	Rounds      []CupRound  // ascending
	Complete    bool
	Champion    *TeamLabel
}

// Cups returns the latest edition of each cup that has one, in competition
// ID order. Read-only.
func (w *World) Cups() []CupEdition {
	var out []CupEdition
	for _, c := range w.cups {
		if e := w.latestEdition(c.ID); e > 0 {
			v, _ := w.Cup(competitions.SeasonRef{Competition: c.ID, Season: e})
			out = append(out, v)
		}
	}
	return out
}

// CupQualifier is a cup a league qualifies teams for: the top Places of the
// league's final table play in the cup's next edition.
type CupQualifier struct {
	Cup    ids.CompetitionID
	Name   string
	Places int
}

// CupQualifiers returns the cups a league qualifies teams for, in cup ID
// order: none for a league that sends nobody or an unknown competition.
// Read-only.
func (w *World) CupQualifiers(league ids.CompetitionID) []CupQualifier {
	var out []CupQualifier
	for _, c := range w.cups {
		for _, q := range c.Qualifiers {
			if q.League == league {
				out = append(out, CupQualifier{Cup: c.ID, Name: c.Name, Places: q.Places})
			}
		}
	}
	return out
}

// Cup returns any edition of a cup. Read-only.
func (w *World) Cup(ref competitions.SeasonRef) (CupEdition, bool) {
	ci, ok := w.cupIndex(ref.Competition)
	entrants, exists := w.competitions.Entrants(ref)
	if !ok || !exists {
		return CupEdition{}, false
	}
	v := CupEdition{Competition: ref.Competition, Name: w.cups[ci].Name, Edition: ref.Season, Complete: w.competitions.SeasonCompleted(ref)}
	for _, t := range entrants {
		v.Entrants = append(v.Entrants, w.teamLabel(t))
	}
	for _, r := range w.competitions.Rounds(ref) {
		cr := CupRound{Round: r.Ref.Round, Name: w.RoundName(r.Ref), Kickoff: r.Kickoff, Status: r.Status}
		for _, id := range r.Fixtures {
			f, _ := w.competitions.Fixture(id)
			cr.Ties = append(cr.Ties, w.fixtureLine(f))
		}
		v.Rounds = append(v.Rounds, cr)
	}
	if champion, ok := w.competitions.Champion(ref); ok {
		label := w.teamLabel(champion)
		v.Champion = &label
	}
	return v, true
}

// PlayoffEdition is a derived view of one promotion play-off's ties (see
// competitions.FormatTies): the placements' tie order, no champion — the
// winners move up, the losers stay down.
type PlayoffEdition struct {
	Competition ids.CompetitionID
	Name        string
	Edition     competitions.Season
	Entrants    []TeamLabel // tie order: competitions.PlayoffPairings
	Rounds      []CupRound  // one
	Complete    bool
}

// Playoffs returns the latest edition of each promotion play-off that has
// one, in competition ID order. Read-only.
func (w *World) Playoffs() []PlayoffEdition {
	links := w.movementLinks()
	comps := make([]ids.CompetitionID, 0, len(links))
	for comp := range competitions.PlayoffCompetitions(links) {
		comps = append(comps, comp)
	}
	slices.Sort(comps)
	var out []PlayoffEdition
	for _, comp := range comps {
		if e := w.latestEdition(comp); e > 0 {
			v, _ := w.Playoff(competitions.SeasonRef{Competition: comp, Season: e})
			out = append(out, v)
		}
	}
	return out
}

// Playoff returns any edition of a promotion play-off. Read-only.
func (w *World) Playoff(ref competitions.SeasonRef) (PlayoffEdition, bool) {
	if _, ok := w.playoffLink(ref.Competition); !ok {
		return PlayoffEdition{}, false
	}
	entrants, exists := w.competitions.Entrants(ref)
	if !exists {
		return PlayoffEdition{}, false
	}
	v := PlayoffEdition{
		Competition: ref.Competition, Name: w.competitionName(ref.Competition), Edition: ref.Season,
		Complete: w.competitions.SeasonCompleted(ref),
	}
	for _, t := range entrants {
		v.Entrants = append(v.Entrants, w.teamLabel(t))
	}
	for _, r := range w.competitions.Rounds(ref) {
		cr := CupRound{Round: r.Ref.Round, Name: w.RoundName(r.Ref), Kickoff: r.Kickoff, Status: r.Status}
		for _, id := range r.Fixtures {
			f, _ := w.competitions.Fixture(id)
			cr.Ties = append(cr.Ties, w.fixtureLine(f))
		}
		v.Rounds = append(v.Rounds, cr)
	}
	return v, true
}
