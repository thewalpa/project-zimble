package content

import (
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// LeagueVersion identifies the competition definitions returned by
// DefaultLeagues and DefaultCups. It is separate from Version so competition
// changes do not alter generated worlds. Version 3 added a second league and
// the continental cup.
const LeagueVersion = 3

// League defines a double round-robin league competition and its seasons'
// scheduling inputs. FirstKickoff is the first season's first kickoff in UTC
// civil time; the career calendar converts it to a game instant. Each later
// season's first kickoff is SeasonInterval after the previous season's, so
// with 52 weeks every season starts on the same weekday and time.
type League struct {
	ID             ids.CompetitionID
	Name           string
	Entrants       int
	FirstKickoff   sim.CivilTime
	RoundInterval  sim.Duration
	SeasonInterval sim.Duration

	// Match rules for the league's fixtures.
	MaxSubstitutions uint8
	MaxBench         uint8
}

// Rounds is the number of rounds in one double round-robin season.
func (l League) Rounds() int { return 2 * (l.Entrants - 1) }

// Validate checks the definition. A season's last kickoff must come before
// the next season's first.
func (l League) Validate() error {
	if !l.ID.Valid() || l.Name == "" || l.Entrants < 2 || l.RoundInterval <= 0 || l.MaxSubstitutions > l.MaxBench {
		return fmt.Errorf("content: invalid league definition %+v", l)
	}
	if lastOffset := sim.Duration(l.Rounds()-1) * l.RoundInterval; l.SeasonInterval <= lastOffset {
		return fmt.Errorf("content: league %d season interval %d does not exceed its last kickoff offset %d", l.ID, l.SeasonInterval, lastOffset)
	}
	return nil
}

// DefaultLeagues returns the built-in leagues: one per default nation, with
// the same calendar, so their rounds kick off together.
func DefaultLeagues() []League {
	second := DefaultLeague()
	second.ID, second.Name = 2, "Harbour League"
	return []League{DefaultLeague(), second}
}

// DefaultLeague returns the first built-in league, for Westmark's clubs.
func DefaultLeague() League {
	return League{
		ID:            1,
		Name:          "Founders League",
		Entrants:      8,
		FirstKickoff:  sim.CivilTime{Year: 2025, Month: 8, Day: 9, Hour: 15}, // a Saturday
		RoundInterval: sim.Week,
		// 364 days: the next season starts on the same weekday, a year on.
		SeasonInterval: 52 * sim.Week,

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
// is played once every qualifying league's season N is complete: its first
// round kicks off FirstRoundDelay after the latest of those seasons' last
// kickoffs, and each later round RoundInterval after the previous one.
//
// Seeding interleaves the qualifiers by finishing position (every league's
// champion, in Qualifiers order, then every runner-up, and so on) and places
// the seeds in the standard bracket, so seed 1 meets the last seed first and
// the top two seeds can meet only in the final. With two leagues of four
// places: A1 v B4, B2 v A3, B1 v A4, A2 v B3.
type Cup struct {
	ID              ids.CompetitionID
	Name            string
	Qualifiers      []Qualifier
	FirstRoundDelay sim.Duration
	RoundInterval   sim.Duration

	// Match rules for the cup's fixtures. Every match needs a winner:
	// a draw is decided by penalties.
	MaxSubstitutions uint8
	MaxBench         uint8
}

// Entrants is the number of teams in each edition.
func (c Cup) Entrants() int {
	n := 0
	for _, q := range c.Qualifiers {
		n += q.Places
	}
	return n
}

// Validate checks the definition on its own: a power of two of at least two
// qualifiers from distinct leagues, and positive timing. Whether each
// league exists and has enough entrants is checked by the application.
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
func DefaultCups() []Cup {
	return []Cup{{
		ID:               3,
		Name:             "Continental Cup",
		Qualifiers:       []Qualifier{{League: 1, Places: 4}, {League: 2, Places: 4}},
		FirstRoundDelay:  2 * sim.Week,
		RoundInterval:    sim.Week,
		MaxSubstitutions: 3,
		MaxBench:         7,
	}}
}
