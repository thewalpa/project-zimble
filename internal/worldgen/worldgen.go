// Package worldgen deterministically generates an initial world snapshot
// from content definitions and a seed.
//
// The snapshot is plain generated data. The application loads it into the
// owning modules; nothing reads the snapshot as runtime state afterwards.
package worldgen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
)

// Version identifies the generation algorithm. Bump it whenever the same
// seed and content would produce a different snapshot.
const Version = 1

// Snapshot is a generated initial world. Slices are in ascending ID order.
type Snapshot struct {
	Seed             random.Seed
	GeneratorVersion int
	RandomVersion    int
	ContentVersion   int
	Clubs            []registry.Club
	Teams            []registry.Team
	Players          []registry.Player
	Profiles         []players.Profile
	Assignments      []employment.Assignment
}

// Generate builds a snapshot. Identical definitions, seed and package
// versions always produce an identical snapshot.
//
// IDs are allocated sequentially from 1 in generation order. Each club gets
// one senior team; each player draws from a stream keyed by its own ID, so
// adding fields to one player's generation does not shift other players.
func Generate(defs content.Definitions, seed random.Seed) (Snapshot, error) {
	if err := defs.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("worldgen: %w", err)
	}
	squad := defs.SquadSize()
	s := Snapshot{
		Seed:             seed,
		GeneratorVersion: Version,
		RandomVersion:    random.Version,
		ContentVersion:   defs.Version,
		Clubs:            make([]registry.Club, 0, defs.ClubCount),
		Teams:            make([]registry.Team, 0, defs.ClubCount),
		Players:          make([]registry.Player, 0, defs.ClubCount*squad),
		Profiles:         make([]players.Profile, 0, defs.ClubCount*squad),
		Assignments:      make([]employment.Assignment, 0, defs.ClubCount*squad),
	}

	clubRNG := random.Derive(seed, "worldgen/clubs", Version)
	towns := clubRNG.Perm(len(defs.Towns))
	var nextPlayer ids.PlayerID
	for i := range defs.ClubCount {
		town := defs.Towns[towns[i]]
		clubID := ids.ClubID(i + 1)
		teamID := ids.TeamID(i + 1)
		suffix := defs.ClubSuffixes[clubRNG.IntN(len(defs.ClubSuffixes))]
		s.Clubs = append(s.Clubs, registry.Club{ID: clubID, Name: town.Name + " " + suffix, ShortName: town.Short})
		s.Teams = append(s.Teams, registry.Team{ID: teamID, Club: clubID, Kind: registry.TeamSenior})

		for _, q := range defs.Roster {
			profile, _ := defs.Profile(q.Position) // presence checked by Validate
			for range q.Count {
				nextPlayer++
				rng := random.Derive(seed, "worldgen/player", Version, uint64(nextPlayer))
				s.Players = append(s.Players, registry.Player{
					ID:        nextPlayer,
					FirstName: defs.FirstNames[rng.IntN(len(defs.FirstNames))],
					LastName:  defs.LastNames[rng.IntN(len(defs.LastNames))],
				})
				var attrs players.Attributes
				for a, r := range profile.Ranges {
					attrs[a] = players.Rating(rng.IntRange(int(r.Min), int(r.Max)))
				}
				s.Profiles = append(s.Profiles, players.Profile{Player: nextPlayer, Position: q.Position, Attributes: attrs})
				s.Assignments = append(s.Assignments, employment.Assignment{Player: nextPlayer, Club: clubID, Team: teamID})
			}
		}
	}
	return s, nil
}

// Fingerprint returns a SHA-256 over a canonical text encoding of the
// snapshot. It identifies generated content independently of Go's struct
// layout or map ordering.
func (s Snapshot) Fingerprint() string {
	h := sha256.New()
	s.writeCanonical(h)
	return hex.EncodeToString(h.Sum(nil))
}

func (s Snapshot) writeCanonical(w io.Writer) {
	fmt.Fprintf(w, "world seed=%d gen=%d rand=%d content=%d\n",
		s.Seed, s.GeneratorVersion, s.RandomVersion, s.ContentVersion)
	for _, c := range s.Clubs {
		fmt.Fprintf(w, "club %d %q %q\n", c.ID, c.Name, c.ShortName)
	}
	for _, t := range s.Teams {
		fmt.Fprintf(w, "team %d club=%d kind=%d\n", t.ID, t.Club, t.Kind)
	}
	for _, p := range s.Players {
		fmt.Fprintf(w, "player %d %q %q\n", p.ID, p.FirstName, p.LastName)
	}
	for _, p := range s.Profiles {
		fmt.Fprintf(w, "profile %d pos=%d attrs=%v\n", p.Player, p.Position, p.Attributes)
	}
	for _, a := range s.Assignments {
		fmt.Fprintf(w, "assignment %d club=%d team=%d\n", a.Player, a.Club, a.Team)
	}
}
