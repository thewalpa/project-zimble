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

// Score is a proposed official regulation result for one fixture.
type Score struct {
	Fixture   ids.FixtureID
	HomeGoals uint16
	AwayGoals uint16
}

// Result is an official result. Competitions owns it; it is recorded once
// and never changed.
type Result struct {
	Fixture    ids.FixtureID
	Season     SeasonRef
	Round      Round
	Home       ids.TeamID
	Away       ids.TeamID
	HomeGoals  uint16
	AwayGoals  uint16
	RecordedAt sim.GameInstant
}

type result struct {
	recorded   bool
	home, away uint16
	at         sim.GameInstant
}

// CompleteRounds records official results for every fixture of the given
// rounds and marks them RoundCompleted, as one change. Every round must
// exist, appear once, await results and have kicked off at or before at.
// scores must contain exactly one entry for each of those rounds' fixtures
// and nothing else. Everything is checked before anything changes: on error
// the store is unchanged.
func (s *Store) CompleteRounds(refs []RoundRef, scores []Score, at sim.GameInstant) error {
	if len(refs) == 0 {
		return fmt.Errorf("competitions: no rounds to complete")
	}
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
		if sc.HomeGoals > MaxGoals || sc.AwayGoals > MaxGoals {
			return fmt.Errorf("competitions: fixture %d score %d-%d exceeds %d", sc.Fixture, sc.HomeGoals, sc.AwayGoals, MaxGoals)
		}
	}

	for _, sc := range scores {
		l := expected[sc.Fixture]
		s.seasons[l.season].results[l.index] = result{recorded: true, home: sc.HomeGoals, away: sc.AwayGoals, at: at}
	}
	for _, st := range rounds {
		st.status = RoundCompleted
	}
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
		HomeGoals: r.home, AwayGoals: r.away, RecordedAt: r.at,
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

// Standings derives the league table from official results. Every entrant
// appears, ranked by points, goal difference and goals scored (all
// descending), then TeamID ascending as an explicit prototype tie-breaker.
// Nothing is stored: the table is recomputed from results on every call.
func (s *Store) Standings(ref SeasonRef) []Standing {
	i, ok := s.seasonIdx[ref]
	if !ok {
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
