package app

import (
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/players"
)

// PositionCount is the number of squad players at a position.
type PositionCount struct {
	Position players.Position
	Count    int
}

// ClubSummary is a derived read view of one club's senior squad.
type ClubSummary struct {
	ID         ids.ClubID
	Name       string
	ShortName  string
	SeniorTeam ids.TeamID
	Players    int
	Positions  []PositionCount // in players.Positions order
	// Mean of players' OverallTenths, in tenths, rounded half up.
	AverageOverallTenths int
}

// Summary is a derived, deterministic view of the whole world.
type Summary struct {
	Seed             random.Seed
	GeneratorVersion int
	RandomVersion    int
	ContentVersion   int
	Fingerprint      string
	Clubs            int
	Teams            int
	Players          int
	ClubRows         []ClubSummary // ascending club ID
}

// Summary composes a view from module queries. It does not mutate state.
func (w *World) Summary() Summary {
	clubs := w.registry.Clubs()
	s := Summary{
		Seed:             w.seed,
		GeneratorVersion: w.generatorVersion,
		RandomVersion:    w.randomVersion,
		ContentVersion:   w.contentVersion,
		Fingerprint:      w.fingerprint,
		Clubs:            len(clubs),
		Teams:            len(w.registry.Teams()),
		Players:          len(w.registry.Players()),
		ClubRows:         make([]ClubSummary, 0, len(clubs)),
	}
	for _, c := range clubs {
		team, _ := w.registry.SeniorTeam(c.ID)
		squad := w.employment.Squad(team)
		counts := map[players.Position]int{}
		total := 0
		for _, id := range squad {
			p, _ := w.players.Profile(id)
			counts[p.Position]++
			total += p.OverallTenths()
		}
		row := ClubSummary{
			ID:         c.ID,
			Name:       c.Name,
			ShortName:  c.ShortName,
			SeniorTeam: team,
			Players:    len(squad),
		}
		for _, pos := range players.Positions() {
			row.Positions = append(row.Positions, PositionCount{Position: pos, Count: counts[pos]})
		}
		if n := len(squad); n > 0 {
			row.AverageOverallTenths = (2*total + n) / (2 * n)
		}
		s.ClubRows = append(s.ClubRows, row)
	}
	return s
}

// SquadPlayer is a derived view of one senior-squad player, composed from
// the registry (name), players (position, attributes) and medical
// (condition, per 10,000).
type SquadPlayer struct {
	Player        ids.PlayerID
	Name          string
	Position      players.Position
	Attributes    players.Attributes
	OverallTenths int
	Condition     uint16
}

// Squad returns a club's senior squad in ascending player ID order, or false
// for an unknown club. Clients use it to build lineups. Read-only.
func (w *World) Squad(club ids.ClubID) ([]SquadPlayer, bool) {
	team, ok := w.registry.SeniorTeam(club)
	if !ok {
		return nil, false
	}
	var out []SquadPlayer
	for _, id := range w.employment.Squad(team) {
		row := SquadPlayer{Player: id}
		if p, ok := w.registry.Player(id); ok {
			row.Name = p.FullName()
		}
		if p, ok := w.players.Profile(id); ok {
			row.Position, row.Attributes, row.OverallTenths = p.Position, p.Attributes, p.OverallTenths()
		}
		row.Condition, _ = w.medical.Condition(id)
		out = append(out, row)
	}
	return out, true
}
