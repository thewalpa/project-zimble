package competitions

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// Points awarded per match. Prototype league rules; later part of the
// competition definition.
const (
	PointsForWin  = 3
	PointsForDraw = 1
)

// MaxGoals bounds one side's official score as a sanity check.
const MaxGoals = 99

// Score is a proposed official result for one fixture: the regulation
// score and, for a knockout tie level after regulation, the penalty
// shootout (zero otherwise).
type Score struct {
	Fixture       ids.FixtureID
	HomeGoals     uint16
	AwayGoals     uint16
	HomePenalties uint16
	AwayPenalties uint16
}

// Result is an official result. Competitions owns it; it is recorded once
// and never changed.
type Result struct {
	Fixture       ids.FixtureID
	Season        SeasonRef
	Round         Round
	Home          ids.TeamID
	Away          ids.TeamID
	HomeGoals     uint16
	AwayGoals     uint16
	HomePenalties uint16 // knockout ties level after regulation only
	AwayPenalties uint16
	RecordedAt    sim.GameInstant
}

// Winner returns the team that won: by goals, else by penalties. ok is
// false for a draw (league matches only).
func (r Result) Winner() (ids.TeamID, bool) {
	for _, p := range [][2]uint16{{r.HomeGoals, r.AwayGoals}, {r.HomePenalties, r.AwayPenalties}} {
		switch {
		case p[0] > p[1]:
			return r.Home, true
		case p[1] > p[0]:
			return r.Away, true
		}
	}
	return 0, false
}

type result struct {
	recorded   bool
	home, away uint16
	pens       [2]uint16 // home, away
	at         sim.GameInstant
}

// checkScore applies a format's result rules: a league result has no
// shootout; a knockout or ties result has one exactly when the goals are
// level, and it has a winner. Goals and penalties are at most MaxGoals.
func checkScore(format Format, fixture ids.FixtureID, goals, pens [2]uint16) error {
	level := goals[0] == goals[1]
	decided := format == FormatKnockout || format == FormatTies
	switch {
	case goals[0] > MaxGoals || goals[1] > MaxGoals || pens[0] > MaxGoals || pens[1] > MaxGoals:
		return fmt.Errorf("competitions: fixture %d score %v (penalties %v) exceeds %d", fixture, goals, pens, MaxGoals)
	case format == FormatLeague && pens != [2]uint16{}:
		return fmt.Errorf("competitions: league fixture %d has a shootout", fixture)
	case decided && level && pens[0] == pens[1]:
		return fmt.Errorf("competitions: %s fixture %d is level after penalties %v", format, fixture, pens)
	case decided && !level && pens != [2]uint16{}:
		return fmt.Errorf("competitions: %s fixture %d was won in regulation but has a shootout", format, fixture)
	}
	return nil
}

