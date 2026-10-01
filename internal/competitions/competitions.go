// Package competitions owns competition-season instances: their format,
// entrants, fixtures, kickoff times, round status and official results.
// Standings, rankings and champions are derived from results on demand.
//
// A league season is a double round robin ranked by its table. A knockout
// season is a fixed bracket of single-match ties (see knockout.go): each
// round's fixtures are created from the previous round's winners when that
// round's results are recorded. A ties season is one round of parallel
// single-match ties (see ties.go), the format of a promotion play-off.
//
// Whether an entrant team exists is checked by the application, which can
// read the registry; this package does not import other domain modules.
package competitions

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// ScheduleVersion identifies the fixture-generation algorithm and the season
// calendar (SeasonKickoff). Bump it when the same seed, season and entrants
// would produce different fixtures or kickoffs. Version 2 anchored seasons to
// their first kickoff's anniversary; pairings are drawn from pairingDraw's
// stream and did not change. Version 3 covers ties seasons (promotion
// play-offs): the application creates them before the cups that follow, so
// later fixture IDs shift.
const ScheduleVersion = 3

// pairingDraw keys the stream that places entrants in schedule slots. It is
// the ScheduleVersion that last changed pairings, so a calendar change does
// not reshuffle every career's fixtures.
const pairingDraw = 1

// MaxLeagueEntrants is the largest league: its double round-robin's
// 2(n-1) rounds must fit in a Round. A league takes any even number of
// entrants from 2 up to this; an odd number would need byes.
const MaxLeagueEntrants = 128

// Season is a 1-based season number within a career.
type Season uint16

// SeasonRef identifies one season of one competition.
type SeasonRef struct {
	Competition ids.CompetitionID
	Season      Season
}

func (r SeasonRef) Valid() bool { return r.Competition.Valid() && r.Season > 0 }

func (r SeasonRef) String() string {
	return fmt.Sprintf("competition %d season %d", r.Competition, r.Season)
}

// Round is a 1-based round number within a competition season.
type Round uint8

// Format is how a competition season is played. Values are durable; never
// reorder.
type Format uint8

const (
	FormatLeague   Format = 1 // double round robin, ranked by the table
	FormatKnockout Format = 2 // single-match ties in a fixed bracket; a level tie goes to penalties
	FormatTies     Format = 3 // one round of parallel single-match ties, pairs in entrant order; a level tie goes to penalties
)

func (f Format) Valid() bool { return f == FormatLeague || f == FormatKnockout || f == FormatTies }

func (f Format) String() string {
	switch f {
	case FormatLeague:
		return "league"
	case FormatKnockout:
		return "knockout"
	case FormatTies:
		return "ties"
	}
	return fmt.Sprintf("Format(%d)", uint8(f))
}

// Fixture is a scheduled meeting. Its ID is opaque and unique across all
// competitions and seasons in the store; Season, Round, Home and Away are its
// natural key. Kickoff is owned here; every fixture in a round currently
// shares the round's kickoff.
type Fixture struct {
	ID      ids.FixtureID
	Season  SeasonRef
	Round   Round
	Home    ids.TeamID
	Away    ids.TeamID
	Kickoff sim.GameInstant
}

// Timing is the explicit scheduling input for a season. Round r kicks off
// at FirstKickoff + (r-1)*RoundInterval. Timing never affects pairings or
// fixture IDs.
type Timing struct {
	FirstKickoff  sim.GameInstant
	RoundInterval sim.Duration
}

type fixtureLoc struct{ season, index int }

type season struct {
	ref      SeasonRef
	format   Format
	entrants []ids.TeamID // league: ascending; knockout: bracket order
	fixtures []Fixture    // by round, then ID
	results  []result     // parallel to fixtures
	rounds   []roundState // index = round-1
}

// Store is the authoritative competitions store. Queries return copies.
type Store struct {
	seasons     []season // in creation order
	seasonIdx   map[SeasonRef]int
	fixtureIdx  map[ids.FixtureID]fixtureLoc
	lastFixture ids.FixtureID
}

func New() *Store {
	return &Store{
		seasonIdx:  map[SeasonRef]int{},
		fixtureIdx: map[ids.FixtureID]fixtureLoc{},
	}
}

// NewSeason describes one season to create. A league's entrant order does
// not matter; a knockout's entrants are its bracket (see knockout.go); a
// ties season's entrants are its pairs flattened in order (see ties.go).
type NewSeason struct {
	Ref      SeasonRef
	Format   Format
	Entrants []ids.TeamID
	Timing   Timing
}

