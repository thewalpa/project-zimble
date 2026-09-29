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
// contract terms; version 3 added birth dates; version 4 generates clubs
// nation by nation; version 5 adds each nation's lower divisions; version 6
// adds the five match attributes (see matchAttributes); version 7 adds
// nations and nationalities (see drawNationality).
const Version = 7

// streamVersion keys the club and player streams: the generator version
// that last changed their existing draws. Version 3 only appended a draw to
// each player's stream, which leaves the earlier draws (names, attributes,
// contracts) as they were; version 4 keeps the first nation's club stream
// and adds one per further nation, and version 5 adds one per lower division
// after every top division. Version 6 appended the match attributes to each
// player's stream. Version 7 appended the nationality. Set it to Version when a change alters an existing draw.
const streamVersion = 2

// YouthVersion identifies youth generation (Youth). Bump it whenever the
// same definitions, seed, ID, position and instant would produce a
// different player. Version 2 added the match attributes; version 3 the
// nationality.
const YouthVersion = 3

// youthStreamVersion keys the youth streams: the youth version that last
// changed an existing draw. Versions 2 and 3 only appended draws.
const youthStreamVersion = 1

// matchAttributes is the first of the attributes added in generator
// version 6 (Dribbling..Positioning). They are drawn last in each player's
// and each youth player's stream, after the birth date, so every earlier
// draw is unchanged; the attributes before it are drawn first, in order.
const matchAttributes = players.Dribbling

// NationID is the ID of the i'th of the content's nations (in content
// order); IDs start at 1.
func NationID(i int) ids.NationID { return ids.NationID(i + 1) }

// drawNationality draws a player's nationality: their club's nation home,
// except foreignPct percent of the time, when it is one of the other nations
// (equally likely). It always makes the same two draws, so the stream after
// it does not depend on the number of nations.
func drawNationality(rng *random.Stream, home ids.NationID, nations, foreignPct int) ids.NationID {
	roll := rng.IntN(100)
	pick := rng.IntN(max(nations-1, 1))
	if nations < 2 || roll >= foreignPct {
		return home
	}
	if pick >= int(home)-1 { // skip the home nation
		pick++
	}
	return NationID(pick)
}

