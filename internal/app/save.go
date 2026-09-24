package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	"github.com/thewalpa/project-zimble/internal/players"
	"github.com/thewalpa/project-zimble/internal/registry"
	"github.com/thewalpa/project-zimble/internal/worldgen"
)

var (
	// ErrIncompatibleSave: the save needs behavior this build does not have.
	ErrIncompatibleSave = errors.New("app: incompatible save")
	// ErrInvalidSave: the save is well-formed but its state is inconsistent.
	ErrInvalidSave = errors.New("app: invalid save")
)

// Versions records every version that produced or will continue a world.
type Versions struct {
	// Provenance only: they produced state that the save stores in full.
	Generator int // worldgen.Version
	Content   int // content.Version
	League    int // content.LeagueVersion
	// Must match this build: they determine future simulation.
	Random        int    // random.Version (per-fixture match streams)
	Schedule      int    // competitions.ScheduleVersion (future seasons)
	Selection     int    // ai.SelectionVersion (future lineups)
	EngineID      string // match engine built by the composition root
	EngineVersion uint32
}

type LeagueSnapshot struct {
	Definition content.League // effective rules, pinned by the save
	Season     competitions.SeasonRef
}

// PayloadRecord is a scheduled task's payload: the round it kicks off.
type PayloadRecord struct {
	ID    sim.PayloadID
	Round competitions.RoundRef
}

// CommandRecord is a successful command and its complete recorded result,
// including detailed match outcomes, so retries after loading are answered
// exactly as before.
type CommandRecord struct {
	Request ResolveRounds // Rounds in canonical order
	Result  RoundsResolved
}

// WorldSnapshot is a world's complete authoritative state at a world
// boundary. It is plain data; derived views (indexes, standings, tables,
// pending rounds, the task heap order) are rebuilt by Restore.
//
// No random stream is live at a world boundary: generation and fixture draws
// are finished and their results are stored, and each match stream is
// re-derived from (Seed, EngineID, EngineVersion, fixture ID) under
// Versions.Random when its round is resolved. Seed and versions are
// therefore the complete random state; there is no stream cursor.
type WorldSnapshot struct {
	Seed               random.Seed
	Epoch              sim.CivilTime
	Versions           Versions
	WorldFingerprint   string // generated-world provenance, shown by Summary
	ContentFingerprint string // SHA-256 of Content and the league definitions
	Content            content.Definitions
	Leagues            []LeagueSnapshot // ascending competition ID
	Revision           Revision

	Registry     registry.Init
	Players      []players.Profile
	Employment   []employment.Assignment
	Competitions competitions.Snapshot
	Scheduler    sim.SchedulerSnapshot
	Payloads     []PayloadRecord // ascending ID
	LastPayload  sim.PayloadID   // payload ID allocator
	Commands     []CommandRecord // ascending command ID
}

func currentVersions(engine matches.Engine) Versions {
	return Versions{
		Generator: worldgen.Version, Content: content.Version, League: content.LeagueVersion,
		Random: random.Version, Schedule: competitions.ScheduleVersion, Selection: ai.SelectionVersion,
		EngineID: engine.ID(), EngineVersion: engine.Version(),
	}
}

