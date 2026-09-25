// Package app is the composition root. It runs the world-initialization
// workflow, owns the module instances and composes cross-module views.
//
// Domain modules never see World; they are wired here and communicate only
// through the values this package passes between them.
package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/thewalpa/project-zimble/internal/ai"
	"github.com/thewalpa/project-zimble/internal/competitions"
	"github.com/thewalpa/project-zimble/internal/content"
	"github.com/thewalpa/project-zimble/internal/core/ids"
	"github.com/thewalpa/project-zimble/internal/core/random"
	"github.com/thewalpa/project-zimble/internal/core/sim"
	"github.com/thewalpa/project-zimble/internal/employment"
	"github.com/thewalpa/project-zimble/internal/matches"
	"github.com/thewalpa/project-zimble/internal/matches/simple"
	"github.com/thewalpa/project-zimble/internal/medical"
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
	"github.com/thewalpa/project-zimble/internal/selection"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

// Config holds the inputs to world creation. Seed and Epoch are required.
type Config struct {
	Seed random.Seed
	// Epoch is the UTC civil time of game instant 0. It is fixed for the
	// career; the clock starts there.
	Epoch sim.CivilTime
	// UserClub is the club the human manages. Zero means none: every club
	// is AI-managed. It is fixed for the career.
	UserClub ids.ClubID
}

// DefaultEpoch is the start of a new career: 2025-07-01 00:00 UTC.
func DefaultEpoch() sim.CivilTime { return sim.CivilTime{Year: 2025, Month: 7, Day: 1} }

// DefaultConfig returns a config with the given seed and DefaultEpoch.
func DefaultConfig(seed random.Seed) Config { return Config{Seed: seed, Epoch: DefaultEpoch()} }

// World holds the authoritative module state of one career.
type World struct {
	seed             random.Seed
	generatorVersion int
	randomVersion    int
	contentVersion   int
	leagueVersion    int
	scheduleVersion  int
	selectionVersion int
	medicalVersion   int
	fingerprint      string
	defs             content.Definitions
	leagues          []leagueEntry // ascending competition ID
	calendar         sim.Calendar
	engine           matches.Engine
	userClub         ids.ClubID // zero: no user club

	// revision increments on every committed change; commands record their
	// results by ID so a retried command is answered, not re-applied.
	revision Revision
	commands map[CommandID]commandRecord

	// Simulation runtime: the world clock, queued tasks and the payload
	// records they reference. Only app interprets task kinds.
	scheduler   *sim.Scheduler
	payloads    map[sim.PayloadID]competitions.RoundRef
	lastPayload sim.PayloadID

	registry     *registry.Registry
	players      *players.Store
	employment   *employment.Store
	medical      *medical.Store
	competitions *competitions.Store
	selections   *selection.Store
}

// leagueEntry is a league definition and its current season.
type leagueEntry struct {
	def    content.League
	season competitions.SeasonRef
}

// firstSeason is the season number of a new career's first season.
const firstSeason competitions.Season = 1

// NewWorld generates a world from the built-in content and cfg.Seed, loads
// it into the owning modules, schedules the first league season and
// validates cross-module invariants. A non-zero cfg.UserClub must name a
// generated club; choosing one does not change the generated world.
func NewWorld(cfg Config) (*World, error) {
	defs := content.Default()
	snap, err := worldgen.Generate(defs, cfg.Seed)
	if err != nil {
		return nil, err
	}
	w, err := load(defs, []content.League{content.DefaultLeague()}, cfg.Epoch, snap)
	if err != nil {
		return nil, err
	}
	if cfg.UserClub != 0 {
		if _, ok := w.registry.Club(cfg.UserClub); !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownClub, cfg.UserClub)
		}
		w.userClub = cfg.UserClub
	}
	return w, nil
}

