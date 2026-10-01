package competitions

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// RoundRef identifies one round of one competition season.
type RoundRef struct {
	Season SeasonRef
	Round  Round
}

func (r RoundRef) String() string { return fmt.Sprintf("%s round %d", r.Season, r.Round) }

// RoundStatus is a round's lifecycle state: Scheduled -> AwaitingResults
// (kicked off) -> Completed (official results recorded). Values are durable;
// never reorder.
type RoundStatus uint8

const (
	RoundScheduled       RoundStatus = 1
	RoundAwaitingResults RoundStatus = 2
	RoundCompleted       RoundStatus = 3
)

func (s RoundStatus) Valid() bool { return s >= RoundScheduled && s <= RoundCompleted }

func (s RoundStatus) String() string {
	switch s {
	case RoundScheduled:
		return "scheduled"
	case RoundAwaitingResults:
		return "awaiting results"
	case RoundCompleted:
		return "completed"
	}
	return fmt.Sprintf("RoundStatus(%d)", uint8(s))
}

// RoundInfo is a read view of one round.
type RoundInfo struct {
	Ref      RoundRef
	Kickoff  sim.GameInstant
	Status   RoundStatus
	Fixtures []ids.FixtureID // ascending
}

type roundState struct {
	kickoff sim.GameInstant
	status  RoundStatus
}

func (t Timing) validate() error {
	if !t.FirstKickoff.Valid() {
		return fmt.Errorf("competitions: first kickoff %d outside supported range", t.FirstKickoff)
	}
	// The upper bound keeps round*interval far from int64 overflow.
	if t.RoundInterval <= 0 || t.RoundInterval > sim.Duration(sim.MaxInstant) {
		return fmt.Errorf("competitions: round interval %d outside 1..%d", t.RoundInterval, sim.MaxInstant)
	}
	return nil
}

// SeasonKickoff returns a league's first kickoff in the given season, from
// its first season's first kickoff (civil UTC): the instant on the same
// weekday and time in the week nearest that kickoff's (season-1)th
// anniversary. Seasons so start at most three days from the anniversary,
// every year, instead of drifting against the civil calendar (and the
// contract year and transfer window, which follow it). A 29 February
// anniversary falls on 1 March in common years.
func SeasonKickoff(cal sim.Calendar, first sim.CivilTime, season Season) (sim.GameInstant, error) {
	start, err := cal.Instant(first)
	if err != nil {
		return 0, fmt.Errorf("competitions: first kickoff: %w", err)
	}
	if season < 1 {
		return 0, fmt.Errorf("competitions: season %d is not 1-based", season)
	}
	month := first
	month.Year, month.Day = first.Year+int(season)-1, 1
	anniversary, err := cal.Instant(month)
	if err != nil {
		return 0, fmt.Errorf("competitions: season %d anniversary: %w", season, err)
	}
	days := (anniversary-start)/sim.GameInstant(sim.Day) + sim.GameInstant(first.Day-1)
	return start.Add(sim.Duration((days+3)/7) * sim.Week)
}

