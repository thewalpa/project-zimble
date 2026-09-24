// Package ids defines the durable typed identifiers shared by all modules.
//
// Each entity kind has its own type so that a ClubID cannot be passed where a
// TeamID is expected. The zero value of every ID is invalid.
package ids

type (
	ClubID   uint64
	TeamID   uint64
	PlayerID uint64
)

func (id ClubID) Valid() bool   { return id != 0 }
func (id TeamID) Valid() bool   { return id != 0 }
func (id PlayerID) Valid() bool { return id != 0 }

type (
	CompetitionID uint64
	FixtureID     uint64
)

func (id CompetitionID) Valid() bool { return id != 0 }
func (id FixtureID) Valid() bool     { return id != 0 }
