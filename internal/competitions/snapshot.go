package competitions

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// Snapshot is the store's authoritative state. Lookup indexes and standings
// are derived and rebuilt on restore.
type Snapshot struct {
	LastFixture ids.FixtureID    // fixture ID allocator
	Seasons     []SeasonSnapshot // creation order
}

type SeasonSnapshot struct {
	Ref      SeasonRef
	Entrants []ids.TeamID     // ascending
	Fixtures []Fixture        // by round, then fixture ID
	Rounds   []RoundSnapshot  // index = round-1
	Results  []ResultSnapshot // fixture order; only fixtures with official results
}

type RoundSnapshot struct {
	Kickoff sim.GameInstant
	Status  RoundStatus
}

type ResultSnapshot struct {
	Fixture    ids.FixtureID
	HomeGoals  uint16
	AwayGoals  uint16
	RecordedAt sim.GameInstant
}

// Snapshot exports the store's state as fresh copies.
func (s *Store) Snapshot() Snapshot {
	out := Snapshot{LastFixture: s.lastFixture, Seasons: make([]SeasonSnapshot, 0, len(s.seasons))}
	for _, se := range s.seasons {
		ss := SeasonSnapshot{
			Ref:      se.ref,
			Entrants: slices.Clone(se.entrants),
			Fixtures: slices.Clone(se.fixtures),
			Rounds:   make([]RoundSnapshot, len(se.rounds)),
		}
		for i, r := range se.rounds {
			ss.Rounds[i] = RoundSnapshot{Kickoff: r.kickoff, Status: r.status}
		}
		for i, r := range se.results {
			if r.recorded {
				ss.Results = append(ss.Results, ResultSnapshot{
					Fixture: se.fixtures[i].ID, HomeGoals: r.home, AwayGoals: r.away, RecordedAt: r.at,
				})
			}
		}
		out.Seasons = append(out.Seasons, ss)
	}
	return out
}

// Restore rebuilds a store from a snapshot. It copies its input, allocates
// nothing and regenerates nothing: fixtures, kickoffs, statuses and results
// are taken as saved and fully validated.
//
// Rejected: invalid or duplicate season refs; non-canonical or invalid
// entrants; wrong round counts, invalid statuses or non-increasing kickoffs;
// fixtures out of order, in the wrong season or round, with a kickoff that
// differs from their round, with zero, duplicate or above-allocator IDs, or
// breaking the double round-robin invariants; results for unknown fixtures,
// duplicated, above MaxGoals, recorded before kickoff, or not matching
// "results exist exactly for completed rounds".
func Restore(snap Snapshot) (*Store, error) {
	s := New()
	s.lastFixture = snap.LastFixture
	for si, ss := range snap.Seasons {
		fail := func(format string, args ...any) (*Store, error) {
			return nil, fmt.Errorf("competitions: restore %s: "+format, append([]any{ss.Ref}, args...)...)
		}
		if !ss.Ref.Valid() {
			return fail("invalid season ref")
		}
		if _, dup := s.seasonIdx[ss.Ref]; dup {
			return fail("duplicate season")
		}
		canonical, err := canonicalEntrants(ss.Entrants)
		if err != nil {
			return fail("%v", err)
		}
		if !slices.Equal(canonical, ss.Entrants) {
			return fail("entrants not in canonical order")
		}
		if want := 2 * (len(canonical) - 1); len(ss.Rounds) != want {
			return fail("%d rounds, want %d", len(ss.Rounds), want)
		}
		rounds := make([]roundState, len(ss.Rounds))
		for i, r := range ss.Rounds {
			if !r.Status.Valid() || !r.Kickoff.Valid() || (i > 0 && r.Kickoff <= ss.Rounds[i-1].Kickoff) {
				return fail("round %d has invalid status %d or kickoff %d", i+1, r.Status, r.Kickoff)
			}
			rounds[i] = roundState{kickoff: r.Kickoff, status: r.Status}
		}

		fixtures := slices.Clone(ss.Fixtures)
		pos := map[ids.FixtureID]int{}
		for i, f := range fixtures {
			switch {
			case f.Season != ss.Ref:
				return fail("fixture %d references %s", f.ID, f.Season)
			case f.Round < 1 || int(f.Round) > len(rounds):
				return fail("fixture %d has round %d", f.ID, f.Round)
			case f.Kickoff != rounds[f.Round-1].kickoff:
				return fail("fixture %d kicks off at %d, round %d at %d", f.ID, f.Kickoff, f.Round, rounds[f.Round-1].kickoff)
			case !f.ID.Valid() || f.ID > snap.LastFixture:
				return fail("fixture ID %d is zero or above allocator %d", f.ID, snap.LastFixture)
			case i > 0 && (f.Round < fixtures[i-1].Round || (f.Round == fixtures[i-1].Round && f.ID <= fixtures[i-1].ID)):
				return fail("fixtures not ordered by round then ID at %d", f.ID)
			}
			if _, dup := s.fixtureIdx[f.ID]; dup {
				return fail("fixture ID %d used by another season", f.ID)
			}
			pos[f.ID] = i
		}
		if err := checkDoubleRoundRobin(canonical, fixtures); err != nil {
			return fail("%v", err)
		}

		results := make([]result, len(fixtures))
		for _, r := range ss.Results {
			i, ok := pos[r.Fixture]
			switch {
			case !ok:
				return fail("result for unknown fixture %d", r.Fixture)
			case results[i].recorded:
				return fail("fixture %d has two results", r.Fixture)
			case r.HomeGoals > MaxGoals || r.AwayGoals > MaxGoals:
				return fail("fixture %d score %d-%d exceeds %d", r.Fixture, r.HomeGoals, r.AwayGoals, MaxGoals)
			case !r.RecordedAt.Valid() || r.RecordedAt < fixtures[i].Kickoff:
				return fail("fixture %d result recorded at %d, before kickoff %d", r.Fixture, r.RecordedAt, fixtures[i].Kickoff)
			}
			results[i] = result{recorded: true, home: r.HomeGoals, away: r.AwayGoals, at: r.RecordedAt}
		}
		for i, f := range fixtures {
			if results[i].recorded != (rounds[f.Round-1].status == RoundCompleted) {
				return fail("fixture %d result recorded=%v but round %d is %s", f.ID, results[i].recorded, f.Round, rounds[f.Round-1].status)
			}
		}

		s.seasonIdx[ss.Ref] = si
		s.seasons = append(s.seasons, season{ref: ss.Ref, entrants: canonical, fixtures: fixtures, results: results, rounds: rounds})
		for i, f := range fixtures {
			s.fixtureIdx[f.ID] = fixtureLoc{season: si, index: i}
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}
