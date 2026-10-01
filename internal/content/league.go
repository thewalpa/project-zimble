package content

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// LeagueVersion identifies the competition definitions returned by
// DefaultLeagues and DefaultCups. It is separate from Version so competition
// changes do not alter generated worlds. Version 3 added a second league and
// the continental cup; version 4 added a second division for each league and
// the promotion links between them; version 5 dropped League.SeasonInterval;
// version 6 added the cups' prize tables.
const LeagueVersion = 6

// MaxLeagueEntrants is the largest league competitions can schedule
// (competitions.MaxLeagueEntrants, which app's tests keep equal). A league
// has an even number of entrants: an odd one would need byes.
const MaxLeagueEntrants = 128

// MaxSeasonSpan is the longest a league season may run, from its first
// kickoff to its last. Seasons start within three days of their first
// kickoff's anniversary, so consecutive seasons are at least 359 days apart;
// a season up to 51 weeks (357 days) long ends before the next one starts.
const MaxSeasonSpan = 51 * sim.Week

// League defines a double round-robin league competition and its seasons'
// scheduling inputs. FirstKickoff is the first season's first kickoff in UTC
// civil time; the career calendar converts it to a game instant. Each later
// season kicks off on its weekday and time in the week nearest its
// anniversary (competitions.SeasonKickoff), and its rounds follow
// RoundInterval apart.
type League struct {
	ID            ids.CompetitionID
	Name          string
	Entrants      int
	FirstKickoff  sim.CivilTime
	RoundInterval sim.Duration

	// Match rules for the league's fixtures.
	MaxSubstitutions uint8
	MaxBench         uint8
}

// Rounds is the number of rounds in one double round-robin season.
func (l League) Rounds() int { return 2 * (l.Entrants - 1) }

// Validate checks the definition: an even number of entrants up to
// MaxLeagueEntrants, and a season whose last kickoff comes within
// MaxSeasonSpan of its first, so that it ends before the next season starts.
func (l League) Validate() error {
	if !l.ID.Valid() || l.Name == "" || l.Entrants < 2 || l.Entrants%2 != 0 || l.Entrants > MaxLeagueEntrants || l.RoundInterval <= 0 || l.MaxSubstitutions > l.MaxBench {
		return fmt.Errorf("content: invalid league definition %+v", l)
	}
	if l.RoundInterval > MaxSeasonSpan {
		return fmt.Errorf("content: league %d round interval %d exceeds a season", l.ID, l.RoundInterval)
	}
	if lastOffset := sim.Duration(l.Rounds()-1) * l.RoundInterval; lastOffset > MaxSeasonSpan {
		return fmt.Errorf("content: league %d last kickoff offset %d exceeds the longest season %d", l.ID, lastOffset, MaxSeasonSpan)
	}
	return nil
}

// DefaultLeagues returns the built-in leagues: a first division per default
// nation (IDs 1 and 2) and a second division for each (IDs 4 and 5; ID 3 is
// the cup), all with the same calendar, so their rounds kick off together.
// Leagues take clubs in ID order, matching the order worldgen generates them:
// both first divisions, then both second divisions.
func DefaultLeagues() []League {
	harbour := DefaultLeague()
	harbour.ID, harbour.Name = 2, "Harbour League"
	foundersTwo := DefaultLeague()
	foundersTwo.ID, foundersTwo.Name = 4, "Founders Second Division"
	harbourTwo := DefaultLeague()
	harbourTwo.ID, harbourTwo.Name = 5, "Harbour Second Division"
	return []League{DefaultLeague(), harbour, foundersTwo, harbourTwo}
}

// Promotion links two adjacent divisions: the boundary places between them
// (the bottom Places of Upper's final ranking and the top Places of Lower's).
// Only Upper, Lower and Places need be known to apply it; what happens at those
// places (the play-offs) is competitions' rule, see competitions/movement.go.
// Authored league and cup IDs must stay below competitions.PlayoffBase (1000),
// where the derived play-off competitions begin; app.World loading rejects any
// at or above it.
type Promotion struct {
	Upper, Lower ids.CompetitionID
	Places       int
}

