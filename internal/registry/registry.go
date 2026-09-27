// Package registry owns world identities: clubs, teams and players' names
// and birth dates.
//
// Player IDs come from one allocator and are never removed or reused: a
// retired player keeps their identity. It holds no football rules. Which club employs a player belongs to the
// employment module; playing ability belongs to players.
package registry

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/sim"
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

// Player is a player's identity, not their ability or employer. Born is
// the start of their birth day (in the career calendar).
type Player struct {
	ID        ids.PlayerID
	FirstName string
	LastName  string
	Born      sim.GameInstant
}

func (p Player) FullName() string { return p.FirstName + " " + p.LastName }

// Init is the initial registry state. LastPlayer is the player ID
// allocator: the highest ID ever issued, at least every player's ID.
type Init struct {
	Clubs      []Club
	Teams      []Team
	Players    []Player
	LastPlayer ids.PlayerID
}

// ErrStalePlan: the registry changed after the plan was made.
var ErrStalePlan = errors.New("registry: plan is stale")

// Registry is the authoritative identity store. Queries return copies.
type Registry struct {
	clubs      []Club // sorted by ID
	teams      []Team // sorted by ID
	players    []Player
	lastPlayer ids.PlayerID
	clubIdx    map[ids.ClubID]int
	teamIdx    map[ids.TeamID]int
	plIdx      map[ids.PlayerID]int
	senior     map[ids.ClubID]ids.TeamID
	generation uint64 // increments on every Apply; plans are tied to one
}

// New validates init and returns a registry holding its own copy.
func New(init Init) (*Registry, error) {
	r := &Registry{
		clubs:      slices.Clone(init.Clubs),
		teams:      slices.Clone(init.Teams),
		players:    slices.Clone(init.Players),
		lastPlayer: init.LastPlayer,
		clubIdx:    make(map[ids.ClubID]int, len(init.Clubs)),
		teamIdx:    make(map[ids.TeamID]int, len(init.Teams)),
		plIdx:      make(map[ids.PlayerID]int, len(init.Players)),
		senior:     make(map[ids.ClubID]ids.TeamID, len(init.Clubs)),
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
		if err := p.validate(r.lastPlayer); err != nil {
			return nil, err
		}
		if _, dup := r.plIdx[p.ID]; dup {
			return nil, fmt.Errorf("registry: duplicate player ID %d", p.ID)
		}
		r.plIdx[p.ID] = i
	}
	return r, nil
}

func (p Player) validate(last ids.PlayerID) error {
	switch {
	case !p.ID.Valid() || p.ID > last:
		return fmt.Errorf("registry: player ID %d is invalid or above the allocator %d", p.ID, last)
	case p.FirstName == "" || p.LastName == "":
		return fmt.Errorf("registry: player %d has an empty name", p.ID)
	case !p.Born.Valid():
		return fmt.Errorf("registry: player %d birth instant %d outside the supported range", p.ID, p.Born)
	}
	return nil
}

// Plan is a validated set of new players, applied by Apply.
type Plan struct {
	generation uint64
	players    []Player
}

// PlanPlayers validates new player identities without changing the
// registry. Their IDs must continue the allocator in order: LastPlayer+1,
// LastPlayer+2, and so on, so an ID is never issued twice.
func (r *Registry) PlanPlayers(add []Player) (Plan, error) {
	next := r.lastPlayer
	for _, p := range add {
		next++
		if p.ID != next {
			return Plan{}, fmt.Errorf("registry: new player ID %d, the allocator issues %d", p.ID, next)
		}
		if err := p.validate(next); err != nil {
			return Plan{}, err
		}
	}
	return Plan{generation: r.generation, players: slices.Clone(add)}, nil
}

// Apply commits a plan. It fails, changing nothing, with ErrStalePlan if the
// registry changed after the plan was made.
func (r *Registry) Apply(p Plan) error {
	if p.generation != r.generation {
		return fmt.Errorf("%w: made at generation %d, registry at %d", ErrStalePlan, p.generation, r.generation)
	}
	for _, pl := range p.players {
		r.plIdx[pl.ID] = len(r.players)
		r.players = append(r.players, pl) // IDs ascend past every existing one
		r.lastPlayer = pl.ID
	}
	r.generation++
	return nil
}

// LastPlayer returns the highest player ID ever issued.
func (r *Registry) LastPlayer() ids.PlayerID { return r.lastPlayer }

// Snapshot exports the registry's authoritative state as fresh copies.
// New(Snapshot()) restores an equivalent registry.
func (r *Registry) Snapshot() Init {
	return Init{Clubs: r.Clubs(), Teams: r.Teams(), Players: r.Players(), LastPlayer: r.lastPlayer}
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
