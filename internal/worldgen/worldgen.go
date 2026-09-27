// Package worldgen deterministically generates an initial world snapshot
// from content definitions and a seed, and the youth players who join the
// world later (see Youth).
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
	"github.com/thewalpa/project-zimble/internal/core/money"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
)

// Version identifies the generation algorithm. Bump it whenever the same
// seed and content would produce a different snapshot. Version 2 added
// contract terms; version 3 added birth dates.
const Version = 3

// streamVersion keys the club and player streams: the generator version
// that last changed their existing draws. Version 3 only appended a draw to
// each player's stream, which leaves the earlier draws (names, attributes,
// contracts) as they were. Set it to Version when a change alters an
// existing draw.
const streamVersion = 2

// YouthVersion identifies youth generation (Youth). Bump it whenever the
// same definitions, seed, ID, position and instant would produce a
// different player.
const YouthVersion = 1

// birthDays returns the range of days before an instant on which a person
// is ages[0]..ages[1] whole years old: ages[0] years never exceed 366 days
// each, and ages[1]+1 years always exceed 365 days each.
func birthDays(ages [2]int) (lo, hi int) { return 366 * ages[0], 365*(ages[1]+1) - 1 }

// ContractTerms are a player's generated contract terms. Years is counted
// from the career start; the application anchors it to the calendar, which
// generation does not know.
type ContractTerms struct {
	Player     ids.PlayerID
	Years      int
	WeeklyWage money.Money
}

// Snapshot is a generated initial world. Slices are in ascending ID order.
// Players' birth instants are relative to the career start (instant 0), at
// which each is defs.Ages[0]..defs.Ages[1] whole years old.
type Snapshot struct {
	Seed             random.Seed
	GeneratorVersion int
	RandomVersion    int
	ContentVersion   int
	Clubs            []registry.Club
	Teams            []registry.Team
	Players          []registry.Player
	Profiles         []players.Profile
	Assignments      []employment.Assignment // Contract is left zero; see Contracts
	Contracts        []ContractTerms
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

	clubRNG := random.Derive(seed, "worldgen/clubs", streamVersion)
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
				rng := random.Derive(seed, "worldgen/player", streamVersion, uint64(nextPlayer))
				player := registry.Player{
					ID:        nextPlayer,
					FirstName: defs.FirstNames[rng.IntN(len(defs.FirstNames))],
					LastName:  defs.LastNames[rng.IntN(len(defs.LastNames))],
				}
				var attrs players.Attributes
				for a, r := range profile.Ranges {
					attrs[a] = players.Rating(rng.IntRange(int(r.Min), int(r.Max)))
				}
				profile := players.Profile{Player: nextPlayer, Position: q.Position, Attributes: attrs}
				s.Profiles = append(s.Profiles, profile)
				s.Assignments = append(s.Assignments, employment.Assignment{Player: nextPlayer, Club: clubID, Team: teamID})
				// Drawn after the attributes, from the same player stream.
				econ := defs.Economy
				years := rng.IntRange(econ.ContractYears[0], econ.ContractYears[1])
				variation := rng.IntRange(-econ.WageVariationPct, econ.WageVariationPct)
				s.Contracts = append(s.Contracts, ContractTerms{
					Player: nextPlayer, Years: years, WeeklyWage: econ.Wage(profile.Overall(), variation),
				})
				// Version 3: drawn last, so the draws above are unchanged.
				player.Born = -sim.GameInstant(rng.IntRange(birthDays(defs.Ages))) * sim.GameInstant(sim.Day)
				s.Players = append(s.Players, player)
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
		fmt.Fprintf(w, "player %d %q %q born=%d\n", p.ID, p.FirstName, p.LastName, p.Born)
	}
	for _, p := range s.Profiles {
		fmt.Fprintf(w, "profile %d pos=%d attrs=%v\n", p.Player, p.Position, p.Attributes)
	}
	for _, a := range s.Assignments {
		fmt.Fprintf(w, "assignment %d club=%d team=%d\n", a.Player, a.Club, a.Team)
	}
	for _, c := range s.Contracts {
		fmt.Fprintf(w, "contract %d years=%d wage=%d\n", c.Player, c.Years, c.WeeklyWage)
	}
}

// Youth generates a player who joins a club from its youth ranks at instant
// at: player ID id, at position pos, with a name from the content's pools,
// defs.Youth.Ages[0]..defs.Youth.Ages[1] whole years old at `at`, and
// attributes from the youth ranges (content.Youth.Range). Every draw comes
// from a stream keyed by the ID, so the player does not depend on who else
// joins.
func Youth(defs content.Definitions, seed random.Seed, id ids.PlayerID, pos players.Position, at sim.GameInstant) (registry.Player, players.Profile, error) {
	profile, ok := defs.Profile(pos)
	if !ok || !id.Valid() {
		return registry.Player{}, players.Profile{}, fmt.Errorf("worldgen: no youth player %d at position %s", id, pos)
	}
	rng := random.Derive(seed, "worldgen/youth", YouthVersion, uint64(id))
	p := registry.Player{
		ID:        id,
		FirstName: defs.FirstNames[rng.IntN(len(defs.FirstNames))],
		LastName:  defs.LastNames[rng.IntN(len(defs.LastNames))],
	}
	var attrs players.Attributes
	for a, r := range profile.Ranges {
		r = defs.Youth.Range(r)
		attrs[a] = players.Rating(rng.IntRange(int(r.Min), int(r.Max)))
	}
	born, err := at.Add(-sim.Duration(rng.IntRange(birthDays(defs.Youth.Ages))) * sim.Day)
	if err != nil {
		return registry.Player{}, players.Profile{}, fmt.Errorf("worldgen: youth player %d: %w", id, err)
	}
	p.Born = born
	return p, players.Profile{Player: id, Position: pos, Attributes: attrs}, nil
}