// load hands each module its slice of the snapshot, creates one season per
// league and schedules one kickoff task per round. The snapshot is not
// retained: each module copies what it owns. Competition creation reads only
// the registry and its own random stream, so it cannot change generated
// players.
//
// Leagues are processed in competition ID order; each takes the next
// league.Entrants clubs in club ID order, so every club enters exactly one
// league.
func load(defs content.Definitions, leagueDefs []content.League, epoch sim.CivilTime, snap worldgen.Snapshot) (*World, error) {
	leagueDefs = slices.Clone(leagueDefs)
	slices.SortFunc(leagueDefs, func(a, b content.League) int { return cmp.Compare(a.ID, b.ID) })
	if len(leagueDefs) == 0 {
		return nil, errors.New("app: no leagues")
	}
	totalEntrants := 0
	for i, l := range leagueDefs {
		if err := l.Validate(); err != nil {
			return nil, err
		}
		if i > 0 && leagueDefs[i-1].ID == l.ID {
			return nil, fmt.Errorf("app: duplicate league ID %d", l.ID)
		}
		totalEntrants += l.Entrants
	}
	calendar, err := sim.NewCalendar(epoch)
	if err != nil {
		return nil, err
	}
	scheduler, err := sim.NewScheduler(0)
	if err != nil {
		return nil, err
	}
	engine, err := simple.New(simple.DefaultParams())
	if err != nil {
		return nil, err
	}
	reg, err := registry.New(registry.Init{Clubs: snap.Clubs, Teams: snap.Teams, Players: snap.Players})
	if err != nil {
		return nil, err
	}
	pl, err := players.New(snap.Profiles)
	if err != nil {
		return nil, err
	}
	emp, err := employment.New(snap.Assignments)
	if err != nil {
		return nil, err
	}
	// Every player starts the career fully fit.
	var fit []medical.Record
	for _, p := range snap.Players {
		fit = append(fit, medical.Record{Player: p.ID, Condition: medical.MaxCondition})
	}
	med, err := medical.New(medical.DefaultParams(), fit)
	if err != nil {
		return nil, err
	}
	clubs := reg.Clubs()
	if totalEntrants != len(clubs) {
		return nil, fmt.Errorf("app: leagues take %d entrants but the world has %d clubs", totalEntrants, len(clubs))
	}
	w := &World{
		seed:             snap.Seed,
		generatorVersion: snap.GeneratorVersion,
		randomVersion:    snap.RandomVersion,
		contentVersion:   snap.ContentVersion,
		leagueVersion:    content.LeagueVersion,
		scheduleVersion:  competitions.ScheduleVersion,
		selectionVersion: ai.SelectionVersion,
		medicalVersion:   medical.Version,
		fingerprint:      snap.Fingerprint(),
		defs:             defs,
		registry:         reg,
		players:          pl,
		employment:       emp,
		medical:          med,
		competitions:     competitions.New(),
		selections:       mustEmptySelections(),
		calendar:         calendar,
		engine:           engine,
		scheduler:        scheduler,
		payloads:         map[sim.PayloadID]competitions.RoundRef{},
		commands:         map[CommandID]commandRecord{},
	}
	if err := w.scheduleRecovery(sim.GameInstant(sim.Day)); err != nil {
		return nil, err
	}
	next := 0
	for _, def := range leagueDefs {
		firstKickoff, err := calendar.Instant(def.FirstKickoff)
		if err != nil {
			return nil, fmt.Errorf("app: league %d first kickoff: %w", def.ID, err)
		}
		if firstKickoff < scheduler.Now() {
			return nil, fmt.Errorf("app: league %d first kickoff %s is before the career epoch %s", def.ID, def.FirstKickoff, epoch)
		}
		// Enter the next block of clubs' senior teams. The competitions
		// module canonicalizes the order again before its seeded draw.
		var entrants []ids.TeamID
		for _, c := range clubs[next : next+def.Entrants] {
			team, _ := reg.SeniorTeam(c.ID) // registry guarantees one
			entrants = append(entrants, team)
		}
		next += def.Entrants
		entry := leagueEntry{def: def, season: competitions.SeasonRef{Competition: def.ID, Season: firstSeason}}
		timing := competitions.Timing{FirstKickoff: firstKickoff, RoundInterval: def.RoundInterval}
		if err := w.competitions.CreateLeagueSeason(snap.Seed, entry.season, entrants, timing); err != nil {
			return nil, err
		}
		if err := w.scheduleRounds(entry.season); err != nil {
			return nil, err
		}
		w.leagues = append(w.leagues, entry)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return w, nil
}

// Validate checks invariants that span modules. Each module has already
// validated its own state on construction.
func (w *World) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }

	clubs := w.registry.Clubs()
	if len(clubs) != w.defs.ClubCount {
		fail("world has %d clubs, want %d", len(clubs), w.defs.ClubCount)
	}

	identities := w.registry.Players()
	profiled := w.players.PlayerIDs()
	if len(profiled) != len(identities) {
		fail("%d player identities but %d profiles", len(identities), len(profiled))
	}
	for _, id := range profiled {
		if _, ok := w.registry.Player(id); !ok {
			fail("profile for unknown player %d", id)
		}
	}

	assignments := w.employment.Assignments()
	if len(assignments) != len(identities) {
		fail("%d player identities but %d employment assignments", len(identities), len(assignments))
	}
	for _, a := range assignments {
		if _, ok := w.registry.Player(a.Player); !ok {
			fail("assignment for unknown player %d", a.Player)
		}
		team, ok := w.registry.Team(a.Team)
		switch {
		case !ok:
			fail("player %d assigned to unknown team %d", a.Player, a.Team)
		case team.Club != a.Club:
			fail("player %d employed by club %d but assigned to team %d of club %d", a.Player, a.Club, a.Team, team.Club)
		}
	}
	if conditions := w.medical.Records(); len(conditions) != len(identities) {
		fail("%d player identities but %d condition records", len(identities), len(conditions))
	}
	for _, p := range identities {
		if _, ok := w.players.Profile(p.ID); !ok {
			fail("player %d has no profile", p.ID)
		}
		if _, ok := w.medical.Condition(p.ID); !ok {
			fail("player %d has no condition record", p.ID)
		}
		if _, ok := w.employment.Assignment(p.ID); !ok {
			fail("player %d has no employment assignment", p.ID)
		}
	}

	for _, c := range clubs {
		team, _ := w.registry.SeniorTeam(c.ID) // registry guarantees one
		squad := w.employment.Squad(team)
		if len(squad) != w.defs.SquadSize() {
			fail("club %d senior squad has %d players, want %d", c.ID, len(squad), w.defs.SquadSize())
		}
		counts := map[players.Position]int{}
		for _, id := range squad {
			if p, ok := w.players.Profile(id); ok {
				counts[p.Position]++
			}
		}
		for _, q := range w.defs.Roster {
			if counts[q.Position] != q.Count {
				fail("club %d senior squad has %d %s, want %d", c.ID, counts[q.Position], q.Position, q.Count)
			}
		}
	}

	errs = append(errs, w.validateCompetitions()...)
	errs = append(errs, w.validateSchedule()...)
	errs = append(errs, w.validateSelections()...)
	return errors.Join(errs...)
}