// DefaultPromotions returns the built-in links: two places between each
// nation's divisions. Continental Cup places stay with the first divisions.
func DefaultPromotions() []Promotion {
	return []Promotion{{Upper: 1, Lower: 4, Places: 2}, {Upper: 2, Lower: 5, Places: 2}}
}

// ValidatePromotions checks links against the leagues: each names two
// distinct leagues of the same size, moves at least one and at most half of
// their places, and no league is the upper end of two links or the lower end
// of two. Links may chain (a league may be one link's lower end and another's
// upper end) but not loop. Leagues must each be valid on their own.
func ValidatePromotions(leagues []League, links []Promotion) error {
	size := map[ids.CompetitionID]int{}
	for _, l := range leagues {
		size[l.ID] = l.Entrants
	}
	down, up := map[ids.CompetitionID]ids.CompetitionID{}, map[ids.CompetitionID]bool{}
	for _, p := range links {
		nu, okU := size[p.Upper]
		nl, okL := size[p.Lower]
		switch {
		case !okU || !okL || p.Upper == p.Lower:
			return fmt.Errorf("content: promotion %+v names a missing or identical league", p)
		case nu != nl:
			return fmt.Errorf("content: promotion %+v joins leagues of %d and %d entrants", p, nu, nl)
		case p.Places < 1 || p.Places > nu/2:
			return fmt.Errorf("content: promotion %+v moves %d places of %d entrants", p, p.Places, nu)
		}
		if _, dup := down[p.Upper]; dup || up[p.Lower] {
			return fmt.Errorf("content: promotion %+v reuses a league end", p)
		}
		down[p.Upper], up[p.Lower] = p.Lower, true
	}
	for start := range down {
		for at, n := start, 0; ; n++ {
			next, ok := down[at]
			if !ok {
				break
			}
			if next == start || n > len(down) {
				return fmt.Errorf("content: promotion links loop through league %d", start)
			}
			at = next
		}
	}
	return nil
}

// DefaultLeague returns the first built-in league, for Westmark's clubs.
func DefaultLeague() League {
	return League{
		ID:            1,
		Name:          "Founders League",
		Entrants:      8,
		FirstKickoff:  sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15}, // a Saturday
		RoundInterval: sim.Week,

		MaxSubstitutions: 3,
		MaxBench:         7,
	}
}

// Qualifier is a league's places in a cup: its top Places teams of the
// season just finished.
type Qualifier struct {
	League ids.CompetitionID
	Places int
}

// Cup defines a knockout competition between leagues' best teams. Edition N
// is drawn once every qualifying league's season N is complete and plays
// during their season N+1. The application spaces rounds across that year's
// league matchdays, four days after each chosen matchday. Season 1 has no
// prior ranking, so no cup edition is played during it.
//
// Seeding interleaves the qualifiers by finishing position (every league's
// champion, in Qualifiers order, then every runner-up, and so on) and places
// the seeds in the standard bracket, so seed 1 meets the last seed first and
// the top two seeds can meet only in the final. With two leagues of four
// places: A1 v B4, B2 v A3, B1 v A4, A2 v B3.
type Cup struct {
	ID         ids.CompetitionID
	Name       string
	Qualifiers []Qualifier
	// Legacy timing metadata, retained in pinned definitions and save
	// fingerprints. ScheduleVersion 3 used these intervals after the
	// qualifying seasons; ScheduleVersion 4 derives midweek kickoffs from
	// the qualifying leagues' next calendar instead.
	FirstRoundDelay sim.Duration
	RoundInterval   sim.Duration

	// Match rules for the cup's fixtures. Every match needs a winner:
	// a draw is decided by penalties.
	MaxSubstitutions uint8
	MaxBench         uint8

	// Prizes are what an entrant is paid for the stage it reached in a
	// completed edition, indexed by stage counted back from the final
	// (competitions.Exit.Stage): Prizes[0] for the champion, [1] for the
	// runner-up, [2] for each semi-final loser, and so on. An entrant is
	// paid once, for its stage only; a stage beyond the table pays nothing.
	// Never increasing: a deeper run never pays less. Empty: no prize money.
	Prizes []money.Money `json:",omitempty"`
}