// contentFingerprint identifies the effective content of a career.
func contentFingerprint(defs content.Definitions, leagues []content.League) string {
	data, err := json.Marshal(struct {
		Content content.Definitions
		Leagues []content.League
	}{defs, leagues})
	if err != nil {
		panic(fmt.Sprintf("app: content is not serializable: %v", err)) // plain data; cannot happen
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (w *World) leagueDefs() []content.League {
	out := make([]content.League, len(w.leagues))
	for i, l := range w.leagues {
		out[i] = l.def
	}
	return out
}

// Snapshot exports the world's authoritative state. The result shares no
// mutable memory with the world. It never changes the world.
func (w *World) Snapshot() WorldSnapshot {
	snap := WorldSnapshot{
		Seed:  w.seed,
		Epoch: w.calendar.Epoch(),
		Versions: Versions{
			Generator: w.generatorVersion, Content: w.contentVersion, League: w.leagueVersion,
			Random: w.randomVersion, Schedule: w.scheduleVersion, Selection: w.selectionVersion,
			EngineID: w.engine.ID(), EngineVersion: w.engine.Version(),
		},
		WorldFingerprint:   w.fingerprint,
		ContentFingerprint: contentFingerprint(w.defs, w.leagueDefs()),
		Content:            w.defs.Clone(),
		Revision:           w.revision,
		Registry:           w.registry.Snapshot(),
		Players:            w.players.Snapshot(),
		Employment:         w.employment.Snapshot(),
		Competitions:       w.competitions.Snapshot(),
		Scheduler:          w.scheduler.Snapshot(),
		LastPayload:        w.lastPayload,
	}
	for _, l := range w.leagues {
		snap.Leagues = append(snap.Leagues, LeagueSnapshot{Definition: l.def, Season: l.season})
	}
	for _, id := range slices.Sorted(maps.Keys(w.payloads)) {
		snap.Payloads = append(snap.Payloads, PayloadRecord{ID: id, Round: w.payloads[id]})
	}
	for _, id := range slices.Sorted(maps.Keys(w.commands)) {
		rec := w.commands[id]
		snap.Commands = append(snap.Commands, CommandRecord{
			Request: ResolveRounds{ID: id, ExpectedRevision: rec.expected, Rounds: slices.Clone(rec.rounds)},
			Result:  cloneResolved(rec.result),
		})
	}
	return snap
}

// Restore builds a new world from a snapshot, as the composition root: it
// rebuilds each module from its own section, constructs the match engine and
// checks it against the saved versions, and validates the whole world before
// returning it. It runs no tasks, resolves nothing, allocates no IDs and
// leaves the revision as saved. On error nothing is returned; no existing
// world is touched. The snapshot is copied, never retained.
func Restore(snap WorldSnapshot) (*World, error) {
	invalid := func(format string, args ...any) (*World, error) {
		return nil, fmt.Errorf("%w: "+format, append([]any{ErrInvalidSave}, args...)...)
	}
	engine, err := simple.New(simple.DefaultParams())
	if err != nil {
		return nil, err
	}
	if err := checkVersions(snap.Versions, currentVersions(engine)); err != nil {
		return nil, err
	}

	defs := snap.Content.Clone()
	var leagueDefs []content.League
	for _, l := range snap.Leagues {
		leagueDefs = append(leagueDefs, l.Definition)
	}
	if got := contentFingerprint(defs, leagueDefs); got != snap.ContentFingerprint {
		return invalid("content fingerprint %s does not match its content (%s)", snap.ContentFingerprint, got)
	}
	if err := defs.Validate(); err != nil {
		return invalid("%v", err)
	}
	calendar, err := sim.NewCalendar(snap.Epoch)
	if err != nil {
		return invalid("%v", err)
	}
	reg, err := registry.New(snap.Registry)
	if err != nil {
		return invalid("%v", err)
	}
	pl, err := players.New(snap.Players)
	if err != nil {
		return invalid("%v", err)
	}
	emp, err := employment.New(snap.Employment)
	if err != nil {
		return invalid("%v", err)
	}
	comps, err := competitions.Restore(snap.Competitions)
	if err != nil {
		return invalid("%v", err)
	}
	scheduler, err := sim.RestoreScheduler(snap.Scheduler)
	if err != nil {
		return invalid("%v", err)
	}

	w := &World{
		seed:             snap.Seed,
		generatorVersion: snap.Versions.Generator,
		randomVersion:    snap.Versions.Random,
		contentVersion:   snap.Versions.Content,
		leagueVersion:    snap.Versions.League,
		scheduleVersion:  snap.Versions.Schedule,
		selectionVersion: snap.Versions.Selection,
		fingerprint:      snap.WorldFingerprint,
		defs:             defs,
		calendar:         calendar,
		engine:           engine,
		revision:         snap.Revision,
		commands:         map[CommandID]commandRecord{},
		scheduler:        scheduler,
		payloads:         map[sim.PayloadID]competitions.RoundRef{},
		lastPayload:      snap.LastPayload,
		registry:         reg,
		players:          pl,
		employment:       emp,
		competitions:     comps,
	}

	for i, l := range snap.Leagues {
		if err := l.Definition.Validate(); err != nil {
			return invalid("%v", err)
		}
		if l.Season.Competition != l.Definition.ID || (i > 0 && l.Definition.ID <= snap.Leagues[i-1].Definition.ID) {
			return invalid("league %d: season %s, or leagues not in ascending unique ID order", l.Definition.ID, l.Season)
		}
		w.leagues = append(w.leagues, leagueEntry{def: l.Definition, season: l.Season})
	}
	if len(w.leagues) == 0 {
		return invalid("no leagues")
	}

	for _, p := range snap.Payloads {
		if p.ID == 0 || p.ID > snap.LastPayload {
			return invalid("payload ID %d is zero or above allocator %d", p.ID, snap.LastPayload)
		}
		if _, dup := w.payloads[p.ID]; dup {
			return invalid("duplicate payload %d", p.ID)
		}
		if _, ok := comps.Round(p.Round); !ok {
			return invalid("payload %d references unknown %s", p.ID, p.Round)
		}
		w.payloads[p.ID] = p.Round
	}

	for _, c := range snap.Commands {
		if err := w.restoreCommand(c, snap.Revision); err != nil {
			return invalid("command %d: %v", c.Request.ID, err)
		}
	}

	if err := w.Validate(); err != nil {
		return invalid("%v", err)
	}
	return w, nil
}

// checkVersions rejects saves whose future simulation this build cannot
// reproduce. Provenance versions may differ: the save stores what they made.
func checkVersions(saved, current Versions) error {
	type pair struct {
		name        string
		saved, have any
	}
	for _, p := range []pair{
		{"random", saved.Random, current.Random},
		{"schedule", saved.Schedule, current.Schedule},
		{"AI selection", saved.Selection, current.Selection},
		{"match engine", saved.EngineID, current.EngineID},
		{"match engine version", saved.EngineVersion, current.EngineVersion},
	} {
		if p.saved != p.have {
			return fmt.Errorf("%w: %s version %v, this build has %v", ErrIncompatibleSave, p.name, p.saved, p.have)
		}
	}
	return nil
}

// restoreCommand validates a recorded command against the restored official
// state and adds it to the command log. A recorded result must describe
// completed rounds whose official results it matches exactly.
func (w *World) restoreCommand(c CommandRecord, revision Revision) error {
	req, res := c.Request, c.Result
	if !validCommandID(req.ID) || res.Command != req.ID {
		return fmt.Errorf("request ID %d, result for command %d", req.ID, res.Command)
	}
	if _, dup := w.commands[req.ID]; dup {
		return errors.New("duplicate command ID")
	}
	rounds, err := canonicalRounds(req.Rounds)
	if err != nil || len(rounds) == 0 || !slices.Equal(rounds, req.Rounds) || !slices.Equal(res.Rounds, rounds) {
		return fmt.Errorf("rounds %v / %v are not one canonical batch", req.Rounds, res.Rounds)
	}
	if res.Revision <= req.ExpectedRevision || res.Revision > revision {
		return fmt.Errorf("result revision %d outside (%d, %d]", res.Revision, req.ExpectedRevision, revision)
	}
	var fixtures []ids.FixtureID
	for _, r := range rounds {
		info, ok := w.competitions.Round(r)
		if !ok || info.Status != competitions.RoundCompleted {
			return fmt.Errorf("%s is not a completed round", r)
		}
		fixtures = append(fixtures, info.Fixtures...)
	}
	slices.Sort(fixtures)
	if len(res.Matches) != len(fixtures) {
		return fmt.Errorf("%d match reports for %d fixtures", len(res.Matches), len(fixtures))
	}
	for i, m := range res.Matches {
		official, ok := w.competitions.Result(m.Fixture)
		switch {
		case m.Fixture != fixtures[i] || !ok:
			return fmt.Errorf("match report %d is for fixture %d", i, m.Fixture)
		case m.Round != (competitions.RoundRef{Season: official.Season, Round: official.Round}):
			return fmt.Errorf("fixture %d reported in %s", m.Fixture, m.Round)
		case m.Home.Team != official.Home || m.Away.Team != official.Away:
			return fmt.Errorf("fixture %d teams differ from the official fixture", m.Fixture)
		case m.Score != [2]uint16{official.HomeGoals, official.AwayGoals} || res.At != official.RecordedAt:
			return fmt.Errorf("fixture %d report %v at %d differs from official %d-%d at %d",
				m.Fixture, m.Score, res.At, official.HomeGoals, official.AwayGoals, official.RecordedAt)
		}
		var goals [2]uint16
		for _, g := range m.Goals {
			if !g.Side.Valid() {
				return fmt.Errorf("fixture %d goal with side %d", m.Fixture, g.Side)
			}
			goals[g.Side.Index()]++
		}
		if goals != m.Score {
			return fmt.Errorf("fixture %d goals %v disagree with score %v", m.Fixture, goals, m.Score)
		}
	}
	w.commands[req.ID] = commandRecord{expected: req.ExpectedRevision, rounds: rounds, result: cloneResolved(res)}
	return nil
}

// NextCommandID returns an ID no recorded command uses: one above the
// highest. Clients such as the CLI use it to continue a loaded career.
func (w *World) NextCommandID() CommandID {
	if len(w.commands) == 0 {
		return 1
	}
	return slices.Max(slices.Collect(maps.Keys(w.commands))) + 1
}