// CompleteRounds records official results for every fixture of the given
// rounds and marks them RoundCompleted, as one change. Every round must
// exist, appear once, await results and have kicked off at or before at.
// scores must contain exactly one entry for each of those rounds' fixtures
// and nothing else, each following its format's rules (see checkScore).
// Completing a knockout round that is not the last creates the next round's
// fixtures from its winners (see knockout.go), allocating fixture IDs in
// (competition, season) order. Everything is checked before anything
// changes: on error the store is unchanged.
func (s *Store) CompleteRounds(refs []RoundRef, scores []Score, at sim.GameInstant) error {
	if len(refs) == 0 {
		return fmt.Errorf("competitions: no rounds to complete")
	}
	refs = slices.Clone(refs)
	slices.SortFunc(refs, func(a, b RoundRef) int {
		return cmp.Or(cmp.Compare(a.Season.Competition, b.Season.Competition), cmp.Compare(a.Season.Season, b.Season.Season), cmp.Compare(a.Round, b.Round))
	})
	type loc struct{ season, index int }
	expected := map[ids.FixtureID]loc{}
	seenRound := map[RoundRef]bool{}
	var rounds []*roundState
	for _, ref := range refs {
		si, ok := s.seasonIdx[ref.Season]
		if !ok || ref.Round < 1 || int(ref.Round) > len(s.seasons[si].rounds) {
			return fmt.Errorf("competitions: unknown %s", ref)
		}
		if seenRound[ref] {
			return fmt.Errorf("competitions: %s listed twice", ref)
		}
		seenRound[ref] = true
		se := &s.seasons[si]
		st := &se.rounds[ref.Round-1]
		if st.status != RoundAwaitingResults {
			return fmt.Errorf("competitions: %s is %s, not awaiting results", ref, st.status)
		}
		if st.kickoff > at {
			return fmt.Errorf("competitions: %s kicks off at %d, after %d", ref, st.kickoff, at)
		}
		rounds = append(rounds, st)
		for i, f := range se.fixtures {
			if f.Round == ref.Round {
				if se.results[i].recorded {
					return fmt.Errorf("competitions: fixture %d already has a result", f.ID)
				}
				expected[f.ID] = loc{si, i}
			}
		}
	}
	if len(scores) != len(expected) {
		return fmt.Errorf("competitions: %d scores for %d fixtures", len(scores), len(expected))
	}
	seenFixture := map[ids.FixtureID]bool{}
	for _, sc := range scores {
		if _, ok := expected[sc.Fixture]; !ok {
			return fmt.Errorf("competitions: fixture %d is not in the rounds being completed", sc.Fixture)
		}
		if seenFixture[sc.Fixture] {
			return fmt.Errorf("competitions: fixture %d scored twice", sc.Fixture)
		}
		seenFixture[sc.Fixture] = true
		l := expected[sc.Fixture]
		if err := checkScore(s.seasons[l.season].format, sc.Fixture, [2]uint16{sc.HomeGoals, sc.AwayGoals}, [2]uint16{sc.HomePenalties, sc.AwayPenalties}); err != nil {
			return err
		}
	}
	// The next knockout rounds, from the new results, before anything is
	// stored.
	byFixture := map[ids.FixtureID]Score{}
	for _, sc := range scores {
		byFixture[sc.Fixture] = sc
	}
	type addition struct {
		season   int
		fixtures []Fixture
	}
	var next []addition
	lastFixture := s.lastFixture
	for _, ref := range refs {
		si := s.seasonIdx[ref.Season]
		se := &s.seasons[si]
		if se.format != FormatKnockout || int(ref.Round) == len(se.rounds) {
			continue
		}
		var winners []ids.TeamID
		for _, f := range se.fixtures {
			if f.Round == ref.Round {
				sc := byFixture[f.ID]
				w, _ := Result{Home: f.Home, Away: f.Away, HomeGoals: sc.HomeGoals, AwayGoals: sc.AwayGoals,
					HomePenalties: sc.HomePenalties, AwayPenalties: sc.AwayPenalties}.Winner()
				winners = append(winners, w)
			}
		}
		next = append(next, addition{season: si, fixtures: tiesOf(ref.Season, ref.Round+1, winners, se.rounds[ref.Round].kickoff, &lastFixture)})
	}

	for _, sc := range scores {
		l := expected[sc.Fixture]
		s.seasons[l.season].results[l.index] = result{recorded: true, home: sc.HomeGoals, away: sc.AwayGoals,
			pens: [2]uint16{sc.HomePenalties, sc.AwayPenalties}, at: at}
	}
	for _, st := range rounds {
		st.status = RoundCompleted
	}
	for _, a := range next {
		se := &s.seasons[a.season]
		for _, f := range a.fixtures {
			s.fixtureIdx[f.ID] = fixtureLoc{season: a.season, index: len(se.fixtures)}
			se.fixtures = append(se.fixtures, f)
			se.results = append(se.results, result{})
		}
	}
	s.lastFixture = lastFixture
	return nil
}

// Result returns a fixture's official result, if recorded.
func (s *Store) Result(id ids.FixtureID) (Result, bool) {
	loc, ok := s.fixtureIdx[id]
	if !ok {
		return Result{}, false
	}
	se := &s.seasons[loc.season]
	return se.resultAt(loc.index)
}

// Results returns a season's official results in fixture order (round, then
// fixture ID).
func (s *Store) Results(ref SeasonRef) []Result {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return nil
	}
	se := &s.seasons[i]
	var out []Result
	for j := range se.fixtures {
		if r, ok := se.resultAt(j); ok {
			out = append(out, r)
		}
	}
	return out
}