// Clone returns a copy that shares no memory with c.
func (c Cup) Clone() Cup {
	c.Qualifiers, c.Prizes = slices.Clone(c.Qualifiers), slices.Clone(c.Prizes)
	return c
}

// Entrants is the number of teams in each edition.
func (c Cup) Entrants() int {
	n := 0
	for _, q := range c.Qualifiers {
		n += q.Places
	}
	return n
}

// Rounds is the number of rounds in each edition: log2 of Entrants.
func (c Cup) Rounds() int {
	r := 0
	for n := c.Entrants(); n > 1; n /= 2 {
		r++
	}
	return r
}

// Validate checks the definition on its own: a power of two of at least two
// qualifiers from distinct leagues, positive timing, and a prize table of at
// most one non-negative, never increasing amount per stage (Rounds + 1).
// Whether each league exists and has enough entrants is checked by the
// application.
func (c Cup) Validate() error {
	n := c.Entrants()
	if !c.ID.Valid() || c.Name == "" || n < 2 || n&(n-1) != 0 || c.FirstRoundDelay <= 0 || c.RoundInterval <= 0 || c.MaxSubstitutions > c.MaxBench {
		return fmt.Errorf("content: invalid cup definition %+v", c)
	}
	for i, q := range c.Qualifiers {
		if !q.League.Valid() || q.Places < 1 || slices.ContainsFunc(c.Qualifiers[:i], func(o Qualifier) bool { return o.League == q.League }) {
			return fmt.Errorf("content: cup %d qualifier %+v is invalid or repeated", c.ID, q)
		}
	}
	if len(c.Prizes) > c.Rounds()+1 {
		return fmt.Errorf("content: cup %d has %d prizes for %d stages", c.ID, len(c.Prizes), c.Rounds()+1)
	}
	for i, p := range c.Prizes {
		if p < 0 || (i > 0 && p > c.Prizes[i-1]) {
			return fmt.Errorf("content: cup %d prizes %v are negative or increase", c.ID, c.Prizes)
		}
	}
	return nil
}

// Seeding returns the cup's bracket as (qualifier index, finishing
// position) pairs in bracket order; see Cup.
func (c Cup) Seeding() [][2]int {
	var seeds [][2]int
	for pos := 1; len(seeds) < c.Entrants(); pos++ {
		for qi, q := range c.Qualifiers {
			if pos <= q.Places {
				seeds = append(seeds, [2]int{qi, pos})
			}
		}
	}
	order := []int{1}
	for len(order) < len(seeds) {
		next := make([]int, 0, 2*len(order))
		for _, s := range order {
			next = append(next, s, 2*len(order)+1-s)
		}
		order = next
	}
	out := make([][2]int, len(seeds))
	for i, s := range order {
		out[i] = seeds[s-1]
	}
	return out
}

// DefaultCups returns the built-in cups: the Continental Cup for the top
// four of both default leagues, starting two weeks after their final round.
// Its prizes are scaled to a 250,000 home gate and wage bills of about 1.7
// million a year: the champion's 1,000,000 is four home gates.
func DefaultCups() []Cup {
	return []Cup{{
		ID:               3,
		Name:             "Continental Cup",
		Qualifiers:       []Qualifier{{League: 1, Places: 4}, {League: 2, Places: 4}},
		FirstRoundDelay:  2 * sim.Week,
		RoundInterval:    sim.Week,
		MaxSubstitutions: 3,
		MaxBench:         7,
		Prizes:           []money.Money{money.Units(1_000_000), money.Units(600_000), money.Units(350_000), money.Units(200_000)},
	}}
}
