package app

import (
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/employment"
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
	// Mean of players' Overall (1..100), rounded half up.
	AverageOverall int
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
	Players          int           // active players
	FreeAgents       int           // active players without a club
	Retired          int           // retired players
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
		Players:          len(w.activePlayers()),
		Retired:          len(w.registry.Players()) - len(w.activePlayers()),
		FreeAgents:       len(w.freeAgentPool()),
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
			total += p.Overall()
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
			row.AverageOverall = (2*total + n) / (2 * n)
		}
		s.ClubRows = append(s.ClubRows, row)
	}
	return s
}

// SquadPlayer is a derived view of one player, composed from the registry
// (name; age in whole years now, from the birth date), players (position,
// attributes and overall, 1..100), medical (condition, 0..100) and
// employment (contract; zero for a free agent). Demand is the weekly wage
// the player asks for in a new contract.
type SquadPlayer struct {
	Player     ids.PlayerID
	Name       string
	Age        int
	Position   players.Position
	Attributes players.Attributes
	Overall    int
	Condition  uint8
	Contract   employment.Contract
	Demand     money.Money
	Value      money.Money // employed players: the fee his club sells for (see sellingPrice)
	Payoff     money.Money // employed players: what releasing him costs now, the rest of his contract
	Listed     bool        // on the transfer list
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
		out = append(out, w.squadPlayer(id))
	}
	return out, true
}

func (w *World) squadPlayer(id ids.PlayerID) SquadPlayer {
	row := SquadPlayer{Player: id}
	if p, ok := w.registry.Player(id); ok {
		row.Name = p.FullName()
		row.Age, _ = w.calendar.WholeYears(p.Born, w.Now())
	}
	if p, ok := w.players.Profile(id); ok {
		row.Position, row.Attributes, row.Overall = p.Position, p.Attributes, p.Overall()
		row.Demand = w.defs.Economy.Demand(row.Overall)
	}
	row.Condition, _ = w.medical.Condition(id)
	if a, ok := w.employment.Assignment(id); ok {
		row.Contract = a.Contract
		row.Value, _ = w.sellingPrice(id, w.Now())
		row.Payoff, _ = releaseCost(a.Contract, w.Now())
		_, row.Listed = w.transfers.Listing(id)
	}
	return row
}

// Content returns a copy of the career's content definitions: the rules
// pinned when the career began (roster quotas, contract lengths, economy).
// Read-only.
func (w *World) Content() content.Definitions { return w.defs.Clone() }

// PlayerName returns a player's full name from the registry, or false if the
// player is unknown. Read-only.
func (w *World) PlayerName(id ids.PlayerID) (string, bool) {
	if p, ok := w.registry.Player(id); ok {
		return p.FullName(), true
	}
	return "", false
}