func (se *season) resultAt(i int) (Result, bool) {
	r := se.results[i]
	if !r.recorded {
		return Result{}, false
	}
	f := se.fixtures[i]
	return Result{
		Fixture: f.ID, Season: f.Season, Round: f.Round, Home: f.Home, Away: f.Away,
		HomeGoals: r.home, AwayGoals: r.away, HomePenalties: r.pens[0], AwayPenalties: r.pens[1], RecordedAt: r.at,
	}, true
}

// Standing is one team's derived league record.
type Standing struct {
	Rank                   int
	Team                   ids.TeamID
	Played                 int
	Won, Drawn, Lost       int
	GoalsFor, GoalsAgainst int
	Points                 int
}

func (s Standing) GoalDifference() int { return s.GoalsFor - s.GoalsAgainst }

// Standings derives a league's table from official results. Every entrant
// appears, ranked by points, goal difference and goals scored (all
// descending), then TeamID ascending as an explicit prototype tie-breaker.
// Nothing is stored: the table is recomputed from results on every call. A
// knockout season has no table (nil); see Ranking.
func (s *Store) Standings(ref SeasonRef) []Standing {
	i, ok := s.seasonIdx[ref]
	if !ok || s.seasons[i].format != FormatLeague {
		return nil
	}
	se := &s.seasons[i]
	rows := make([]Standing, len(se.entrants))
	index := map[ids.TeamID]int{}
	for j, t := range se.entrants {
		rows[j].Team = t
		index[t] = j
	}
	for j := range se.fixtures {
		r, ok := se.resultAt(j)
		if !ok {
			continue
		}
		home, away := &rows[index[r.Home]], &rows[index[r.Away]]
		record(home, int(r.HomeGoals), int(r.AwayGoals))
		record(away, int(r.AwayGoals), int(r.HomeGoals))
	}
	slices.SortFunc(rows, func(a, b Standing) int {
		return cmp.Or(
			-cmp.Compare(a.Points, b.Points),
			-cmp.Compare(a.GoalDifference(), b.GoalDifference()),
			-cmp.Compare(a.GoalsFor, b.GoalsFor),
			cmp.Compare(a.Team, b.Team),
		)
	})
	for j := range rows {
		rows[j].Rank = j + 1
	}
	return rows
}

func record(row *Standing, scored, conceded int) {
	row.Played++
	row.GoalsFor += scored
	row.GoalsAgainst += conceded
	switch {
	case scored > conceded:
		row.Won++
		row.Points += PointsForWin
	case scored == conceded:
		row.Drawn++
		row.Points += PointsForDraw
	default:
		row.Lost++
	}
}

// completed reports whether every round of the season is completed.
func (se *season) completed() bool {
	for _, r := range se.rounds {
		if r.status != RoundCompleted {
			return false
		}
	}
	return true
}

// SeasonCompleted reports whether every round of the season has official
// results.
func (s *Store) SeasonCompleted(ref SeasonRef) bool {
	i, ok := s.seasonIdx[ref]
	return ok && s.seasons[i].completed()
}

// Ranking orders a season's entrants, best first: a league by its table; a
// knockout by the round each team reached, a team still in (or the
// champion) ahead of one that lost in that round, then bracket order. For a
// completed knockout that is the champion, the runner-up, the semi-final
// losers, and so on. A ties season ranks its winners first, in tie order
// (see ties.go). Nil for an unknown season.
func (s *Store) Ranking(ref SeasonRef) []ids.TeamID {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return nil
	}
	se := &s.seasons[i]
	switch se.format {
	case FormatLeague:
		var out []ids.TeamID
		for _, st := range s.Standings(ref) {
			out = append(out, st.Team)
		}
		return out
	case FormatTies:
		return se.tiesRanking()
	}
	return se.knockoutRanking()
}

// Champion returns the winner of a completed season: the top of a league's
// table or the winner of a knockout's final. A ties season has no champion:
// every tie stands alone.
func (s *Store) Champion(ref SeasonRef) (ids.TeamID, bool) {
	if !s.SeasonCompleted(ref) {
		return 0, false
	}
	if f, ok := s.Format(ref); ok && f != FormatLeague && f != FormatKnockout {
		return 0, false
	}
	r := s.Ranking(ref)
	if len(r) == 0 {
		return 0, false
	}
	return r[0], true
}
