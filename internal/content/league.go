package content

import (
	"fmt"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
)

// LeagueVersion identifies the definition returned by DefaultLeague. It is
// separate from Version so league changes do not alter generated worlds.
const LeagueVersion = 1

// League defines a double round-robin league competition and its first
// season's scheduling inputs. FirstKickoff is UTC civil time; the career
// calendar converts it to a game instant.
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

func (l League) Validate() error {
	if !l.ID.Valid() || l.Name == "" || l.Entrants <= 0 || l.RoundInterval <= 0 || l.MaxSubstitutions > l.MaxBench {
		return fmt.Errorf("content: invalid league definition %+v", l)
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

		MaxSubstitutions: 3,
		MaxBench:         7,
	}
}
