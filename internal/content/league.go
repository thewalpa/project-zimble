package content

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// LeagueVersion identifies the definition returned by DefaultLeague. It is
// separate from Version so league changes do not alter generated worlds.
const LeagueVersion = 2

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

// DefaultLeague returns the built-in league for the generated senior teams.
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
