// Package registry owns world identities: clubs, teams and players' names.
//
// It holds no football rules. Which club employs a player belongs to the
// employment module; playing ability belongs to players.
package registry

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
)

// TeamKind distinguishes a club's teams. Values are durable; never reorder.
type TeamKind uint8

const TeamSenior TeamKind = 1

func (k TeamKind) Valid() bool { return k == TeamSenior }

func (k TeamKind) String() string {
	if k == TeamSenior {
		return "senior"
	}
	return fmt.Sprintf("TeamKind(%d)", uint8(k))
}

type Club struct {
	ID        ids.ClubID
	Name      string
	ShortName string
}

type Team struct {
	ID   ids.TeamID
	Club ids.ClubID
	Kind TeamKind
}

// Player is a player's identity, not their ability or employer.
type Player struct {
	ID        ids.PlayerID
	FirstName string
	LastName  string
}

func (p Player) FullName() string { return p.FirstName + " " + p.LastName }

// Init is the initial registry state.
type Init struct {
	Clubs   []Club
	Teams   []Team
	Players []Player
}

// Registry is the authoritative identity store. Queries return copies.
type Registry struct {
	clubs   []Club // sorted by ID
	teams   []Team // sorted by ID
	players []Player
	clubIdx map[ids.ClubID]int
	teamIdx map[ids.TeamID]int
	plIdx   map[ids.PlayerID]int
	senior  map[ids.ClubID]ids.TeamID
}

// New validates init and returns a registry holding its own copy.
func New(init Init) (*Registry, error) {
	r := &Registry{
		clubs:   slices.Clone(init.Clubs),
		teams:   slices.Clone(init.Teams),
		players: slices.Clone(init.Players),
		clubIdx: make(map[ids.ClubID]int, len(init.Clubs)),
		teamIdx: make(map[ids.TeamID]int, len(init.Teams)),
		plIdx:   make(map[ids.PlayerID]int, len(init.Players)),
		senior:  make(map[ids.ClubID]ids.TeamID, len(init.Clubs)),
	}
	slices.SortFunc(r.clubs, func(a, b Club) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(r.teams, func(a, b Team) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(r.players, func(a, b Player) int { return cmp.Compare(a.ID, b.ID) })

	names := map[string]ids.ClubID{}
	shorts := map[string]ids.ClubID{}
	for i, c := range r.clubs {
		if !c.ID.Valid() {
			return nil, fmt.Errorf("registry: invalid club ID %d", c.ID)
		}
		if _, dup := r.clubIdx[c.ID]; dup {
			return nil, fmt.Errorf("registry: duplicate club ID %d", c.ID)
		}
		if c.Name == "" || c.ShortName == "" {
			return nil, fmt.Errorf("registry: club %d has an empty name", c.ID)
		}
		if other, dup := names[c.Name]; dup {
			return nil, fmt.Errorf("registry: clubs %d and %d share name %q", other, c.ID, c.Name)
		}
		if other, dup := shorts[c.ShortName]; dup {
			return nil, fmt.Errorf("registry: clubs %d and %d share short name %q", other, c.ID, c.ShortName)
		}
		names[c.Name], shorts[c.ShortName] = c.ID, c.ID
		r.clubIdx[c.ID] = i
	}

	for i, t := range r.teams {
		if !t.ID.Valid() {
			return nil, fmt.Errorf("registry: invalid team ID %d", t.ID)
		}
		if _, dup := r.teamIdx[t.ID]; dup {
			return nil, fmt.Errorf("registry: duplicate team ID %d", t.ID)
		}
		if _, ok := r.clubIdx[t.Club]; !ok {
			return nil, fmt.Errorf("registry: team %d references unknown club %d", t.ID, t.Club)
		}
		if !t.Kind.Valid() {
			return nil, fmt.Errorf("registry: team %d has invalid kind %d", t.ID, t.Kind)
		}
		if t.Kind == TeamSenior {
			if other, dup := r.senior[t.Club]; dup {
				return nil, fmt.Errorf("registry: club %d has two senior teams (%d, %d)", t.Club, other, t.ID)
			}
			r.senior[t.Club] = t.ID
		}
		r.teamIdx[t.ID] = i
	}
	for _, c := range r.clubs {
		if _, ok := r.senior[c.ID]; !ok {
			return nil, fmt.Errorf("registry: club %d has no senior team", c.ID)
		}
	}

	for i, p := range r.players {
		if !p.ID.Valid() {
			return nil, fmt.Errorf("registry: invalid player ID %d", p.ID)
		}
		if _, dup := r.plIdx[p.ID]; dup {
			return nil, fmt.Errorf("registry: duplicate player ID %d", p.ID)
		}
		if p.FirstName == "" || p.LastName == "" {
			return nil, fmt.Errorf("registry: player %d has an empty name", p.ID)
		}
		r.plIdx[p.ID] = i
	}
	return r, nil
}

// Snapshot exports the registry's authoritative state as fresh copies.
// New(Snapshot()) restores an equivalent registry.
func (r *Registry) Snapshot() Init {
	return Init{Clubs: r.Clubs(), Teams: r.Teams(), Players: r.Players()}
}

// Clubs returns all clubs in ascending ID order.
func (r *Registry) Clubs() []Club { return slices.Clone(r.clubs) }

// Teams returns all teams in ascending ID order.
func (r *Registry) Teams() []Team { return slices.Clone(r.teams) }

// Players returns all player identities in ascending ID order.
func (r *Registry) Players() []Player { return slices.Clone(r.players) }

func (r *Registry) Club(id ids.ClubID) (Club, bool) {
	i, ok := r.clubIdx[id]
	if !ok {
		return Club{}, false
	}
	return r.clubs[i], true
}

func (r *Registry) Team(id ids.TeamID) (Team, bool) {
	i, ok := r.teamIdx[id]
	if !ok {
		return Team{}, false
	}
	return r.teams[i], true
}

func (r *Registry) Player(id ids.PlayerID) (Player, bool) {
	i, ok := r.plIdx[id]
	if !ok {
		return Player{}, false
	}
	return r.players[i], true
}

// SeniorTeam returns the club's senior team.
func (r *Registry) SeniorTeam(club ids.ClubID) (ids.TeamID, bool) {
	t, ok := r.senior[club]
	return t, ok
}