// validateCompetitions checks that every competition season belongs to a
// known league, each league has its defined number of entrants, every
// entrant is a registered senior team, and every club enters exactly one
// league.
func (w *World) validateCompetitions() []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("app: "+format, args...)) }
	if err := w.competitions.Validate(); err != nil {
		errs = append(errs, err)
	}
	known := map[competitions.SeasonRef]bool{}
	for _, l := range w.leagues {
		known[l.season] = true
	}
	for _, ref := range w.competitions.Seasons() {
		if !known[ref] {
			fail("%s belongs to no league", ref)
		}
	}
	clubsEntered := map[ids.ClubID]competitions.SeasonRef{}
	for _, l := range w.leagues {
		entrants, ok := w.competitions.Entrants(l.season)
		if !ok {
			fail("%s does not exist", l.season)
			continue
		}
		if len(entrants) != l.def.Entrants {
			fail("%s has %d entrants, want %d", l.season, len(entrants), l.def.Entrants)
		}
		for _, id := range entrants {
			team, ok := w.registry.Team(id)
			switch {
			case !ok:
				fail("%s entrant %d is not a registered team", l.season, id)
			case team.Kind != registry.TeamSenior:
				fail("%s entrant %d is not a senior team", l.season, id)
			default:
				if other, dup := clubsEntered[team.Club]; dup {
					fail("club %d is entered in both %s and %s", team.Club, other, l.season)
				}
				clubsEntered[team.Club] = l.season
			}
		}
	}
	for _, c := range w.registry.Clubs() {
		if _, ok := clubsEntered[c.ID]; !ok {
			fail("club %d is entered in no league", c.ID)
		}
	}
	return errs
}