// CreateLeagueSeason schedules a double round-robin league for the entrants.
// Entrant order does not matter: entrants are sorted by ID before the seeded
// draw. The entrants slice is not retained or modified. Every round starts
// as RoundScheduled. On error the store is unchanged.
func (s *Store) CreateLeagueSeason(seed random.Seed, ref SeasonRef, entrants []ids.TeamID, timing Timing) error {
	return s.CreateSeasons(seed, []NewSeason{{Ref: ref, Format: FormatLeague, Entrants: entrants, Timing: timing}})
}

// CreateLeagueSeasons creates league seasons with CreateSeasons.
func (s *Store) CreateLeagueSeasons(seed random.Seed, specs []NewSeason) error {
	specs = slices.Clone(specs)
	for i := range specs {
		specs[i].Format = FormatLeague
	}
	return s.CreateSeasons(seed, specs)
}

// CreateSeasons creates several seasons of any format as one unit: every
// season is validated and generated before any is stored, so on error the
// store is unchanged. Seasons are created, and fixture IDs allocated, in
// (competition, season) order whatever the input order. Each league draw
// uses its own stream, keyed by (seed, competition, season); a knockout
// bracket involves no draw.
func (s *Store) CreateSeasons(seed random.Seed, specs []NewSeason) error {
	specs = slices.Clone(specs)
	slices.SortFunc(specs, func(a, b NewSeason) int {
		return cmp.Or(cmp.Compare(a.Ref.Competition, b.Ref.Competition), cmp.Compare(a.Ref.Season, b.Ref.Season))
	})
	staged := make([]season, 0, len(specs))
	next := s.lastFixture
	for i, spec := range specs {
		ref := spec.Ref
		if !ref.Valid() {
			return fmt.Errorf("competitions: invalid season %+v", ref)
		}
		if _, dup := s.seasonIdx[ref]; dup || (i > 0 && specs[i-1].Ref == ref) {
			return fmt.Errorf("competitions: %s already exists", ref)
		}
		if spec.Format == FormatKnockout {
			se, err := newKnockout(ref, spec.Entrants, spec.Timing, &next)
			if err != nil {
				return err
			}
			staged = append(staged, se)
			continue
		}
		if spec.Format == FormatTies {
			se, err := newTies(ref, spec.Entrants, spec.Timing, &next)
			if err != nil {
				return err
			}
			staged = append(staged, se)
			continue
		}
		if spec.Format != FormatLeague {
			return fmt.Errorf("competitions: %s has invalid format %d", ref, spec.Format)
		}
		canonical, err := canonicalEntrants(spec.Entrants)
		if err != nil {
			return err
		}
		kickoffs, err := roundKickoffs(spec.Timing, 2*(len(canonical)-1))
		if err != nil {
			return err
		}

		rng := random.Derive(seed, "competitions/fixtures", pairingDraw,
			uint64(ref.Competition), uint64(ref.Season))
		pairs := doubleRoundRobin(canonical, rng)

		fixtures := make([]Fixture, len(pairs))
		for i, p := range pairs {
			next++
			fixtures[i] = Fixture{ID: next, Season: ref, Round: p.round, Home: p.home, Away: p.away, Kickoff: kickoffs[p.round-1]}
		}
		rounds := make([]roundState, len(kickoffs))
		for i, k := range kickoffs {
			rounds[i] = roundState{kickoff: k, status: RoundScheduled}
		}
		if err := checkDoubleRoundRobin(canonical, fixtures); err != nil {
			return fmt.Errorf("competitions: generated invalid schedule for %s: %w", ref, err)
		}
		staged = append(staged, season{ref: ref, format: FormatLeague, entrants: canonical, fixtures: fixtures, results: make([]result, len(fixtures)), rounds: rounds})
	}

	for _, se := range staged {
		si := len(s.seasons)
		s.seasonIdx[se.ref] = si
		s.seasons = append(s.seasons, se)
		for i, f := range se.fixtures {
			s.fixtureIdx[f.ID] = fixtureLoc{season: si, index: i}
		}
	}
	s.lastFixture = next
	return nil
}

// Seasons returns all competition seasons in creation order.
func (s *Store) Seasons() []SeasonRef {
	out := make([]SeasonRef, len(s.seasons))
	for i, se := range s.seasons {
		out[i] = se.ref
	}
	return out
}