// roundKickoffs returns the kickoff of each of n rounds: Timing's explicit
// Kickoffs, which must be exactly n strictly ascending valid instants with
// no FirstKickoff or RoundInterval beside them, or else FirstKickoff and
// RoundInterval apart.
func roundKickoffs(t Timing, n int) ([]sim.GameInstant, error) {
	if t.Kickoffs != nil {
		if t.FirstKickoff != 0 || t.RoundInterval != 0 {
			return nil, fmt.Errorf("competitions: timing sets both kickoffs and a first kickoff or interval")
		}
		if len(t.Kickoffs) != n {
			return nil, fmt.Errorf("competitions: %d kickoffs for %d rounds", len(t.Kickoffs), n)
		}
		for i, k := range t.Kickoffs {
			if !k.Valid() || (i > 0 && k <= t.Kickoffs[i-1]) {
				return nil, fmt.Errorf("competitions: round %d kickoff %d is invalid or not after round %d", i+1, k, i)
			}
		}
		return slices.Clone(t.Kickoffs), nil
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	out := make([]sim.GameInstant, n)
	for i := range out {
		k, err := t.FirstKickoff.Add(sim.Duration(i) * t.RoundInterval)
		if err != nil {
			return nil, fmt.Errorf("competitions: round %d kickoff: %w", i+1, err)
		}
		out[i] = k
	}
	return out, nil
}

// CupMidweek is how long after a league matchday a cup round played
// between matchdays kicks off: the Wednesday after a Saturday, at the same
// time of day. A league whose rounds are not further apart than this cannot
// host a cup between them.
const CupMidweek = 4 * sim.Day

// CupMatchdays spreads a cup edition's rounds over a league season of
// leagueRounds matchdays: cup round r (1-based) of cupRounds is played
// CupMidweek after matchday ceil(r*leagueRounds/cupRounds), so the rounds
// are as evenly spaced as whole matchdays allow and the final follows the
// season's last matchday. It returns those matchdays (1-based), or an error
// when the cup has more rounds than the season has matchdays (two rounds
// would follow the same one).
func CupMatchdays(leagueRounds, cupRounds int) ([]int, error) {
	if cupRounds < 1 || leagueRounds < cupRounds {
		return nil, fmt.Errorf("competitions: %d cup rounds do not fit between %d league matchdays", cupRounds, leagueRounds)
	}
	out := make([]int, cupRounds)
	for r := range out {
		out[r] = ((r+1)*leagueRounds + cupRounds - 1) / cupRounds
	}
	return out, nil
}

// Rounds returns a season's rounds in ascending order.
func (s *Store) Rounds(ref SeasonRef) []RoundInfo {
	i, ok := s.seasonIdx[ref]
	if !ok {
		return nil
	}
	se := &s.seasons[i]
	out := make([]RoundInfo, len(se.rounds))
	for r, st := range se.rounds {
		out[r] = RoundInfo{Ref: RoundRef{ref, Round(r + 1)}, Kickoff: st.kickoff, Status: st.status}
	}
	for _, f := range se.fixtures {
		out[f.Round-1].Fixtures = append(out[f.Round-1].Fixtures, f.ID)
	}
	return out
}

// Round returns one round.
func (s *Store) Round(ref RoundRef) (RoundInfo, bool) {
	rounds := s.Rounds(ref.Season)
	if ref.Round < 1 || int(ref.Round) > len(rounds) {
		return RoundInfo{}, false
	}
	return rounds[ref.Round-1], true
}

// PendingRounds returns every round awaiting results, ordered by kickoff,
// competition, season and round.
func (s *Store) PendingRounds() []RoundInfo {
	var out []RoundInfo
	for _, se := range s.seasons {
		for _, r := range s.Rounds(se.ref) {
			if r.Status == RoundAwaitingResults {
				out = append(out, r)
			}
		}
	}
	slices.SortFunc(out, func(a, b RoundInfo) int {
		return cmp.Or(
			cmp.Compare(a.Kickoff, b.Kickoff),
			cmp.Compare(a.Ref.Season.Competition, b.Ref.Season.Competition),
			cmp.Compare(a.Ref.Season.Season, b.Ref.Season.Season),
			cmp.Compare(a.Ref.Round, b.Ref.Round),
		)
	})
	return out
}

// BeginRounds marks rounds as kicked off and awaiting results. Every ref must
// exist, appear once, be RoundScheduled, have its fixtures and kick off
// exactly at at. All refs
// are checked before any is changed: on error the store is unchanged.
// Fixtures are not marked played and no results are created.
func (s *Store) BeginRounds(refs []RoundRef, at sim.GameInstant) error {
	if len(refs) == 0 {
		return fmt.Errorf("competitions: no rounds to begin")
	}
	seen := map[RoundRef]bool{}
	targets := make([]*roundState, 0, len(refs))
	for _, ref := range refs {
		i, ok := s.seasonIdx[ref.Season]
		if !ok || ref.Round < 1 || int(ref.Round) > len(s.seasons[i].rounds) {
			return fmt.Errorf("competitions: unknown %s", ref)
		}
		if seen[ref] {
			return fmt.Errorf("competitions: %s listed twice", ref)
		}
		seen[ref] = true
		st := &s.seasons[i].rounds[ref.Round-1]
		if st.status != RoundScheduled {
			return fmt.Errorf("competitions: %s is %s, not scheduled", ref, st.status)
		}
		if !slices.ContainsFunc(s.seasons[i].fixtures, func(f Fixture) bool { return f.Round == ref.Round }) {
			return fmt.Errorf("competitions: %s has no fixtures yet: the previous round is not complete", ref)
		}
		if st.kickoff != at {
			return fmt.Errorf("competitions: %s kicks off at %d, not %d", ref, st.kickoff, at)
		}
		targets = append(targets, st)
	}
	for _, st := range targets {
		st.status = RoundAwaitingResults
	}
	return nil
}
