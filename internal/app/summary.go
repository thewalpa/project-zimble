package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
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
	Nation     string // the country the club plays in
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
			Nation:     w.nationName(c.Nation),
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
	Player      ids.PlayerID
	Name        string
	Nationality string
	Age         int
	Position    players.Position
	Attributes  players.Attributes
	Overall     int
	Condition   uint8
	DaysOut     uint16 // injured: the recovery days he still misses; zero when fit
	Contract    employment.Contract
	Demand      money.Money
	Value       money.Money // employed players: the fee his club sells for (see sellingPrice)
	Payoff      money.Money // employed players: what releasing him costs now, the rest of his contract
	Listed      bool        // on the transfer list
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

// ErrUnknownPlayer means an observation request names an unregistered player.
var ErrUnknownPlayer = errors.New("app: unknown player")

// PlayerObservation is the detached information a club knows about a player.
// Today's knowledge policy reveals exact ratings, condition and contract
// demands to every club. It carries no module profile or match input. Match
// simulation and development must continue to read their authoritative stores.
// Asking prices and release costs belong to negotiation and football rules,
// respectively; they are not player knowledge.
type PlayerObservation struct {
	Player      ids.PlayerID
	Name        string
	Nationality string
	Age         int
	Club        ids.ClubID // zero for a free agent or retired player
	Team        ids.TeamID // zero when not employed
	Position    players.Position
	Attributes  players.Attributes // exact, 1..100
	Overall     int                // exact, 1..100
	Condition   uint8              // 0..100; zero for a retired player
	DaysOut     uint16
	Contract    employment.Contract // zero when not employed
	Demand      money.Money         // weekly wage requested in a new contract
	Retired     bool
}

// ClubObservations identifies the observing club and the world state from
// which its detached player information was read. AsOf is the query instant,
// not a stored scouting report date. Players are in ascending player ID order.
type ClubObservations struct {
	Observer ids.ClubID
	Revision Revision
	AsOf     sim.GameInstant
	Players  []PlayerObservation
}

// ObservePlayers returns the requested players as known by club, independent
// of whether its manager is human or AI. Club must be registered and nonzero;
// every requested player must be registered (including retired players).
// Duplicate IDs are returned once; an empty request returns no players.
// Neither the request nor the world is changed. Invalid requests return no
// partial observations. Use this contract for manager information and project
// it into detached AI inputs; never use it to supply match physics.
//
// There is currently no stored scouting knowledge or uncertainty. Introducing
// either requires a versioned, saved policy here for both controllers before
// callers may hide information in their presentation or decision inputs.
func (w *World) ObservePlayers(club ids.ClubID, requested []ids.PlayerID) (ClubObservations, error) {
	if _, ok := w.registry.Club(club); !ok {
		return ClubObservations{}, fmt.Errorf("%w: %d", ErrUnknownClub, club)
	}
	requested = slices.Clone(requested)
	slices.Sort(requested)
	requested = slices.Compact(requested)
	for _, id := range requested {
		if _, ok := w.registry.Player(id); !ok {
			return ClubObservations{}, fmt.Errorf("%w: %d", ErrUnknownPlayer, id)
		}
	}
	out := ClubObservations{Observer: club, Revision: w.Revision(), AsOf: w.Now()}
	for _, id := range requested {
		out.Players = append(out.Players, w.playerObservation(id))
	}
	return out, nil
}

// playerObservation is the current exact-knowledge projection shared by
// club observations and the existing player views.
func (w *World) playerObservation(id ids.PlayerID) PlayerObservation {
	row := PlayerObservation{Player: id}
	if p, ok := w.registry.Player(id); ok {
		row.Name = p.FullName()
		row.Nationality = w.nationName(p.Nationality)
		row.Age, _ = w.calendar.WholeYears(p.Born, w.Now())
	}
	if p, ok := w.players.Profile(id); ok {
		row.Position, row.Attributes, row.Overall = p.Position, p.Attributes, p.Overall()
		row.Demand = w.defs.Economy.Demand(row.Overall)
		row.Retired = p.Retired
	}
	row.Condition, _ = w.medical.Condition(id)
	row.DaysOut, _ = w.medical.DaysOut(id)
	if a, ok := w.employment.Assignment(id); ok {
		row.Club, row.Team, row.Contract = a.Club, a.Team, a.Contract
	}
	return row
}

func (w *World) squadPlayer(id ids.PlayerID) SquadPlayer {
	p := w.playerObservation(id)
	row := SquadPlayer{
		Player: p.Player, Name: p.Name, Nationality: p.Nationality, Age: p.Age,
		Position: p.Position, Attributes: p.Attributes, Overall: p.Overall,
		Condition: p.Condition, DaysOut: p.DaysOut, Contract: p.Contract, Demand: p.Demand,
	}
	if p.Club != 0 {
		row.Value, _ = w.sellingPrice(id, w.Now())
		row.Payoff, _ = releaseCost(p.Contract, w.Now())
		_, row.Listed = w.transfers.Listing(id)
	}
	return row
}

// Content returns a copy of the career's content definitions: the rules
// pinned when the career began (roster quotas, contract lengths, economy).
// Read-only.
func (w *World) Content() content.Definitions { return w.defs.Clone() }

func (w *World) nationName(id ids.NationID) string {
	n, _ := w.registry.Nation(id)
	return n.Name
}

// PlayerName returns a player's full name from the registry, or false if the
// player is unknown. Read-only.
func (w *World) PlayerName(id ids.PlayerID) (string, bool) {
	if p, ok := w.registry.Player(id); ok {
		return p.FullName(), true
	}
	return "", false
}