// Format returns a season's format.
func (s *Store) Format(ref SeasonRef) (Format, bool) {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return 0, false
	}
	return s.seasons[i].format, true
}

// Entrants returns a season's entrant teams: a league's in ascending ID
// order, a knockout's in bracket order.
func (s *Store) Entrants(ref SeasonRef) ([]ids.TeamID, bool) {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return nil, false
	}
	return slices.Clone(s.seasons[i].entrants), true
}

// Fixtures returns a season's fixtures ordered by round, then fixture ID.
func (s *Store) Fixtures(ref SeasonRef) []Fixture {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return nil
	}
	return slices.Clone(s.seasons[i].fixtures)
}

func (s *Store) Fixture(id ids.FixtureID) (Fixture, bool) {
	loc, ok := s.fixtureIdx[id]
	if !ok {
		return Fixture{}, false
	}
	return s.seasons[loc.season].fixtures[loc.index], true
}

// Validate re-checks every season's schedule and results against its format
// and global fixture uniqueness.
func (s *Store) Validate() error {
	var errs []error
	seen := map[ids.FixtureID]SeasonRef{}
	for _, se := range s.seasons {
		if err := se.checkFormat(); err != nil {
			errs = append(errs, fmt.Errorf("competitions: %s: %w", se.ref, err))
		}
		for _, f := range se.fixtures {
			if other, dup := seen[f.ID]; dup {
				errs = append(errs, fmt.Errorf("competitions: fixture %d in both %s and %s", f.ID, other, se.ref))
			}
			seen[f.ID] = se.ref
			if f.Season != se.ref {
				errs = append(errs, fmt.Errorf("competitions: fixture %d stored under %s but references %s", f.ID, se.ref, f.Season))
			}
			if int(f.Round) <= len(se.rounds) && f.Kickoff != se.rounds[f.Round-1].kickoff {
				errs = append(errs, fmt.Errorf("competitions: fixture %d kicks off at %d, round %d at %d", f.ID, f.Kickoff, f.Round, se.rounds[f.Round-1].kickoff))
			}
		}
		for i, f := range se.fixtures {
			// Official results exist exactly for fixtures of completed rounds.
			if int(f.Round) <= len(se.rounds) && se.results[i].recorded != (se.rounds[f.Round-1].status == RoundCompleted) {
				errs = append(errs, fmt.Errorf("competitions: fixture %d result recorded=%v but round %d is %s",
					f.ID, se.results[i].recorded, f.Round, se.rounds[f.Round-1].status))
			}
		}
		for i, r := range se.rounds {
			if !r.status.Valid() {
				errs = append(errs, fmt.Errorf("competitions: %s round %d has invalid status %d", se.ref, i+1, r.status))
			}
			if i > 0 && r.kickoff <= se.rounds[i-1].kickoff {
				errs = append(errs, fmt.Errorf("competitions: %s round %d kicks off no later than round %d", se.ref, i+1, i))
			}
		}
	}
	return errors.Join(errs...)
}

// checkFormat checks a season's fixtures and results against its format.
func (se *season) checkFormat() error {
	switch se.format {
	case FormatLeague:
		if err := checkDoubleRoundRobin(se.entrants, se.fixtures); err != nil {
			return err
		}
		for i, r := range se.results {
			if r.pens != [2]uint16{} {
				return fmt.Errorf("league fixture %d has a shootout", se.fixtures[i].ID)
			}
		}
		return nil
	case FormatKnockout:
		return se.checkKnockout()
	case FormatTies:
		return se.checkTies()
	}
	return fmt.Errorf("invalid format %d", se.format)
}

// canonicalEntrants validates entrants and returns a sorted copy.
func canonicalEntrants(entrants []ids.TeamID) ([]ids.TeamID, error) {
	if n := len(entrants); n < 2 || n%2 != 0 || n > MaxLeagueEntrants {
		return nil, fmt.Errorf("competitions: a league needs an even number of 2 to %d entrants, not %d", MaxLeagueEntrants, n)
	}
	out := slices.Clone(entrants)
	slices.Sort(out)
	for i, t := range out {
		if !t.Valid() {
			return nil, fmt.Errorf("competitions: invalid entrant team ID %d", t)
		}
		if i > 0 && out[i-1] == t {
			return nil, fmt.Errorf("competitions: duplicate entrant team %d", t)
		}
	}
	return out, nil
}