// drawAttributes draws the attributes from..to-1, each from its range.
func drawAttributes(rng *random.Stream, attrs *players.Attributes, ranges [players.NumAttributes]content.Range, from, to players.Attribute) {
	for a := from; a < to; a++ {
		attrs[a] = players.Rating(rng.IntRange(int(ranges[a].Min), int(ranges[a].Max)))
	}
}

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
	Nations          []registry.Nation
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
// IDs are allocated sequentially from 1 in generation order: every nation's
// top division, then every nation's second division, and so on. Each
// division's clubs draw their towns and suffixes from a stream of their own
// (the first nation's top division uses the stream of earlier versions, so
// its clubs are unchanged). Each club gets one senior team; each player
// draws from a stream keyed by its own ID, so adding fields to one player's
// generation does not shift other players.
func Generate(defs content.Definitions, seed random.Seed) (Snapshot, error) {
	if err := defs.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("worldgen: %w", err)
	}
	squad, clubs := defs.SquadSize(), defs.ClubCount()
	s := Snapshot{
		Seed:             seed,
		GeneratorVersion: Version,
		RandomVersion:    random.Version,
		ContentVersion:   defs.Version,
		Nations:          make([]registry.Nation, 0, len(defs.Nations)),
		Clubs:            make([]registry.Club, 0, clubs),
		Teams:            make([]registry.Team, 0, clubs),
		Players:          make([]registry.Player, 0, clubs*squad),
		Profiles:         make([]players.Profile, 0, clubs*squad),
		Assignments:      make([]employment.Assignment, 0, clubs*squad),
	}

	for i, nation := range defs.Nations {
		s.Nations = append(s.Nations, registry.Nation{ID: NationID(i), Name: nation.Name})
	}
	var nextPlayer ids.PlayerID
	var nextClub int
	// Tier by tier, nation by nation: every nation's top division comes
	// first, so adding a lower tier leaves the top divisions as they were.
	for tier := range defs.Nations[0].Divisions {
		for ni, nation := range defs.Nations {
			division := nation.Divisions[tier]
			// The first nation's top division keeps the stream of earlier
			// versions; each other division has one keyed by nation and tier.
			clubRNG := random.Derive(seed, "worldgen/clubs", streamVersion)
			switch {
			case tier > 0:
				clubRNG = random.Derive(seed, "worldgen/clubs", streamVersion, uint64(ni), uint64(tier))
			case ni > 0:
				clubRNG = random.Derive(seed, "worldgen/clubs", streamVersion, uint64(ni))
			}
			towns := clubRNG.Perm(len(division.Towns))
			for i := range division.Clubs {
				town := division.Towns[towns[i]]
				nextClub++
				clubID := ids.ClubID(nextClub)
				teamID := ids.TeamID(nextClub)
				suffix := defs.ClubSuffixes[clubRNG.IntN(len(defs.ClubSuffixes))]
				s.Clubs = append(s.Clubs, registry.Club{ID: clubID, Name: town.Name + " " + suffix, ShortName: town.Short, Nation: NationID(ni)})
				s.Teams = append(s.Teams, registry.Team{ID: teamID, Club: clubID, Kind: registry.TeamSenior})

				for _, q := range defs.Roster {
					pp, _ := defs.Profile(q.Position) // presence checked by Validate
					for range q.Count {
						nextPlayer++
						rng := random.Derive(seed, "worldgen/player", streamVersion, uint64(nextPlayer))
						player := registry.Player{
							ID:        nextPlayer,
							FirstName: defs.FirstNames[rng.IntN(len(defs.FirstNames))],
							LastName:  defs.LastNames[rng.IntN(len(defs.LastNames))],
						}
						var attrs players.Attributes
						drawAttributes(rng, &attrs, pp.Ranges, 0, matchAttributes)
						profile := players.Profile{Player: nextPlayer, Position: q.Position, Attributes: attrs}
						s.Assignments = append(s.Assignments, employment.Assignment{Player: nextPlayer, Club: clubID, Team: teamID})
						// Drawn after the attributes, from the same player stream.
						econ := defs.Economy
						years := rng.IntRange(econ.ContractYears[0], econ.ContractYears[1])
						variation := rng.IntRange(-econ.WageVariationPct, econ.WageVariationPct)
						s.Contracts = append(s.Contracts, ContractTerms{
							Player: nextPlayer, Years: years, WeeklyWage: econ.Wage(profile.Overall(), variation),
						})
						// Version 3: drawn after the contract, so the draws above are unchanged.
						player.Born = -sim.GameInstant(rng.IntRange(birthDays(defs.Ages))) * sim.GameInstant(sim.Day)
						// Version 6: drawn after the birth date, for the same reason.
						drawAttributes(rng, &profile.Attributes, pp.Ranges, matchAttributes, players.NumAttributes)
						// Version 7: drawn last of all.
						player.Nationality = drawNationality(rng, NationID(ni), len(defs.Nations), defs.ForeignPct)
						s.Players = append(s.Players, player)
						s.Profiles = append(s.Profiles, profile)
					}
				}
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
	for _, n := range s.Nations {
		fmt.Fprintf(w, "nation %d %q\n", n.ID, n.Name)
	}
	for _, c := range s.Clubs {
		fmt.Fprintf(w, "club %d %q %q nation=%d\n", c.ID, c.Name, c.ShortName, c.Nation)
	}
	for _, t := range s.Teams {
		fmt.Fprintf(w, "team %d club=%d kind=%d\n", t.ID, t.Club, t.Kind)
	}
	for _, p := range s.Players {
		fmt.Fprintf(w, "player %d %q %q born=%d nation=%d\n", p.ID, p.FirstName, p.LastName, p.Born, p.Nationality)
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
// attributes from the youth ranges (content.Youth.Range) and, unless
// defs.Youth.ForeignPct of the time, the nationality of the joining club's
// nation home. Every draw comes from a stream keyed by the ID, so the player
// does not depend on who else joins.
func Youth(defs content.Definitions, seed random.Seed, id ids.PlayerID, pos players.Position, at sim.GameInstant, home ids.NationID) (registry.Player, players.Profile, error) {
	profile, ok := defs.Profile(pos)
	if !ok || !id.Valid() {
		return registry.Player{}, players.Profile{}, fmt.Errorf("worldgen: no youth player %d at position %s", id, pos)
	}
	if !home.Valid() || int(home) > len(defs.Nations) {
		return registry.Player{}, players.Profile{}, fmt.Errorf("worldgen: youth player %d for unknown nation %d", id, home)
	}
	rng := random.Derive(seed, "worldgen/youth", youthStreamVersion, uint64(id))
	p := registry.Player{
		ID:        id,
		FirstName: defs.FirstNames[rng.IntN(len(defs.FirstNames))],
		LastName:  defs.LastNames[rng.IntN(len(defs.LastNames))],
	}
	ranges := profile.Ranges
	for a, r := range ranges {
		ranges[a] = defs.Youth.Range(r)
	}
	var attrs players.Attributes
	drawAttributes(rng, &attrs, ranges, 0, matchAttributes)
	born, err := at.Add(-sim.Duration(rng.IntRange(birthDays(defs.Youth.Ages))) * sim.Day)
	if err != nil {
		return registry.Player{}, players.Profile{}, fmt.Errorf("worldgen: youth player %d: %w", id, err)
	}
	p.Born = born
	// Version 2: drawn last, so the draws above are unchanged.
	drawAttributes(rng, &attrs, ranges, matchAttributes, players.NumAttributes)
	// Version 3: drawn last of all.
	p.Nationality = drawNationality(rng, home, len(defs.Nations), defs.Youth.ForeignPct)
	return p, players.Profile{Player: id, Position: pos, Attributes: attrs}, nil
}
