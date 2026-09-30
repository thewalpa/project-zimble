# Progress

## Milestone 1: deterministic generated world and CLI (done)

`go run ./cmd/simulate -seed 42` generates 8 fictional clubs, each with one senior team and 20 players (3 GK, 7 DF, 6 MF, 4 FW), then prints a summary with versions, a content fingerprint and one row per club.

### Packages

| Package | Responsibility |
| --- | --- |
| `internal/core/ids` | `ClubID`, `TeamID`, `PlayerID` (distinct `uint64` types, 0 invalid) |
| `internal/core/random` | SplitMix64 stream, seed derivation (FNV-1a domain hash), unbiased `IntN`, `Perm` |
| `internal/registry` | Clubs, teams (kind: senior), player identities |
| `internal/players` | Position, six 1–20 attributes, position-weighted overall |
| `internal/employment` | Player → employing club and team; squad query |
| `internal/content` | Versioned definitions: towns, name pools, roster template, attribute ranges |
| `internal/worldgen` | `Generate(defs, seed) → Snapshot`; canonical SHA-256 fingerprint |
| `internal/app` | Composition root: generate, load modules, cross-module `Validate`, `Summary` |
| `cmd/simulate` | CLI; `-seed` selects the world (random if omitted, see below) |

### Decisions

- **Team membership lives in `employment`**, not `registry`, because the architecture says Employment owns who employs a player. The displayed roster is a derived view.
- **Position lives in `players`** with attributes; the registry holds identity only.
- **Own PRNG** instead of `math/rand/v2`, so outputs depend on this repository's `random.Version` rather than the Go release. Tests pin SplitMix64 reference values.
- **Per-entity streams.** Club names use one stream; each player uses a stream keyed by `(seed, "worldgen/player", worldgen.Version, playerID)`.
- **Sequential IDs** from 1 in generation order (clubs 1–8, teams 1–8, players 1–160).
- **Golden fingerprint.** `worldgen_test.go` pins seed 42's fingerprint. An intended change to generated content must bump a version and update it.
- **Snapshot vs runtime state.** Modules copy their part of the generated snapshot; `app` keeps only its fingerprint.
- **Seed.** Originally required. Changed later: when `-seed` is omitted, the CLI draws one from `crypto/rand` (not the clock) and reports it on stderr (`using random seed N (rerun with -seed N to reproduce)`) and in the `world seed=N` header. Only the CLI picks seeds; `app.Config` still requires an explicit seed.
- **Import graph enforced** by `internal/app/boundaries_test.go`.
- **Integer summary maths.** Overall and averages are computed in tenths.
- `go.mod` declares `go 1.25`; developed with Go 1.27.1.

### Limitations

- Nothing mutates after creation: no commands, events, world revisions, clock or scheduler yet.
- No birth dates, nationalities, contracts, wages, potential or condition.
- No save/load.
- Player names can repeat across the world; IDs are identity.
- Reproducibility is verified on linux/amd64 only. The generator uses integer arithmetic only, so other platforms are expected to match but have not been checked.

## Milestone 2: deterministic league fixtures (done)

`go run ./cmd/simulate -seed 42` now also prints the Founders League season 1 schedule: 14 rounds of 4 fixtures (56 total), grouped by round.

### Changes

| Package | Change |
| --- | --- |
| `internal/core/ids` | Added `CompetitionID`, `FixtureID` (0 invalid) |
| `internal/content` | Added `League` definition (`DefaultLeague`: ID 1, "Founders League", 8 entrants), versioned by `LeagueVersion` |
| `internal/competitions` | New. `Store.CreateLeagueSeason`, `Seasons`, `Entrants`, `Fixtures`, `Fixture`, `Validate`; `SeasonRef{Competition, Season}`, `Round`, `Fixture` |
| `internal/app` | Creates the first league season from every club's senior team; `Validate` checks entrants against the registry; `Schedule()` view |
| `cmd/simulate` | Prints fixtures by round |

All changes to existing contracts are additive. `worldgen`, `Snapshot`, `content.Definitions` and `content.Version` are unchanged, so the seed-42 world fingerprint is still `8bc01a11…`.

### Decisions

- **Competition-season identity** is `competitions.SeasonRef{Competition ids.CompetitionID, Season Season}`, where `Season` is a 1-based season number in the career. No dates.
- **Fixture identity.** `FixtureID` is opaque and unique across all competitions and seasons in a world. The `competitions` store assigns IDs from one counter, starting at 1 and never reused. The natural key `(SeasonRef, Round, Home, Away)` is stored as fields, not encoded in the ID. Deterministic because `app` creates seasons in a fixed order. The counter must be persisted once save/load exists.
- **Entrants are teams, not clubs.** `competitions` validates count (exactly `SupportedEntrants` = 8), non-zero and unique IDs. `app` validates that entrants are registered senior teams, one per club, because `competitions` cannot import `registry`.
- **Algorithm:** sort entrants by ID; draw a slot permutation from `random.Derive(seed, "competitions/fixtures", ScheduleVersion, competitionID, season)`; circle method with de Werra's canonical venue rule for rounds 1–7; rounds 8–14 mirror 1–7 with venues swapped. The permutation is the only randomness.
- **Venue balance:** 7 home and 7 away per team; no team plays more than 2 consecutive home or away games within a half, or 3 across the halfway point. A first version that alternated venues by slot passed all required invariants but gave 7-game home runs, and was replaced before commit (`ScheduleVersion` stays 1).
- **All-or-nothing creation.** `CreateLeagueSeason` validates, generates and self-checks the schedule before committing; on error the store is unchanged.
- **League definition versioned separately** (`LeagueVersion`) so competition content cannot alter generated worlds.
- Golden SHA-256 of the seed-42 schedule is pinned in `competitions_test.go`.

### Limitations

- Only 8-entrant double round-robin leagues. The algorithm handles any even count; other sizes are rejected until tested.
- No dates, kickoff times, match days or venues beyond home/away.
- No results, standings, scheduler or game clock.
- Round order is fixed by the algorithm (the second half mirrors the first in the same order); only team-to-slot assignment is seeded.
- `CreateLeagueSeason` is a direct method call, not a command with an envelope, revision or event.

## Milestone 3: game time and deterministic scheduling (done)

A new world starts at the career epoch (2025-07-01 00:00 UTC, instant 0) with one kickoff task per league round. `Continue` advances logical time and stops when round 1 kicks off (Sat 2025-08-09 15:00 UTC); round 1 then awaits results and the world stays there. The CLI prints the round calendar and demonstrates three `Continue` calls.

### Changes

| Package | Change |
| --- | --- |
| `internal/core/sim` | New. `GameInstant`/`Duration` (minutes), `CivilTime` (UTC), `Calendar`, `Task`/`TaskSpec`, `Before`, `Scheduler` (clock + heap queue + `RunUntil`) |
| `internal/competitions` | `Timing{FirstKickoff, RoundInterval}` parameter on `CreateLeagueSeason`; `Fixture.Kickoff`; `RoundRef`, `RoundStatus`, `RoundInfo`; `Rounds`, `Round`, `PendingRounds`, `BeginRounds` |
| `internal/content` | `League.FirstKickoff` (civil UTC) and `RoundInterval` (7 days) |
| `internal/app` | `Config.Epoch` (required) and `DefaultConfig`; world clock and task scheduling; `Continue`, `ReachedTarget`, `FixtureRoundReady`; `Now`, `Calendar`; kickoff and status in `Schedule()`; `validateSchedule` |
| `cmd/simulate` | Round calendar with kickoff times; `Continue` demo |

Breaking contract changes: `CreateLeagueSeason` takes `Timing`; `app.Config` requires `Epoch` (use `DefaultConfig`). Pairings, fixture IDs, the schedule golden hash and the world fingerprint are unchanged.

### Time model

- `GameInstant` is `int64` minutes since the career epoch; the supported range is ±`sim.MaxInstant` (500,000,000 minutes, about 950 years). Out-of-range values are rejected, not clamped.
- The epoch is a `CivilTime` in UTC, set once in `app.Config`, stored in the world's `Calendar`, and must be in 1900–2999. The clock starts at 0.
- Conversion uses Unix seconds and `time.UTC` only. No time zones, no DST; non-existent civil times (Feb 30, minute 60) are rejected.
- Kickoffs are owned by `competitions`: round r = `FirstKickoff + (r-1)*RoundInterval`. All fixtures in a round share the round's kickoff.

### Scheduling

- Tasks are plain records `(ID, DueAt, Phase, StableOrder, Kind, PayloadID)` ordered by `(DueAt, Phase, StableOrder, ID)`. The scheduler assigns IDs sequentially from 1.
- `Schedule` rejects due times before now, invalid phases (valid 1–6, per the architecture's phase table) and kind 0.
- A **cohort** is every task sharing the head's `(DueAt, Phase)`. `RunUntil` hands a whole cohort to one handler; the cohort is removed and the clock set to its `DueAt` only if the handler succeeds.
- Round kickoff tasks: kind `taskRoundKickoff` (1), phase `Fixtures`, `StableOrder` = competition ID, and a payload record in `app` holding only a `RoundRef`. Payloads are deleted when the round is dispatched.

### Continue semantics

`World.Continue(until)`:

1. `until` outside the supported range, or `until < Now()` (`ErrTargetBeforeNow`): error, no change. Checked even while paused.
2. If any round is `AwaitingResults`: return `FixtureRoundReady` for all such rounds. No time passes and no task runs. Repeated calls return an identical result.
3. Otherwise run due cohorts. The target is inclusive: a task due exactly at `until` runs.
4. A kickoff cohort is dispatched atomically. First every task is checked (known kind, payload present), then `competitions.BeginRounds` checks every round (exists, once, `Scheduled`, kickoff equals now) before marking any. Then payloads are deleted, the cohort is removed and the clock moves to the kickoff. Returns `FixtureRoundReady{At, Rounds}`, with rounds ordered by (kickoff, competition, season, round).
5. With no interruption, the clock moves to `until` and `ReachedTarget{Now}` is returned.
6. Failure: the failing cohort stays queued, the clock stays where it was, and no round changes status. Cohorts committed earlier in the same call stay committed. In this milestone every cohort interrupts, so a call commits at most one.
7. **Simultaneous kickoffs** (same instant and phase, e.g. two competitions) form one cohort: dispatched together, reported together in deterministic order, and blocking together. A later cohort is never dispatched while any round awaits results.

Fixtures are never marked played and no results are invented. Nothing resolves a pending round yet, so a world currently stops at round 1.

### Verification

- Calendar: known values (including a leap day and negative instants); round trips at range ends, a prime-stride sweep of the full range, and every minute of a leap February; independence from `time.Local`; rejection of invalid epochs, civil times and out-of-range instants.
- Queue: each tie-breaker in `Before`; out-of-order insertion runs in order and groups cohorts by (DueAt, Phase); invalid specs rejected.
- `RunUntil`: inclusive target, past target, stop-after-commit, failure keeps the task and clock, one call equals several.
- Competitions: kickoffs per round and fixture, timing does not change pairings or IDs, invalid timing rejected, `BeginRounds` once only and all-or-nothing, deterministic pending order, copies returned.
- App: one task per round due at its kickoff; before, at and past kickoff; paused idempotence; past-target rejection; one call equals several; missing payload and an unknown task kind (first or last in its cohort) leave all state unchanged; three simultaneous leagues created out of order dispatch as one ordered cohort, deterministically; read queries do not consume tasks; epoch validation.
- `TestNoWallClockOrLocalTime` scans all non-test sources.
- CLI: calendar lines, Continue demo lines and byte-identical repeated runs.

### Limitations

- No way to resolve a pending round (no match engine or results), so a career cannot pass round 1.
- Only round kickoff tasks exist. Handlers cannot schedule new tasks during a run yet, and there is no guard against same-instant rescheduling into an earlier phase.
- Simultaneous rounds involving the same team (possible if a team entered two competitions) are not detected; entrant clash rules come with cups.
- No task `SchemaVersion`, save/load or persisted counters (task IDs, payload IDs, fixture IDs).
- Calendar conversion relies on Go's `time` package for UTC civil arithmetic; the civil range is bounded by `MaxInstant` and epoch years.

## Milestone 4: minute-based match engine (done)

`internal/matches` defines the match-session contract and `internal/matches/simple` implements it. The engine is isolated: nothing in the world calls it yet, it changes no world state, and pending rounds stay pending. CLI output is unchanged.

### Contract (`internal/matches`)

- **Input** `MatchInput{Match FixtureID, Home/Away TeamInput, Rules{MaxSubstitutions, MaxBench}}`. `TeamInput` has the team ID, `Tactics{Mentality}`, exactly 11 `Starters` (slot order) and a `Bench`. `PlayerInput` holds a player ID, a match `Role` (GK/DF/MF/FW) and detached `Ratings` (the six `players` attributes, 1..20). `matches` does not import `players`; `app` will map profiles into it.
- `MatchInput.Validate(engineMaxBench)` requires:
  - a valid match ID and two distinct valid teams;
  - 11 starters with exactly one goalkeeper;
  - a bench no larger than `Rules.MaxBench`, which is no larger than the engine limit, and `MaxSubstitutions` no larger than `MaxBench`;
  - valid roles, mentality and ratings;
  - non-zero player IDs, unique across both teams.
- **Engine**: `ID`, `Version`, `Capabilities`, `Start(*MatchInput, RandomState)`, `Restore`.
- **Session**: `Advance(AdvanceRequest{ToMinute}, *MatchStepResult)`, `Apply(MatchCommand)`, `Checkpoint`.
- **Position**: `MatchPosition{Period, Minute}`. `Minute` counts regulation minutes played: 0 at kickoff, 45 at half time, 90 at full time. The periods are FirstHalf, HalfTime, SecondHalf and FullTime.
- **Step result**: `Status` (Running, DecisionRequired, Finished), `Stop` (Target, HalfTime, FullTime), `View` (score, mentality, substitutions used, `OnPitch [2][11]`), `Events`, `Outcome`.
- **Events** `MatchEvent{Seq, Minute, Kind, Side, Player, Other, Mentality, Period}`. Kinds: Goal, Substitution (`Player` came on, `Other` went off), MentalityChange, PeriodEnd. `Seq` runs 1..n across the session.
- **Outcome** `MatchOutcome{Match, Status, Resolution, EngineID, EngineVersion, Score, Goals, Participants}`:
  - `Status` is `ResultPending` until full time, then `ResultCompleted`. Values 3 (abandoned) and 4 (awarded) are reserved.
  - `Resolution` is always `ResolutionRegulation`.
  - `Participation{Player, Side, Started, OnMinute, OffMinute}` covers minutes (On, Off].
- **Capabilities**: `simple` advertises Substitutions and Mentality only. ExtraTime, Penalties, Injuries, Cards, Checkpoints, PositionalFrames and DetailedStats are false. `Checkpoint` and `Restore` return `ErrUnsupported`.
- **Errors**: `ErrInvalidInput`, `ErrInvalidRequest`, `ErrInvalidCommand`, `ErrMatchFinished`, `ErrUnsupported`, all matchable with `errors.Is`.

### Session semantics

- `Advance` simulates minutes up to `ToMinute`, inclusive, which must be between the current minute and 90.
  - It always stops at 45 (`DecisionRequired`, `StopHalfTime`), even if asked for more. The next `Advance` whose target is past 45 resumes play, so the default half-time decision is "no change". `Advance(45)` at half time plays nothing.
  - At 90 it returns `Finished` with a completed outcome.
- Commands are allowed at any stop before full time and take effect from the next simulated minute.
  - A command's event is stamped with the stop minute and `Seq` when applied, then delivered once, at the start of the next `Advance` output.
  - A player substituted at stop minute m has played m minutes (Off = m), and the replacement plays 90 − m (On = m).
- `CommandSetMentality` must name a valid mentality different from the current one.
- `CommandSubstitute` requires all of:
  - the match has kicked off (the lineup at minute 0 belongs to squad selection);
  - the team has substitutions left;
  - the player going off is on the pitch, so a player already taken off can't go off again;
  - the player coming on is an unused bench player, so nobody returns or comes on twice;
  - goalkeepers are replaced like-for-like, so exactly one is always on the pitch.
- `Apply` and `Advance` check everything before changing anything. A rejected call changes neither the session nor `dst`.
- **After full time**, `Advance(90)` refills `dst` with the final state, no events and the same outcome. A lower target is `ErrInvalidRequest`, and `Apply` returns `ErrMatchFinished`.

### Memory ownership

- **Input**: `Start` copies input into fixed-size arrays inside the session and never retains or modifies the caller's slices.
- **Output**: `Advance` resets the lengths of `dst.Events`, `dst.Outcome.Goals` and `dst.Outcome.Participants` to zero, keeps their capacity and appends copies. `View` is made of fixed-size arrays. Nothing in `dst` shares memory with the session; it stays valid until the caller passes `dst` to `Advance` again.
- Reusing a `dst` from another session replaces all of its contents, so no stale result carries over.

### Model (`simple.ModelVersion` = 1, constants in `simple.Params`)

All calculations use integers: probabilities in ppm, multipliers in permille, ratings in tenths. That makes results identical across platforms regardless of floating-point fused multiply-add.

- **Fatigue**: a player's effective rating falls by `(30 − Stamina)` per 10,000 for each minute they have played, capped at 30%. Substitutes start fresh.
- **Attack**: the role-weighted mean over outfield players of Finishing:Passing:Pace in the ratio 4:4:2. Role weights are DF 1, MF 3, FW 5.
- **Defense**: the role-weighted mean of Defending:Pace:Passing in the ratio 6:2:2. Role weights are DF 5, MF 3, FW 1.
- **Chance per minute** (home side first, then away): `110,000 · 2A/(A + D_opp)` ppm. It is multiplied by the side's own mentality factor (defensive/balanced/attacking = 800/1000/1200‰), by the opponent's mentality factor (850/1000/1150‰), and for the home side by home advantage (1050‰). The result is clamped to 20,000–400,000.
- **Shooter**: chosen with weight = shot share (DF 1, MF 3, FW 6) × effective Finishing.
- **Conversion**: `125,000 · (Fin + 10)/(GK_opp + 10)` ppm, clamped to 30,000–450,000.
- **Attribute effects**: Goalkeeping only affects saves, Defending only affects defense, Finishing affects attack, shooter choice and conversion, Passing and Pace affect attack and defense, and Stamina only affects fatigue.
- **Formation** is not modelled.
- **Seeded batch results (2000 matches each, fixed seeds)**:
  - Equal teams (strength 12): 2.75 goals per match; 771 home wins, 714 away wins, 515 draws.
  - Weak home team (9) against strong away team (15): the away team wins 71%.
  - Both sides attacking produces about twice the goals of both sides defensive.

### Randomness and versions

- `matches.FixtureRandom(seed, engineID, engineVersion, fixtureID)` computes `random.Derive(seed, "matches/"+engineID, engineVersion, fixtureID)`. It returns `RandomState{Words:[state,0,0,0], Algorithm: AlgorithmSplitMix64, Version: random.Version}`, and the engine rejects any other shape.
- Within each minute, the random draws happen in the same order: home chance, then the shooter and conversion if a chance occurs, then the away side. Draws depend only on the sequence of minutes, so splitting a match into different `Advance` calls doesn't change it.
- `ModelVersion` changes whenever outcomes for the same input, seed and commands change. Unlike `random.Version`, it is separate from the random algorithm.
- `random.Stream.State()` was added. It is additive and changes no output.

### Verification

- **Complete-match invariants (300 fixtures):**
  - varied strengths, with and without commands;
  - `Seq` has no gaps or duplicates across `Advance` calls;
  - goal events equal the outcome's goals, which equal the score and view;
  - period ends at 45 and 90;
  - 990 participant minutes per side, all starters present, `Started` consistent;
  - every scorer was on the pitch at the goal minute.
- **Determinism and chunking:**
  - the same input, seed and commands give the same match;
  - a different fixture ID changes the match;
  - four stop plans (command stops only, every minute, irregular, overshooting past half time) give identical events and outcomes.
- **Half time and substitutions:**
  - half time is a mandatory stop, and an unfinished session exposes no result;
  - a substitution gives the correct view and participation minutes.
- **Rejected input:**
  - 15 illegal commands, each leaving a deep copy of the session state unchanged;
  - a session that receives rejected commands plays out identically to one that doesn't;
  - finished-session behavior;
  - invalid `Advance` calls leave both the session and `dst` unchanged;
  - 16 invalid-input cases plus a nil input, and 3 invalid-random-state cases;
  - invalid `Params`.
- **Ownership:**
  - mutating the input after `Start` has no effect, and `Start` doesn't modify it;
  - `dst` buffers are reused without reallocation, writing over `dst` doesn't reach the session, and a reused `dst` carries no stale state;
  - command events are delivered exactly once, even through no-op `Advance` calls.
- **Model trends:** home advantage; the stronger team wins at least 55%; mentality moves goals in the right direction; fatigue lowers ratings and substitutes are fresher.
- **Deliberate-bug checks:** breaking the clearing of pending command events, or substitute minutes, made the invariant tests fail.
- **Import rules:** `matches` imports only `core/ids` and `core/random`; `matches/simple` adds only `matches`.

### Benchmark baseline

`go test ./internal/matches/simple -run '^$' -bench . -benchmem`, on an AMD Ryzen 9 7950X, Go 1.27.1, linux/amd64, 3 runs:

| Benchmark | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkCompleteMatch` (Start + two Advance, reused dst) | ~35,400 | 4,752 | 13 |
| `BenchmarkAdvanceOnly` (stepping only) | ~34,400 | 0 | 0 |

All allocations are in `Start`: the session, its random stream, its goal and pending-event buffers, and input validation's duplicate-ID map and slices. The stepping loop allocates nothing. Most of the time goes into recomputing team strength twice per side per minute. This is a baseline, not an optimization target.

### Limitations

- Regulation time only: no stoppage time, extra time, penalties, injuries, cards, positional frames or detailed statistics.
- No checkpoints: `Checkpoint` and `Restore` return `ErrUnsupported`, so a match can't be saved mid-play.
- Half time is the only mandatory decision point; there are no injury-forced substitutions.
- Pre-match condition and health are not inputs yet. Fatigue starts at zero every match.
- The only tactic is mentality; formation, pressing and roles beyond GK/DF/MF/FW aren't modelled. Playing a player out of position uses the given role with unchanged ratings.
- The model is tuned against synthetic teams only.

## Milestone 5: round resolution, results and standings (done)

`go run ./cmd/simulate -seed 42 -season` plays all 14 rounds through `Continue` → `ResolveRounds` and prints each round's results, the final table and the champion. Without `-season`, output is unchanged.

### Changes

| Package | Change |
| --- | --- |
| `internal/competitions` | `RoundCompleted` (3); `Score`, `Result`, `CompleteRounds`, `Result`, `Results`, `SeasonCompleted`; derived `Standing`/`Standings`; `PointsForWin`/`PointsForDraw`/`MaxGoals`; `Validate` checks that results exist exactly for completed rounds |
| `internal/ai` | New. `SelectTeam` (4-4-2 AI lineup), `RoleScore`, `SelectionVersion` |
| `internal/content` | `League.MaxSubstitutions` (3) and `MaxBench` (7) |
| `internal/app` | `ResolveRounds` command, `CommandID`, `Revision`, `World.Revision()`, `FixtureRoundReady.Revision`, typed errors; the world owns a `simple` engine; multiple leagues; `Schedule()` → `Schedules()` (now with results); `Tables()` |
| `cmd/simulate` | `-season` flag |

Contract changes:
- `app.Schedule()` became `Schedules()` (one per league).
- `load` takes `[]content.League`.
- `FixtureRoundReady` gained `Revision`.
- `FixtureLine` gained `Played`/`Score`.

The world fingerprint (`8bc01a11…`) and the schedule golden hash are unchanged.

### Resolution operation

`World.ResolveRounds(ResolveRounds{ID, ExpectedRevision, Rounds}) (RoundsResolved, error)`:

1. **Command checks:**
   - `ID` is non-zero, and `Rounds` has no duplicates or zero values;
   - an already-recorded `ID` gets the special handling under *Retries and duplicates*;
   - `ExpectedRevision` equals `Revision()` (otherwise `ErrStaleRevision`);
   - some rounds are pending (otherwise `ErrNoPendingRounds`);
   - `Rounds`, in any order, equals the whole pending batch (otherwise `ErrBatchMismatch`).
2. **Prepare** (read-only):
   - every fixture of every batch round, sorted by fixture ID;
   - each round is awaiting results, and each fixture belongs to it and has no result;
   - a team appearing in two fixtures is `ErrOverlappingTeams`;
   - each side's lineup comes from `ai.SelectTeam` over its current squad (employment + players, copied into `ai.Candidate`s);
   - one detached `MatchInput` per fixture, validated with `simple.MaxBench`.
   Every input exists before any match runs.
3. **Simulate**, in fixture-ID order, each with `matches.FixtureRandom(worldSeed, engineID, engineVersion, fixtureID)`, advancing to full time. At most 4 `Advance` calls are allowed, which guards against an engine that never finishes. Outcomes are copied out of the reused step buffer. No world state is touched.
4. **Validate every outcome** before recording any:
   - `ResultCompleted` and `ResolutionRegulation`;
   - the right fixture, engine ID and version;
   - score within `MaxGoals`;
   - every participant was selected for that side, with valid, non-duplicated minutes, 990 per side;
   - every goal was scored by a participant on the pitch, and goals per side equal the score.
5. **Apply**: one `competitions.CompleteRounds(rounds, scores, Now())` call. It checks every round (exists, once, awaiting results, kicked off at or before now) and every score (exactly the batch's fixtures, once each, no existing result, within `MaxGoals`), then records all results and marks all rounds `RoundCompleted`. It changes nothing on error.
6. **Commit in `app`**: increment the revision and record the command with a copy of its result. Nothing here can fail.

A failure in steps 1–5 leaves the world unchanged: clock, revision, tasks, round statuses, results and command log. Pending state is derived from round status, so it clears only at step 5.

### Retries and duplicates

- **Same `ID` and content** (`ExpectedRevision` and canonical `Rounds`) as an earlier *successful* command: returns a copy of the recorded result and changes nothing, even though the revision has since moved on.
- **Same `ID`, different content:** `ErrCommandIDReused`.
- **Failed commands** are not recorded, so the same `ID` can be retried. Each fixture's stream depends only on the seed and fixture ID, so a retry produces the same matches as an attempt that never failed.
- **A new `ID` for a batch that's already resolved:** `ErrStaleRevision`, or `ErrNoPendingRounds` if it carries the current revision.
- **No double counting:** `competitions` rejects results for any round not awaiting results and any fixture that already has a result. Standings are computed from results, so there is no separately stored tally to double-count.

### Clock and revision

- Resolution takes no world time. Results become official at `Now()`, which equals the batch's kickoff, and are stored as `Result.RecordedAt`. `ResolveRounds` never moves the clock or runs tasks, so `Continue` still owns progression. Match duration isn't modelled yet; a later "full-time" task could move official time to kickoff + about 110 minutes.
- `Revision` increments on every committed change: a `Continue` call that moved the clock or dispatched work, and each successful `ResolveRounds`. It counts commits, so advancing in more `Continue` calls produces a higher revision for the same world content.

### Ownership and ranking

- `competitions` owns official results: home/away goals and `RecordedAt` per fixture, recorded once and never changed.
- Detailed outcomes (scorers, minutes) are returned in `RoundsResolved.Matches` but not stored. There is no match-records module yet, and the command log keeps only this in-memory copy.
- `Standings(season)` is derived from entrants and results on every call. Wins are worth 3 points and draws 1. Rows are ranked by points, goal difference and goals scored (all descending), then **TeamID ascending as an explicit prototype tie-breaker**.

### AI selection (`ai.SelectionVersion` = 1)

- **Formation:** 4-4-2, with Balanced mentality and no in-match decisions.
- **Candidates** are sorted by player ID first, so input order doesn't matter.
- **Filling each role:** natural players first, by `RoleScore`, highest first, with ties going to the lower ID. Outfield gaps are filled from other outfield players by the needed role's score. A natural goalkeeper is required.
- **Role scores:**
  - GK: 10·Goalkeeping;
  - DF: 6·Def + 2·Pace + 2·Pass;
  - MF: 5·Pass + 2·Stamina + 2·Pace + Fin;
  - FW: 6·Fin + 3·Pace + Pass.
- **Bench:** the best remaining goalkeeper, then the best remaining players by their natural-role score, up to `MaxBench`.

### Multiple leagues

`load` accepts several league definitions. Sorted by ID, each takes the next `Entrants` clubs by club ID, and their sum must equal the club count. `Validate` requires the following:
- every competition season belongs to a defined league;
- each league has its defined number of entrants;
- every entrant is a registered senior team;
- every club enters exactly one league.

Leagues with identical timing kick off together and resolve as one batch.

### Verification

- **Full season (seed 42):**
  - 14 batches of 4 matches, with rising revisions, each official at its kickoff;
  - 56 fixtures with exactly one result each, and all rounds completed;
  - every team played 14 matches;
  - league-wide goals for equal goals against, wins equal losses, draws are even, and wins + draws/2 = 56;
  - points = 3W + D;
  - the standings match an independent recomputation from the results;
  - afterwards there is no pending work, `Continue` reaches its target, and `Validate` passes.
- **Reproducibility:**
  - two worlds give identical resolutions and tables;
  - a golden SHA-256 of all seed-42 results;
  - the world fingerprint and fixtures are unchanged after the season;
  - `-season` CLI output is byte-identical across runs.
- **Simultaneous leagues:** a 16-club, two-league world resolves both leagues' rounds as one 8-match batch in fixture order and plays a full season, with both tables checked independently.
- **Overlap:** a league re-using all 8 teams at the same kickoffs is rejected with `ErrOverlappingTeams`, and the world is unchanged. `Validate` also rejects that world.
- **Commands:**
  - duplicate retries return the recorded result;
  - mutating a returned result doesn't reach the log;
  - reused IDs, stale revisions and new IDs for resolved batches are rejected;
  - 7 invalid command shapes are rejected;
  - the tables never change on a rejected command.
- **Failure atomicity** (8 injected faults: a lineup can't be selected, `Start` fails on the third match, a match never finishes, the score disagrees with goals, the wrong match ID, the wrong engine version, bad participant minutes, a non-completed result):
  - the world is unchanged;
  - the batch is still pending, with no partial results;
  - the failures after simulation provably happen after all 4 matches ran;
  - a retry with the same command ID produces exactly the results of a world that never failed.
- **Deliberate-bug check:** moving the revision bump before simulation made the atomicity test fail.
- **`competitions`:** `CompleteRounds` all-or-nothing (10 rejection cases), no double completion, results in fixture order, tie-breakers exercised by an engineered round, and an empty table ordered by TeamID.
- **`ai`:** the selected lineups pass the match contract; no benched natural player outranks a starter; the result doesn't depend on input order and the input isn't modified; ties go to lower IDs; outfield gaps are filled; impossible squads are rejected; small benches are handled.

### Limitations

- AI makes no in-match decisions (no substitutions or mentality changes) and always plays 4-4-2 Balanced. There's no user-controlled selection yet.
- Matches take no world time; official results are stamped at kickoff.
- Detailed match records and player statistics aren't stored. The command log is in memory only, and there's no save/load yet.
- TeamID as the final tie-breaker is a prototype rule; head-to-head and play-offs aren't implemented.
- There's no season rollover: after round 14, `Continue` just advances the clock.
- Squads never change (no fatigue carry-over, injuries or transfers), so lineups repeat all season.

## Milestone 6: save/load at world boundaries (done)

A world can be saved and loaded at any world boundary:
- straight after creation;
- between completed rounds;
- while a batch is awaiting results;
- after the season is complete.

Loading gives back exactly the saved world: `Restore(snapshot).Snapshot()` equals the snapshot. Mid-match saves are not supported, because sessions never outlive a `ResolveRounds` call.

```sh
go run ./cmd/simulate -seed 42 -rounds 7 -save career.json
go run ./cmd/simulate -load career.json            # status only; changes nothing
go run ./cmd/simulate -load career.json -season    # rounds 8-14, same final table as an uninterrupted -season run
```

### Changes

| Package | Change |
| --- | --- |
| `internal/registry`, `players`, `employment` | `Snapshot()`; restore through the existing validating `New` |
| `internal/competitions` | `Snapshot`/`SeasonSnapshot`/`RoundSnapshot`/`ResultSnapshot`, `Store.Snapshot()`, `Restore()` |
| `internal/core/sim` | `SchedulerSnapshot`, `Scheduler.Snapshot()`, `RestoreScheduler()` |
| `internal/content` | `Definitions.Clone()` |
| `internal/app` | `WorldSnapshot` and its section types, `World.Snapshot()`, `Restore()`, `NextCommandID()`, `Pending()`, `ErrIncompatibleSave`, `ErrInvalidSave`; `World` records the league, schedule and selection versions |
| `internal/storage` | New. JSON envelope codec (`Encode`/`Decode`), `Save` (atomic replace) and `Load` (decode, then `app.Restore`); `ErrMalformedSave`, `ErrUnsupportedSave` |
| `cmd/simulate` | `-load`, `-save`, `-rounds N`; `-season` now plays the *remaining* rounds; status mode for a loaded world |

### Save model

`app.WorldSnapshot` is plain data, and each section is the owning module's own snapshot type:

| Section | Contents |
| --- | --- |
| Identity | `Seed`, `Epoch` (the calendar is rebuilt from it) |
| `Versions` | Provenance: `Generator`, `Content`, `League`. Must match: `Random`, `Schedule`, `Selection`, `EngineID`, `EngineVersion` |
| Content | `WorldFingerprint` (generated-world provenance); `ContentFingerprint` = SHA-256 of `Content` plus the league definitions; `Content` (effective `content.Definitions`); `Leagues` (effective `content.League` rules and season) |
| `Revision` | World revision |
| Modules | `Registry` (`registry.Init`), `Players`, `Employment`, `Competitions` (fixture allocator, entrants, fixtures with kickoffs, round kickoffs and statuses, official results) |
| Runtime | `Scheduler` (clock, task-ID allocator, queued tasks), `Payloads` + `LastPayload` (payload allocator) |
| `Commands` | Each successful `ResolveRounds` request (canonical rounds) and its complete `RoundsResolved`, including every `MatchReport` with goals |

**Authoritative:** everything in the table.

**Derived and rebuilt on load:**
- all lookup indexes;
- the senior-team map;
- the task heap order;
- the calendar;
- pending rounds (from round status);
- standings, tables, schedule views and summaries.

**Nothing is regenerated from the seed.** `Restore` never calls `CreateLeagueSeason` or `scheduleRounds` and never allocates an ID.

**Why no random stream position is saved:**
- World generation and fixture draws finished at creation, and their results are stored.
- Each match stream is re-derived at resolution time as `FixtureRandom(Seed, EngineID, EngineVersion, fixtureID)` under `random.Version`. It lives only inside one `ResolveRounds` call.
- Match sessions are never saved.

So the seed plus those versions is the complete random state at a world boundary.

### Export and restore

- **Export:** `Snapshot()` methods return fresh copies. Tests change every section of an exported snapshot and confirm the world is unaffected.
- **Restore:** each module rebuilds from a validated copy:
  - registry, players and employment through their `New` constructors;
  - `competitions.Restore`;
  - `sim.RestoreScheduler`.

  Restored worlds share no memory with the snapshot or with each other.
- **Composition root:** `app.Restore`:
  1. builds the `simple` engine and checks versions;
  2. checks the content fingerprint and validates the content;
  3. rebuilds the calendar and every module;
  4. validates leagues, payloads and each command record;
  5. runs `World.Validate()`.

  It returns a world only if every step passes.

### Compatibility and failure behavior

- **Envelope** (`storage`):
  - a wrong `Format` or `Schema` is `ErrUnsupportedSave`;
  - invalid or truncated JSON, unknown fields, trailing data or a checksum mismatch is `ErrMalformedSave`.
  - `SchemaVersion` = 1, and there are no migrations yet.
- **Versions:** a mismatch in random, schedule, AI selection, engine ID or engine version is `ErrIncompatibleSave`; these decide future simulation. Generator, content and league versions are recorded but not required to match, because the save carries the state and content they produced.
- **State** (`ErrInvalidSave`):
  - a content-fingerprint mismatch (content or league rules edited);
  - any module constructor or `Restore` failure;
  - `competitions`:
    - entrants or rounds malformed;
    - fixtures out of order, with a kickoff that differs from their round's, in the wrong season, breaking the double round-robin rules, or with IDs above the allocator;
    - results duplicated, for unknown fixtures, above the goal limit, recorded before kickoff, or not existing exactly for completed rounds;
  - scheduler:
    - task IDs zero, duplicated, or above the allocator;
    - a task due before the saved clock;
    - an invalid phase or kind;
  - payloads: IDs zero, duplicated or above the allocator, or pointing at unknown rounds;
  - commands:
    - duplicate IDs, or a request ID that differs from its result's;
    - rounds that aren't canonical or aren't completed;
    - a result revision outside `(expected, saved revision]`;
    - match reports that don't cover exactly the batch's fixtures, or that disagree with the official result, teams, round or recorded time, or whose goals disagree with the score;
  - any `World.Validate` error (cross-module references, league entry, task/payload/round consistency).
- **Candidate world:** decoding produces a candidate that is fully validated before `Load` returns it. On any error `Load` returns `nil`, and no existing world is touched.
- **Safe replacement:** `Save` writes `.<name>.tmp-*` in the same directory, fsyncs it, sets 0644, closes it, renames it over the target, then fsyncs the directory (best effort). A failure at any step removes the temporary file, and the previous save stays loadable. This was tested with failures in create, a partial write, sync and rename.

### File format

```json
{"Format": "project-zimble/save", "Schema": 1, "PayloadSHA256": "<hex of compact payload>", "Payload": { ...WorldSnapshot... }}
```

- Indented JSON (about 88 KB for a seed-42 save after round 7). Encoding is deterministic: the same world always produces the same bytes.
- The payload's Go field names are the schema. Renaming or adding authoritative fields requires a new `SchemaVersion`.
- `uint64` seeds above 2^53 round-trip exactly, because they decode into typed fields.

### CLI

- **Start:** `-seed N`, or nothing for a random seed reported on stderr, or `-load FILE`.
- **Mode:** `-season` (all remaining rounds), `-rounds N` (at most N batches), or nothing. With nothing, a new world shows the calendar and the `Continue` demo, and a loaded world shows its status only (read-only via `World.Pending()`).
- **Then:** `-save FILE`, written only if the run succeeded.
- **Rejected combinations:**
  - `-load` with `-seed` (the save's configuration is never silently overridden);
  - `-season` with `-rounds`;
  - `-rounds` < 1;
  - an empty `-load` or `-save` file name.
- **Errors** are reported as `simulate: load FILE: storage: malformed save: …`, or `save FILE: …`.
- **Command IDs** continue from `World.NextCommandID()`, so a loaded career never reuses an ID.

### Verification

- **Round trips at every save point** (creation, between rounds, pending, season complete, two leagues pending):
  - JSON is lossless;
  - `Restore(s).Snapshot() == s`, so loading runs no tasks, resolves nothing, allocates no IDs and doesn't change the revision;
  - `Validate` passes;
  - tables, schedules and the summary are identical.
- **Save after round 7, then finish:**
  - rounds 8–14 `RoundsResolved`, including all goal details, are identical;
  - so are the final tables, clock and revision;
  - and so is the *entire* final `WorldSnapshot`, compared with an uninterrupted season driven by the same command sequence;
  - the same holds through real files in `storage`, and through the CLI (rounds 8–14 output and the final table).
- **Pending save:** the loaded world reports the identical `FixtureRoundReady`, and resolving it with the same command gives the identical result and world. The CLI's pending save, loaded with `-rounds 1`, matches a fresh run's round 1.
- **Retry after load:**
  - a previously successful command returns its original result, with the world unchanged;
  - reusing its ID with a different revision or different rounds is `ErrCommandIDReused`;
  - `NextCommandID` continues after the log;
  - detailed outcomes from the log (goals) survive a save.
- **Allocators:** after load, a new season's fixture IDs, task IDs and payload IDs are all above the saved allocators, and no existing ID is reissued. Snapshots whose allocators are below existing IDs are rejected (competitions, scheduler, payloads).
- **Memory isolation:** mutating an exported snapshot doesn't change the world; mutating the input after `Restore` doesn't change the restored world; two worlds restored from one snapshot are independent.
- **Incompatible versions:** all 5 must-match versions are rejected. Provenance differences are accepted and preserved.
- **Invalid state:** 25 broken-reference and inconsistency cases in `app`, 20 in `competitions.Restore` and 7 in `RestoreScheduler`, each rejected with no world returned.
- **Deliberate-bug check:** removing command-log validation let 9 invalid cases through, so the tests have teeth.
- **Files:**
  - every truncated prefix of a save is rejected;
  - so are a corrupted payload (checksum), trailing data, unknown envelope and payload fields, other formats and other schemas;
  - invalid state and incompatible versions are rejected through `Load`;
  - failed saves keep the previous file loadable and leave no temporary files;
  - replacing an existing save works and keeps mode 0644;
  - a missing directory produces an error naming the path.
- **CLI:** rejected option combinations, clear load and save errors, and a status view that leaves the save file byte-identical. A manual run did a 7-round save, status, load `-season`, a pending save, loading and re-saving over the same file, and truncated, future-schema, edited, missing-file and incompatible-option cases.
- All earlier tests and goldens (world fingerprint, schedule hash, season results hash) are unchanged.

### Limitations

- No save migrations: any `SchemaVersion` or must-match version change makes older saves unloadable, with an explicit error.
- No mid-match saves and no `MatchCheckpoint` support.
- Detailed match outcomes are persisted only inside the command log. There's no separate match-record store, and nothing compacts the log.
- JSON is simple but verbose. The checksum detects accidental corruption, not tampering.
- Directory fsync is best effort. Atomic replacement relies on POSIX rename semantics and hasn't been tested on Windows.

## Milestone 7: user club and lineup commands (done)

A career can be created with a club to manage. When a batch is pending, the manager can submit a lineup (starting XI with roles, bench, mentality) for their club's fixture. `ResolveRounds` plays it; a fixture without one is played with the AI selection, which is the explicit default.

```sh
go run ./cmd/simulate -seed 42 -club 3 -season                        # managed, AI default: same results as without -club
go run ./cmd/simulate -seed 42 -club 3 -mentality attacking -season   # submit a lineup before each of club 3's matches
```

### Changes

| Package | Change |
| --- | --- |
| `internal/selection` | New. `Slot`, `Lineup` (`Validate`, `Clone`, `Equal`, `Players`), `Entry`, `Store` (`New`, `Submit`, `Lineup`, `Entries`, `Snapshot`) |
| `internal/app` | `Config.UserClub`; `UserClub()`; `FixtureRoundReady.UserFixtures`; `SubmitLineup` command and `LineupSubmitted`; `SuggestLineup`, `SubmittedLineup` queries; `SelectedBy` and `MatchReport.Selected`; errors `ErrUnknownClub`, `ErrNoUserClub`, `ErrNotUserFixture`, `ErrFixtureNotPending`, `ErrInvalidLineup`; `Validate` checks lineups |
| `internal/app` (save) | `WorldSnapshot.UserClub`, `Lineups`; the command log is now `ResolveCommands []ResolveRecord` and `LineupCommands []LineupRecord` (was `Commands []CommandRecord`) |
| `internal/storage` | `SchemaVersion` = 2 |
| `cmd/simulate` | `-club N`, `-mentality M`; managed fixtures are marked in round output and status |

Contract changes: `CommandRecord` became `ResolveRecord`, `WorldSnapshot.Commands` became `ResolveCommands`, and `FixtureRoundReady`/`MatchReport` gained fields. Output without `-club` is byte-identical to milestone 6. The world fingerprint, schedule hash and seed-42 season golden are unchanged.

### Decisions

- **New `selection` module** is the architecture's "Squad and tactics" owner. It holds at most one lineup per (fixture, team) and validates its shape:
  - 11 starters with valid roles and exactly one goalkeeper;
  - non-zero player IDs used once across starters and bench;
  - a valid mentality.

  It imports the `matches` contract (roles, `Tactics`) like `ai` does, so lineups reach the engine without translation. `CLAUDE.md` records this exception.
- **The user club is career configuration owned by `app`**, fixed at creation. Zero means none, so every club is AI-managed. Choosing a club does not touch world generation or scheduling, so the fingerprint and fixtures are the same with or without one.
- **Where a lineup can be submitted:** only for a user-club fixture in the pending batch, which is the pre-match decision point. By then the squad can't change before the match. Lineups for future fixtures (templates or preferences) are deferred.
- **Out of position is allowed:** any player may start in any role, with unchanged ratings. That includes an outfield player in goal, which the match contract permits and the model punishes through Goalkeeping. Bench players play in their natural role, as with the AI.
- **What the app checks** (`lineupInput`), at submission and again at resolution:
  - the bench is within the league's `MaxBench`;
  - every player is employed by the user team and has a profile.

  Starters take the lineup's roles and profile ratings; the bench takes natural roles.
- **Resubmitting** (a new command ID) replaces the stored lineup. Lineups are kept after the match as the record of what was chosen.
- **`SuggestLineup`** returns the AI selection as a `Lineup`, so clients can start from the default. Submitting it unchanged plays exactly the AI match; a test proves this against the season golden.
- **Commands.** `SubmitLineup{ID, ExpectedRevision, Fixture, Lineup}` follows the `ResolveRounds` rules:
  - checks run first: ID, retry, revision, user club, fixture pending and the user's, lineup against squad and rules;
  - then one `selection.Submit` call commits;
  - then the revision increments and the command is recorded.

  Command IDs share one space across kinds, so a lineup command's ID reused for `ResolveRounds` (or the reverse) is `ErrCommandIDReused`. Because a submission moves the revision, clients resolve with `Revision()` rather than the revision reported in `FixtureRoundReady`.
- **`MatchReport.Selected [2]SelectedBy`** (durable: 1 = AI, 2 = manager) records per side which selection was played.
- **Continue.** Every batch still stops `Continue`; the user club plays every round of an 8-team league. `FixtureRoundReady.UserFixtures` marks the batch fixtures that need, or can take, the manager's decision. Auto-resolving batches that contain no user fixture is deferred until a world exists where that happens in normal play (byes, cups, several leagues).

### Save schema v2

- `UserClub` (0 = none) and `Lineups` (`[]selection.Entry`, by fixture and team) are authoritative.
- The command log is one typed list per command kind, each in ascending ID order: `ResolveCommands` and `LineupCommands`. IDs are unique across both lists.
- Schema v1 saves are rejected with `ErrUnsupportedSave` ("schema version 1, this build reads 2"). There are no migrations yet.
- **Restore validation** (`ErrInvalidSave`):
  - the user club is registered;
  - every lineup is valid in shape, belongs to the user team and is for a fixture that team plays;
  - that fixture has kicked off, and its bench fits the league rules;
  - a lineup whose match is still pending selects only current squad players;
  - each `MatchReport.Selected` value is valid and says "manager" exactly when a lineup is stored for that fixture and team;
  - each lineup command is valid in shape and for a user fixture that has kicked off, still has a stored lineup, and has a result revision within `(expected, saved]`.

### Verification

- **Default is AI:** with a user club and no lineups, all 56 reports say AI on both sides and the season equals the golden season.
- **User fixtures:** each of the 14 batches reports exactly the managed team's fixture; a world without a user club reports none.
- **Lineup is played:**
  - after a submission, the planned match input for the user side has exactly the submitted starters, roles, bench (natural roles), ratings and mentality;
  - the opponent side and every other fixture use the AI selection;
  - the old batch revision is rejected as stale.
- **Only the user's matches change:** over a full season of changed lineups (attacking, one forward swapped), every non-user fixture's detailed report (score, goals, selections) is identical to the AI season. At least one user match differs, and each lineup adds exactly one revision.
- **Suggestion equals default:** submitting `SuggestLineup` every round reproduces the golden season. Match details are identical apart from `Selected`.
- **Rejections leave the world unchanged** (17 cases): zero ID, stale or future revision, another club's fixture, an unknown or zero fixture, a later or already played fixture, 10 starters, 0 or 2 goalkeepers, an invalid role, a duplicate player, another club's player, an unknown player, no mentality, a bench over `MaxBench`. Also: no user club (submit and suggest), and suggesting for a non-pending fixture.
- **Retries:** the same ID and lineup returns the recorded result with no change; a different lineup, or an ID shared with a `ResolveRounds`, is rejected; a resubmission replaces the lineup and is the one played.
- **Save while pending with a lineup:**
  - the JSON round trip is lossless;
  - the user club, lineup, pending batch and lineup-command retry survive;
  - resolving gives an identical result and world;
  - a managed season played to the end after load keeps round-tripping.

  The same holds through real files in `storage` and through the CLI (7 managed rounds, save, load, finish = uninterrupted managed season).
- **Invalid saves:** 19 lineup, club and command cases are rejected with `ErrInvalidSave`.
- **Deliberate-bug checks** each made tests fail:
  - ignoring stored lineups at resolution;
  - skipping the `Selected` check on restore;
  - dropping `validateSelections`;
  - skipping the squad-membership check.
- **`selection`:** 14 invalid shapes, out of position and empty bench accepted, insert and replace order, all-or-nothing `Submit`, `New` rejects duplicates, and no shared memory in either direction.
- **CLI:**
  - `-club` without lineups prints the same results as a plain season;
  - `-mentality attacking` changes only lines involving the managed club and is reproducible;
  - option validation covers `-club` with `-load`, `-club 0`, an unknown club, `-mentality` without a mode, an unknown mentality, and `-mentality` without a managed club (new or loaded).
- Output without `-club` is byte-identical to the previous commit for the demo, `-season` and a save/load cycle.

### Limitations

- The user can't change anything during a match (substitutions, mentality at half time), although the session supports it. Resolution still runs every match to full time.
- There's no squad query with names, positions and ratings for choosing a lineup, beyond `SuggestLineup` and the existing summary.
- Lineups can't be prepared before kickoff, and there are no default tactics or formation preferences that carry over between matches.
- AI-only batches still stop `Continue`.
- A submitted lineup that became invalid before resolution would fail `ResolveRounds` rather than fall back to the AI. That can't happen yet, because nothing changes squads between submission and resolution.
- Players never tire between matches, so the same lineups are equally good all season.

## Milestone 8: basic condition (done)

Players now have a condition (fitness). Playing lowers it by minutes played, and a daily recovery task restores it. Condition weakens players in matches, and the AI rotates tired players out. A loaded managed career's status shows the squad with condition.

```sh
go run ./cmd/simulate -seed 42 -club 3 -rounds 3 -save career.json && go run ./cmd/simulate -load career.json   # squad with condition
```

### Changes

| Package | Change |
| --- | --- |
| `internal/medical` | New. `Store` (`New`, `Condition`, `Records`, `Snapshot`, `PlanExposure`, `PlanRecovery`, `Apply`), `Plan`, `Params` (`Drain`, `Recovery`), `Record`, `Exposure`, `Rest`, `MaxCondition`, `Version` = 1, `ErrStalePlan` |
| `internal/core/sim` | A cohort handler may schedule follow-up tasks, but only after its own cohort; anything else is `ErrNotAfterCohort` |
| `internal/matches` | `PlayerInput.Condition` (required, 1..`MaxCondition` = 10,000) |
| `internal/matches/simple` | `Params.ConditionFloorPer10k` (7,000); readiness multiplies effective ratings. `ModelVersion` = 2 |
| `internal/ai` | `Candidate.Condition`; players are ranked by `RoleScore × condition`. `SelectionVersion` = 2 |
| `internal/app` | Medical store (all players start fully fit); task kind `taskRecovery` (2); cohort dispatch by kind; exposure applied during `ResolveRounds`; `Squad(club)` query; `Versions.Medical` (must match); `WorldSnapshot.Medical` |
| `internal/storage` | `SchemaVersion` = 3 |
| `cmd/simulate` | Status of a managed career lists the squad with condition |

The world fingerprint and schedule hash are unchanged. The season golden changed deliberately and is now `9b3259c6…`. Three versions cover the change: `simple.ModelVersion` 2 (which also re-derives every fixture's random stream, because the engine version is part of it), `ai.SelectionVersion` 2 and the new `medical.Version` 1.

### Rules (`medical.Version` = 1, `DefaultParams`)

Condition is an integer per 10,000.

- **Match drain:** `minutes × (20 + (20 − stamina))`, never below `MinCondition` (2,000). With stamina 6, a full match costs 3,060; with stamina 18, it costs 1,980.
- **Daily recovery:** `300 + 10 × stamina`, capped at 10,000. With stamina 6 that's 360 a day; with stamina 18, 480.
- **Balance:** a stamina-9 player who plays 90 minutes every week holds about level. Fitter players recover fully, and less fit ones decline until rotated.
- **Seed-42 season:** at the end, conditions range from 6,760 to 10,000 (median 8,400). The AI started a different XI than in round 1 in 42 of 112 team-rounds.
- **In a match (simple engine):** readiness = `7,000 + 3,000 × condition / 10,000`, per 10,000. It multiplies every effective rating before in-match fatigue: condition 7,000 is −9% and 2,000 is −24%. Substitutes start from their own condition.
- **AI selection:** `fitScore = RoleScore × condition`, so a player at 70% is worth 70% of their role score. Starters, gap-fillers and bench all use it; ties still go to the lower ID.

### Decisions

- **Owner.** A separate `medical` package, not a store inside `players`: condition changes daily while attributes don't, and the architecture's Medical row will grow injuries and availability. It imports only `core/ids`. Stamina comes from `players` as detached input to each rule, so no derived rate is stored twice.
- **Two-step changes.** `PlanExposure` and `PlanRecovery` validate everything and compute the new values without touching the store; `Apply` commits.
  - A plan is tied to the store's generation, so a stale or replayed plan is `ErrStalePlan` and changes nothing.
  - This lets `ResolveRounds` stay all-or-nothing across two modules: plan exposure (can fail), then `competitions.CompleteRounds` (all-or-nothing), then apply exposure. The last step can't fail, because nothing touched medical in between; a failure there panics as a broken invariant.
- **Exposure.** Every participant loses condition for the minutes played (all 22 starters, since the AI makes no substitutions). Unused bench players are unaffected. Stamina comes from the match input's ratings.
- **The daily recovery task.**
  - It is one queued task with no payload, in the Preparation phase, due on whole days since the epoch; with the default epoch that's 00:00 UTC. The first is due at instant `Day`.
  - Its handler plans recovery for every player, schedules tomorrow's task, then applies. Scheduling is the only step after planning that can fail, and a failure there changes nothing.
  - It never interrupts `Continue`, so a single `Continue` call can now commit many cohorts.
  - Invariant (checked by `Validate` and on load): exactly one recovery task, due in `(Now, Now+Day]` on a whole day.
  - This is the first recurring task. Future weekly wages and monthly development follow the same pattern.
- **Scheduler guard.** While a handler runs, `Schedule` accepts only tasks at a later instant, or at the same instant in a later phase (these run in the same `RunUntil` call). This enforces the architecture's same-instant rule, and it guarantees that removing the finished cohort afterwards removes exactly the cohort's tasks.
- **Cohorts have one kind.** `handleCohort` rejects mixed kinds and dispatches kickoff (which interrupts) or recovery (which doesn't). Recovery and kickoffs never share a cohort, since they're in different phases.
- **The contract requires condition.** `Condition` 0 is rejected rather than read as "exhausted", so a producer that forgets the field fails loudly. The medical floor keeps real values at 2,000 or above, and `medical.New` rejects stored values outside `MinCondition..MaxCondition`.
- **`Squad(club)`** composes registry, players and medical per player, in ascending ID order. It is read-only.

### Verification

- **`medical`:**
  - rejects invalid stores: zero or duplicate player, condition above maximum or below the floor, invalid params;
  - drain grows with minutes and falls with stamina; floor, recovery and cap are exact; unexposed players are untouched;
  - 9 invalid plans rejected, and planning never mutates;
  - stale and replayed plans rejected without change;
  - no shared memory.
- **Scheduler:** a handler's 9 attempts to schedule into an earlier instant, an earlier phase or its own cohort are rejected. A self-rescheduling task and a later-phase follow-up run in order within one call. The guard doesn't apply outside a run.
- **Engine:** a home XI at condition 4,000 wins less and loses more over 2,000 seeded matches; a tired player's effective rating is lower; conditions 0 and 10,001 are rejected, and so is a zero floor. Existing invariant and balance tests pass at full condition.
- **AI:** a goalkeeper just tired enough gives way to the backup and becomes first substitute, carrying their condition. A barely tired better goalkeeper keeps the place. Condition 0 is rejected.
- **App:**
  - conditions after round 2 equal an independent computation from the same outcomes (all 88 participants), and nobody else changes;
  - daily recovery is exact for three days, with exactly one recovery task, correctly aligned, after each day; recovery never interrupts;
  - every planned input carries the medical condition, and the AI rotates in 42 team-rounds;
  - recovery over one `Continue` call equals recovery over many (the whole snapshot matches apart from the revision);
  - saving mid-recovery (three midnights after round 3, at 20:00) and finishing the season gives the identical world;
  - `Squad` agrees with the owning modules, with exactly the 11 starters tired after one match;
  - the 8 injected resolution failures now also prove conditions are unchanged, because the compared state includes medical records;
  - 11 invalid medical and recovery-task saves are rejected, and a medical version mismatch is `ErrIncompatibleSave`.
- **Deliberate-bug checks** each made tests fail: exposure not applied, recovery not applied, AI ignoring condition, engine ignoring readiness, scheduler guard removed.
- **CLI:** a managed status lists 20 squad rows with some players tired. The save/load season matches the uninterrupted one, and runs are reproducible.
- **Benchmarks** (same machine as milestone 4): a complete match ~33,900 ns/op with 13 allocs; stepping only ~31,500 ns/op with 0 allocs. `B/op` rose from 4,752 to 5,584 because each session player carries readiness.

### Limitations

- No injuries, availability or medical staff. Condition is the only health fact.
- Recovery ignores training, age and match congestion. With weekly rounds, condition matters mostly for low-stamina players.
- The AI still makes no in-match substitutions, so starters always play 90 minutes.
- There's no per-player match history; minutes played aren't stored outside the drain they cause.
- Condition can't be set or influenced by the manager, except by choosing who plays.

## Milestone 9: season rollover (done)

A career now continues past its first season. After the last round is resolved, a season-end task creates each league's next season: the same entrants, a new draw and new fixture IDs, starting 52 weeks after the previous season's first kickoff (season 2: Sat 2026-08-08 15:00 UTC). Past seasons stay in the store, and their tables and champions remain queryable. Condition recovery continues through the off-season.

```sh
go run ./cmd/simulate -seed 42 -season -save career.json   # season 1
go run ./cmd/simulate -load career.json                     # off-season status: season 1 champion, empty season 2 table
go run ./cmd/simulate -load career.json -season             # season 2 (fixtures F57-F112)
```

### Changes

| Package | Change |
| --- | --- |
| `internal/competitions` | `NewSeason`, `CreateLeagueSeasons` (all-or-nothing batch, canonical order); `CreateLeagueSeason` wraps a one-season batch |
| `internal/content` | `League.SeasonInterval` (default 52 weeks), `League.Rounds()`; `Validate` requires the next season to start after the last kickoff and at least 2 entrants. `LeagueVersion` = 2 |
| `internal/app` | Task kind `taskSeasonEnd` (3) with `SeasonEndPayload` records; `endSeasons`; `validateSeasons`; `Table(season)`, `History()` (`SeasonRecord`); `Tables()` stays current-season |
| `internal/app` (save) | `Payloads` renamed `KickoffPayloads`; `SeasonEndPayloads` added (IDs unique across both lists, one allocator) |
| `internal/storage` | `SchemaVersion` = 4 |
| `cmd/simulate` | `-season` and `-rounds` play only the current season and print that season's tables; status lists past champions |

The world fingerprint, schedule hash and the season-1 golden `9b3259c6…` are unchanged: rollover doesn't touch season 1. The content fingerprint changes because the league definition has a new field.

### Decisions

- **When the season ends.** The season-end task is due at the season's last kickoff, in the Consequences phase, with `StableOrder` = competition ID.
  - That's the same instant as the last round, but later in the phase order. Because `Continue` runs nothing while a round awaits results, the task can only run after the last round is resolved.
  - The handler rejects an incomplete season anyway, as a guard.
  - It doesn't interrupt `Continue`. After the last `ResolveRounds`, the next `Continue`, even to the same instant, runs it.
- **What it does**, per ending season in the cohort:
  - checks the payload, that the season is the league's current one and that it is complete;
  - takes the entrants from the ending season;
  - sets the first kickoff to the previous first kickoff + `SeasonInterval`, and rejects anything invalid or not in the future;
  - creates every next season in one `CreateLeagueSeasons` call.

  Only then does it delete the consumed payloads, advance each league's current season and schedule the new kickoff tasks and the next season-end task. That scheduling can't fail (kickoffs are validated and lie after this instant), so a failure there panics as a broken invariant.
- **Atomic batches.** Leagues whose seasons end at the same instant roll over as one cohort, and `CreateLeagueSeasons` makes their creation all-or-nothing. Seasons are created, and fixture IDs allocated, in (competition, season) order.
- **Draws.** Each season has its own stream `(seed, "competitions/fixtures", ScheduleVersion, competition, season)`, as before. Season 2 is a new draw, not a copy of season 1.
- **History is derived, not stored.** Ending a season records nothing new; its fixtures and results stay in `competitions`, and `Table(ref)` and `History()` derive standings and champions on demand. This keeps one owner per fact and avoids a copy that could drift from the results.
- **Current season.** `app` keeps each league's current season (`LeagueSnapshot.Season`). `Schedules()` and `Tables()` show current seasons, so after rollover they show the new, unplayed season.
- **CLI.** `-season` and `-rounds` capture the current seasons at the start, stop a day after their last kickoff (past the season end) and print those seasons' tables. A later run plays the next season, and `-rounds 20` stops after 14 rounds.
- **Validation** (`validateSeasons`, run by `Validate` and on load):
  - each league's seasons are exactly 1..current;
  - earlier seasons are complete, have the current entrants and each starts `SeasonInterval` after the previous one;
  - every competition season belongs to a league;
  - each league has exactly one season-end task: for its current season, at that season's last kickoff, in the Consequences phase, with `StableOrder` equal to the competition ID;
  - each season-end payload is used by exactly one task and names a current season.

  `validateCompetitions` now checks only current-season entry rules.

### Verification

- **Three consecutive seasons:**
  - season 2 starts 364 days after season 1 (Sat 2026-08-08 15:00) with the same entrants and a different draw;
  - 14 kickoff tasks are queued, and everyone is fully fit before season 2's first kickoff;
  - after season 2, season 3 exists; fixture IDs run 1–56, 57–112 and 113–168 with no repeats;
  - completed seasons have exactly one result per fixture, and season 3 has none;
  - `History` reports two champions equal to the tops of their tables, which match an independent recomputation;
  - registry, players, employment and the world fingerprint are unchanged.
- **Timing:** while the last round awaits results, `Continue` even 30 days ahead doesn't end the season. After resolution, `Continue` to the same instant ends it, with one new revision.
- **Saves:** a save before the season end (last round resolved, task queued) and one in the off-season (100 days later) each continue into an identical next season and world. New save points, the off-season and season 2 pending, round-trip losslessly.
- **Atomic rollover:** in a two-league world, a missing season-end payload for league 2 fails the cohort with no new season for either league. After repair, both roll over together (competition 1 gets F113–168, competition 2 gets F169–224).
- **Guards:** ending an incomplete season is rejected with no change. 10 invalid season states are rejected on load:
  - a missing season-end payload, or one for season 1, for an unknown season or reusing a kickoff payload's ID;
  - a season-end task that is early, in the wrong phase or missing;
  - season 1 removed, or a league set back to season 1;
  - an edited `SeasonInterval`, with the content fingerprint recomputed.
- **`competitions`:** an invalid batch (bad entrants, a duplicate in the batch, an existing season, bad timing) changes nothing. A batch in any order equals creating each season in canonical order. Season 2 pairings differ from season 1.
- **`content`:** league validation, including overlapping seasons, is rejected; a season starting one minute after the last kickoff interval is accepted.
- **Deliberate-bug checks** each made tests fail:
  - skipping the completeness check;
  - dropping `validateSeasons`;
  - not scheduling the new season's kickoffs;
  - starting the next season a week after the last round instead of `SeasonInterval` after the first kickoff.
- **CLI:**
  - `-season -save`, then status, then `-load -season` plays season 2 (Round 1 on 2026-08-08, F57–F112, "Final table … season 2", a champion);
  - the off-season status shows the season-1 champion;
  - `-rounds 20` plays 14 rounds.

### Limitations

- The same clubs play every season. There's no promotion or relegation, and no entrant changes between seasons.
- Nothing else happens at the season end: no prizes, no aging, no contract expiry, no squad turnover. Squads are identical every season apart from condition.
- The off-season is empty time. The next season is scheduled at the season end, about 9 months ahead.
- Leagues with different `SeasonInterval` or timing roll over independently. There's no shared season calendar or cross-competition dependency yet (cups, qualification).
- There's no inbox or event notifying the user that a season ended; the client sees it through `History`, `Tables` and `Schedules`.

## Milestone 10: committed domain events and an inbox (done)

Every committed change now appends typed, past-tense domain events to a journal in the same commit. An inbox read model for the manager consumes them. The status view of a loaded career shows the latest messages:

```text
Inbox (latest 10 of 30)
  Sat 2025-11-08 15:00 UTC  matchday: Founders League round 14, F54 v Wyrmsby Wanderers (home)
  Sat 2025-11-08 15:00 UTC  result: F54 0-0 v Wyrmsby Wanderers (home)
  Sat 2025-11-08 15:00 UTC  Founders League season 1 ended: champion Hollowick Wanderers; your position: 5
  Sat 2025-11-08 15:00 UTC  Founders League season 2 scheduled: first kickoff Sat 2026-08-08 15:00 UTC
```

### Changes

| Package | Change |
| --- | --- |
| `internal/events` | New contract. `Event` envelope (`ID`, `OccurredAt`, `Revision`, `Sequence`, `Cause{Kind, ID}`, `Kind`, `SchemaVersion`) with exactly one typed payload: `RoundStarted`, `MatchCompleted`, `LineupSubmitted`, `SeasonEnded` (final `Ranking`), `SeasonStarted`; `Validate`, `Clone`, `CloneAll`; `SchemaVersion` = 1 |
| `internal/inbox` | New read model. `Inbox` (`New`, `Apply`, `Messages`, `Offset`, `Snapshot`), `Message`, kinds Matchday/Result/SeasonEnded/SeasonStarted, `MaxMessages` = 200 |
| `internal/app` | Events staged by each commit and published with its new revision; `Events()`, `Inbox()` (`InboxItem` with labels); `validateJournal`; `Continue` counts a revision when the clock moves or any cohort commits |
| `internal/app` (save) | `WorldSnapshot.Events` (retained journal), `LastEvent`, `Inbox` (offset and messages) |
| `internal/storage` | `SchemaVersion` = 5 |
| `cmd/simulate` | Status prints the latest 10 inbox messages |

All goldens and every CLI mode except status are byte-identical to milestone 9. A season-1 save grows from about 139 KB to 169 KB.

### Events

| Kind | Emitted by | Cause | Payload |
| --- | --- | --- | --- |
| `RoundStarted` (1) | kickoff cohort | task | competition, season, round, pairings |
| `MatchCompleted` (2) | `ResolveRounds` | command | fixture, round, teams, official score |
| `LineupSubmitted` (3) | `SubmitLineup` | command | fixture, team |
| `SeasonEnded` (4) | season-end cohort | task | final ranking (champion first) |
| `SeasonStarted` (5) | season-end cohort | task | next season, first kickoff, entrants |

A managed 8-team season emits 86 events (14 + 56 + 14 + 1 + 1). Daily recovery emits none: per the architecture, numeric adjustments aren't broadcast one by one.

### Decisions

- **Typed union, no `any`.** An event is one envelope with a pointer field per payload kind. `Validate` requires exactly the payload that matches `Kind`, with non-zero identities. JSON omits the unused pointers. `events` imports only core, so any read model can consume it, and it uses plain `Season`/`Round` numbers instead of `competitions` types.
- **Emit, then publish.** A handler or command calls `emit` only after its all-or-nothing module call has succeeded. After the revision increments, `publish` assigns sequential event IDs, the revision and a per-commit `Sequence` from 1. It then lets the inbox consume the events, appends them to the journal and trims it.
  - Failed commands, retries of recorded commands, reads and a paused `Continue` emit nothing. As a guard, `Continue` also drops anything a failing cohort handler staged.
- **Continue revision rule.** Previously `Continue` counted a revision when the clock moved or the queue length changed. Now it counts one when the clock moves or any cohort commits. One `Continue` call that commits several cohorts publishes all their events at one revision. Cohorts committed before a later failure still publish.
- **Causes.** Task-caused events carry the task ID, command-caused events the command ID. Validation requires a recorded command or an allocated task ID; tasks are consumed, so only the allocator can be checked.
- **The inbox is a projection.**
  - It keeps a consumer offset: events at or before it are skipped, the next must be `offset + 1`, and a gap or an invalid event rejects the whole delivery with no change. Delivering in any chunking, or twice, gives the same inbox.
  - It keeps messages only for the managed team (matchdays, results), plus every season message. Without a user club, only season messages appear.
  - Messages store IDs; `app.Inbox()` resolves display names.
  - It is updated synchronously in `publish`, so it's always caught up. Restore requires `Offset == LastEvent`.
  - It is bounded at 200 messages, oldest dropped. That's about 6 seasons, since a managed season produces 30 messages.
- **World creation emits no events.** The initial world is state, not a change, so there's no "season 1 scheduled" message. The first events come from the first kickoff.
- **Retention.** The journal keeps the newest `journalRetention` (1,000) events, about 11 seasons. Only events the inbox has consumed are dropped, which is always all of them, because the inbox is synchronous. Event IDs stay sequential across trimming, because the allocator is saved. Durable history doesn't depend on the journal: results, seasons and champions live in `competitions`.
- **Persistence.** The retained journal, the allocator and the inbox (offset and messages) are saved. Messages are persisted rather than rebuilt, because trimmed events can't be replayed.
- **Load-time validation** (`validateJournal`, also part of `Validate`):
  - **Journal shape:** event IDs are contiguous and end at the allocator; at most `journalRetention` events are kept; each event is valid.
  - **Ordering:** revisions and times never decrease and never exceed the world's; the sequence restarts at 1 for each commit.
  - **Causes:** each names a recorded command or an allocated task.
  - **Payloads** agree with the owning modules: a started round exists, has kicked off at that instant and has those pairings; results equal the official ones, including their time; a lineup's team plays the fixture; season rankings equal the final standings; a started season has those entrants and that first kickoff.
  - **Inbox:** it has consumed every event, and while the journal is complete (still starting at event 1), it must equal a rebuild from the journal.

### Verification

- **Journal:**
  - a managed season gives exactly 14/56/14/1/1 events with contiguous IDs;
  - each `ResolveRounds` result matches its `MatchCompleted` events (same command cause, revision, instant and fixture order);
  - the season events share their task cause, and the ranking's head is the table's champion;
  - `Validate`'s cross-checks pass.
- **Nothing else emits:** reads, a paused `Continue`, a stale `ResolveRounds`, an invalid lineup and a retried command leave the journal unchanged. All existing atomicity tests (8 resolution faults, failed kickoffs, failed cohorts, failed season ends) now also compare events and the inbox.
- **Inbox:**
  - 14 matchdays, 14 results, 1 champion and 1 season start;
  - each result's goals and venue are checked against the official result, and the season message's champion and position against the table;
  - rebuilding from the journal in chunks of 1, 7 or 50 (re-delivering every prefix), or all at once, equals the live inbox;
  - an unmanaged world has no match messages.
- **Saves:** saving between every lineup and result for the first 40 events, then finishing, gives the same journal and inbox as never saving. Every earlier save test now round-trips the journal and inbox too.
- **Retention:** with retention 30, the journal keeps events 57–86, the allocator and offset stay at 86, the inbox keeps its 30 messages, and the world round-trips.
- **Invalid saves:** 14 cases are rejected:
  - a missing event, the allocator ahead of the journal, an unknown kind, a payload of another kind;
  - an edited score or pairing, a lineup event for a team not in the fixture;
  - an unknown command cause, an unallocated task cause, a future revision, a broken sequence;
  - an inbox behind the journal, an edited inbox message or a dropped one.
- **`events`:** 16 invalid envelopes and payloads are rejected, and clones share nothing.
- **`inbox`:** exact messages for a known journal; idempotent and chunk-independent delivery; gaps and invalid events rejected atomically; the 200-message bound; restore validation (6 cases plus match messages without a team); copies.
- **Deliberate-bug checks** each made tests fail: the inbox not fed, a wrong score in `MatchCompleted`, `validateJournal` removed, a retried command emitting.
- **CLI:** a managed status shows the latest 10 messages including results and the round-7 matchday; the off-season status shows the season-ended and season-scheduled messages.

### Limitations

- No read or unread state, and no way for the user to dismiss or act on a message. The inbox has no commands yet.
- Matchday messages are informational. The decision itself is still the pending batch (`FixtureRoundReady.UserFixtures`).
- There's one consumer. Multiple consumers with independent offsets, and asynchronous or lagging projections, would need trimming by the minimum offset and a catch-up on load.
- There are no events for recovery, and no player-level events (no player history).
- Event payload versions are all 1 and there are no migrations, like the rest of the save.

## Interactive mode: `cmd/play` (done)

A terminal game loop over the existing app API, so the game can actually be played. No simulation code changed: every goal, table and golden is identical.

```sh
go run ./cmd/play                    # new career: random seed, choose a club at the prompt
go run ./cmd/play -seed 42 -club 3   # reproducible new career
go run ./cmd/play -load career.json  # resume (the save must have a managed club)
```

| Command | Effect |
| --- | --- |
| `status` (`s`) | date, season progress, the waiting match or the next one |
| `squad` | ID, position, overall, condition, and whether the player is in the current XI or on the bench |
| `table` (`t`), `fixtures` (`f`) | league table with your row marked; your fixtures with results (W/D/L) |
| `inbox` (`i`) `[N]` | latest N messages |
| `lineup` (`l`) | the waiting match's lineup: slot, role, player, ratings, condition, out-of-position flags, bench |
| `swap A B`, `role P GK\|DF\|MF\|FW`, `mentality` (`m`) `M`, `reset` | edit the lineup draft |
| `continue` (`c`) | if a match is waiting, play it (submitting the draft if edited), print full time, scorers, other results and new inbox messages; otherwise advance to your next matchday |
| `season` | play the rest of the season (an edited draft is used for the waiting match only) |
| `save [FILE]`, `quit` (`q`), `help` | `quit` warns once about unsaved progress; end of input discards it |

### Decisions

- **A client, not a new layer.** `cmd/play` uses only public queries and commands: `Pending`, `SuggestLineup`, `SubmittedLineup`, `SubmitLineup`, `ResolveRounds`, `Continue`, `Squad`, `Schedules`, `Table`, `Inbox`, and `storage.Save`/`Load`. The boundary test gives it its own import entry.
- **The draft is client state.** Viewing or editing the lineup changes nothing in the world. The draft starts from the submitted lineup, or else from `SuggestLineup`. Every edit is re-validated with `selection.Lineup.Validate` and rejected (with the reason) if it would break the shape, e.g. two goalkeepers. It becomes one `SubmitLineup` only when the match is played, and only if it was edited, so an untouched match stays "selected by AI". Saving doesn't store an unplayed draft, and the game says so.
- **`continue` does the next thing.** It plays the waiting batch, or advances with `Continue(now + 400 days)` to the next batch. Batches without the user's club are resolved automatically on the way. Because nothing interrupts at the season end, one `continue` after the last round crosses the off-season to the next season's matchday. New inbox messages (season ended with your position, next season scheduled) are printed along the way.
- **`swap`** exchanges any two squad players' places (XI slot, bench spot, or unselected). A starter slot keeps its role, so swapping a defender into a forward slot plays them out of position, which the lineup view flags. `role` changes a starter's role.
- **New careers** show the seed and the flags to reproduce them.

### Verification (`cmd/play/main_test.go`, scripted stdin)

- **Choosing a club:** invalid IDs are rejected and re-prompted.
- **Edits reach the match:** `swap 59 60`, `mentality attacking` and `role 49 mf` are played exactly. After saving, the loaded world's submitted lineup for fixture 3 has player 60 in slot 11, player 49 as a midfielder and attacking mentality. No lineup is submitted for the following, unedited match, and a session that only viewed the lineup submits nothing.
- **Reproducibility:** the same script prints identical output twice. Every scorer has a name, from either club's squad.
- **Save and resume:** the loaded session reports 1 of 14 rounds played and the next fixture, and isn't flagged unsaved.
- **Mistakes:** 11 kinds (no waiting match, an unknown command, unknown or unselected players, bad usage, two goalkeepers, a non-starter role, a bad role or mentality, a bad inbox count) print an error, and the lineup stays the AI suggestion.
- **`season`:** prints 14 results and the final table. The following `continue` shows the season-ended and season-2-scheduled messages and stops at the 2026-08-08 matchday.
- **Startup and exit:** end of input reports discarded progress. Bad arguments are rejected: `-load` with `-club`, a missing file, an unknown club, stray arguments, and a save without a managed club.

### Limitations

- There's still nothing to decide during a match: matches play straight through.
- Lineups can only be edited once the matchday has arrived, not ahead of time, and there's no saved "preferred XI" that carries over. Each match starts from the AI's suggestion.
- It's plain line-based output with no colours or screen layout. Player attributes are shown in the lineup view only.

## One 100-point scale for ratings and condition (done)

Attributes, overall and condition now use a 100-point scale everywhere: content, generation, storage, the match engine, AI, medical rules, saves and every screen. An intermediate step that only converted ratings for display was replaced by this change. Numbers quoted in earlier milestones (1–20 ratings, condition per 10,000) describe the model as it was then.

| Package | Change | Version |
| --- | --- | --- |
| `internal/players` | `MaxRating` = 100; `Overall()` (integer 1..100, rounded half up) replaces `OverallTenths`; the display helpers are removed | – |
| `internal/content` | Attribute ranges mapped 1→1 … 20→100 (e.g. GK goalkeeping 10–18 → 48–90) | `content.Version` 2 |
| `internal/matches` | `MaxRating` 100; `MaxCondition` 100; `PlayerInput.Condition` is `uint8` (1..100) | – |
| `internal/matches/simple` | A rating counts as 2 model units (same magnitudes as the old tenths); fatigue per 100,000 (`FatigueBasePer100k` 300, step 2 per stamina point, cap 30,000); readiness `7,000 + 3,000 × condition / 100` | `ModelVersion` 3 |
| `internal/ai` | `Candidate.Condition` `uint8`; `fitScore` = RoleScore × condition (0..100) | `SelectionVersion` 3 |
| `internal/medical` | Condition `uint8` 0..100, `MinCondition` 20, stamina 1..100 | `medical.Version` 2 |
| `internal/app` | `SquadPlayer{Attributes, Overall, Condition}`, `ClubSummary.AverageOverall` | – |
| `internal/storage` | Save values change meaning | `SchemaVersion` 7 (the bump was missed here and made with the next milestone) |
| CLIs | Integer OVR and attributes; condition as `87%` | – |

**Medical rules.** Condition is whole points; rates are finer and rounded half up once per result:
- **Match drain:** `minutes × (2,000 + (100 − stamina) × 20) / 10,000`. A full match costs 31 at stamina 30, 27 at 50 and 20 at 90.
- **Daily recovery:** `(300 + 2 × stamina) / 100`, which is 3 points below stamina 25, 4 up to 74 and 5 from 75. So one day of rest gives whole points, while a match still reflects stamina finely.
- **Balance:** a stamina-45 player who plays every week holds level, as the stamina-9 player did on the old scale. At the end of the seed-42 season, conditions range 58–100 (median 79), and the AI started a different XI than in round 1 in 19 team-rounds.

**Engine balance** (seeded batches of 2,000; the test teams are now built from strengths 45/60/75 instead of 9/12/15):
- equal teams: 2.77 goals per match, 808 home wins, 686 away wins, 506 draws;
- weak home against strong away: the away team wins 74%;
- attacking 7,573 goals against defensive 3,755;
- a home XI at condition 40 wins 557 instead of 808.

**Goldens** (updated deliberately):
- world fingerprint `5efa08bf…` (content v2);
- season `27ecee6a…`;
- the schedule hash is unchanged, because it doesn't depend on players.

**Tests.** Rescaled everywhere, plus new ones:
- exact rounding of drain and recovery (`TestRoundingOfWholePoints`);
- `Overall` rounding at .33 and .67;
- range rejections at 101.

The `cmd/play` scripts use new player IDs, because the world is newly generated. All checks and CLI modes pass.

**Trade-offs.**
- Condition in whole points means daily recovery has only three steps (3/4/5). Finer recovery would need fractional state, which the 100-point rule excludes.
- Readiness, fatigue and drain rates are internal fixed-point multipliers, not scales users see.

## Milestone 11: user in-match decisions (done)

The manager can now play their own match live: stop at any minute, see events and the score, make substitutions and change mentality, then finish. Half time always stops for a decision. Not deciding means "no change", so the explicit default is the kickoff selection played unchanged.

```text
> watch                      KICKOFF ... 21'  GOAL  DUN  Dario Kowal (1-0) ... HALF TIME
> sub 60 59                  45'  SUB   DUN  Nils Holm on for Dario Kowal
> mentality attacking        45'  TACT  DUN  now attacking
> watch 70 / watch           ... 89'  GOAL  HOL ... FULL TIME
> continue                   the round is recorded; other results and inbox as before
```

### Changes

| Package | Change |
| --- | --- |
| `internal/app` | `PlayMatch{ID, ExpectedRevision, Fixture, ToMinute}` and `MatchDecision{..., Command matches.MatchCommand}` commands; `MatchStepped` result; `LiveMatch` view and query; `LiveStop`; errors `ErrNoLiveMatch`, `ErrMatchInProgress`, `ErrMatchDecision`. `ResolveRounds` continues a live match; `SubmitLineup` is rejected once the match is live; `validateLive` |
| `internal/app` (save) | `Live *LiveSnapshot{Fixture, Stops}`; `PlayCommands`, `DecisionCommands` |
| `internal/storage` | `SchemaVersion` = 7 (also covers the missed bump of the 100-point rescale) |
| `cmd/play` | `watch [MIN]`, `sub OUT IN`, live `mentality`, live `lineup` (on the pitch, bench states, players taken off); `status` shows `LIVE`; `swap`/`role`/`reset` are refused after kickoff |

No version bumps: matches that aren't played live are unchanged, and all goldens hold.

### Decisions

- **Replay log, not a stored session.** The world keeps only `liveState{fixture, stops[{Minute, Commands}]}`. Whenever the session is needed (the next step, the view, resolution, validation), `replay` rebuilds it from three things: the frozen match input from `prepareBatch`, the fixture's random stream (`matchRandom`) and the stops.
  - Each stop is exactly one `Advance` to its minute, which must reach it exactly as `PlayMatch` recorded it.
  - Then come its commands, then a no-op `Advance` that delivers their events.
  - This works because sessions are deterministic, and chunking the same stops and commands never changes a match (proven in milestone 4).
  - Mid-match saves therefore need no engine checkpoint; the simple engine still has none. A restored save is validated by replaying it.
  - The cost is one replay (about 35 µs) per step, which is negligible.
- **The input is frozen.** While a match is live, the pending batch blocks `Continue`, and `SubmitLineup` for that fixture is `ErrMatchInProgress`. Nothing that feeds the match input (squad, lineup, condition) can change, so every replay sees the same input.
- **Half time is mandatory.** `PlayMatch` makes one engine `Advance`, which stops at 45 when crossing it. The recorded stop is the minute actually reached, and the next call continues. `ToMinute` must be after the current minute and at most 90. Reaching 90 finishes the session, but the result is official only through `ResolveRounds`.
- **Decisions** are for the manager's side only; the other side is rejected before the engine sees it. The engine validates everything else: substitutions left, player on the pitch or on the bench and unused, goalkeepers like-for-like, a new mentality, not finished. A rejected decision changes nothing. An accepted one is appended to the current stop and takes effect from the next minute.
- **Resolution.** `ResolveRounds` replays the live match and advances it to full time. Every other fixture is simulated as before, the whole batch stays all-or-nothing, and the live state is cleared in the commit. Condition exposure uses the outcome's participation, so a substitute and the player they replaced each lose condition for their own minutes.
- **Commands and events.** Both new commands take a `CommandID` and `ExpectedRevision`, move the revision and are recorded for retries, including the returned view. Live steps emit no journal events, because the architecture treats match events as match-local until the result is final. `MatchCompleted` still comes from `ResolveRounds`.

### Verification

- **Equivalence:** playing live to 20, 45, 70 and 90 without decisions, then resolving, gives exactly the direct resolution. That holds for every match report (ignoring command IDs and revisions) and every condition. The live events' goals equal the final report's goals, and the `LiveMatch` query equals the last step.
- **Half-time substitution:**
  - `PlayMatch` to 60 from kickoff stops at 45 with a decision required;
  - the substitution is shown in the view (on the pitch, substitutions used) and as an event at 45;
  - every other fixture is identical to the direct resolution;
  - the substitute and the replaced player each lose exactly `Drain(45, stamina)`.
- **Mentality:** a change at 60' shows in the view and as an event at 60.
- **Rejections leave the world unchanged:**
  - `PlayMatch` with a zero ID, a stale revision, another fixture, a minute back in time, the same minute or minute 91;
  - decisions for the opponent, for a player not on the pitch, for the same mentality, an unknown kind, a goalkeeper swapped for an outfielder, and with no live match;
  - a lineup submitted during the live match.

  Also: substitutions beyond the limit, a decision after full time, and playing before kickoff.
- **Retries:** both commands return their recorded results unchanged, and reused IDs are rejected across kinds.
- **Saves:** saving at half time after a substitution round-trips losslessly, with the same `LiveMatch`. Continuing to 75 and resolving on both worlds gives identical results and snapshots.
- **Invalid saves:** 8 live states are rejected: another fixture, no stops, minutes not increasing, a first stop past half time, a decision for the opponent, an invalid substitution, a play record for another fixture, and a decision record with a future revision.
- **Deliberate-bug checks** each made tests fail: resolution ignoring the live match, replay dropping decisions, `validateLive` removed, lineups not frozen.
- **CLI:**
  - a scripted live session shows kickoff, half time, the substitution and tactic lines, 70', full time and the confirmed round;
  - a session saved at half time and resumed reports `LIVE 45'` with 2 substitutions left and the substitute on the pitch, and ends with byte-identical output to the uninterrupted session from 70' on;
  - 6 mistakes are reported.

### Limitations

- The AI makes no in-match decisions for either side, including the manager's opponent.
- There are no injuries or forced substitutions, stoppage time, extra time or penalties. The only mandatory stop is half time.
- Recorded `MatchStepped` results (with the full view) make the save grow by a few KB per live match. They're kept for exact retries, and nothing compacts them yet.
- Only one live match can be in progress: the manager's fixture in the pending batch.

## Milestone 12: contracts and a finance ledger (done)

Every player now has a contract (weekly wage, expiry), and every club has a ledger. Wages are paid weekly, home matches earn gate receipts, and each balance is derived from the ledger. The balance and wage bill appear in `status`, wages and contract ends in `squad`, and the ledger under the new `finances` command.

```text
Balance 2,000,000.00 | weekly wages 32,890.00
  56  MF  Callum Ibsen    77  100%     2,660.00  to 2026
Sat 2025-11-08 15:00 UTC   gate receipts F54     250,000.00     3,157,980.00
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/core/money` | New. `Money` (int64 minor units, 100 per unit), `Units`, checked `Add`/`Neg`/`Sum` (`ErrOverflow`), `String` ("-1,234,567.89") | – |
| `internal/employment` | `Contract{Expires, WeeklyWage}` in each `Assignment` (validated: positive wage, positive valid expiry; the end is exclusive); `WageBill(club)` with overflow checks | – |
| `internal/content` | `Economy{OpeningBalance 2,000,000, GatePerHomeMatch 250,000, WageReference 1,600 at ReferenceOverall 60, WageVariationPct 20, ContractYears 1–4}`; `Economy.Wage` | `content.Version` 3 |
| `internal/worldgen` | `ContractTerms{Player, Years, WeeklyWage}`, drawn after each player's attributes from the same stream | `worldgen.Version` 2 |
| `internal/finance` | New. Ledger store: `Entry{ID, Club, At, Kind, Amount, Fixture}`, kinds opening/wages/gate; `Plan`/`Apply` (stale plans rejected), `Balance`, `Accounts`, `Entries`, `Entry`, `All`, `Snapshot` | – |
| `internal/core/sim` | A cohort is the consecutive queued tasks sharing (DueAt, Phase, **Kind**) | – |
| `internal/events` | `LedgerPosted{Entries[{Entry, Club, Kind, Amount, Balance, Fixture}]}` (kind 6) | – |
| `internal/app` | Contracts anchored at creation; accounts opened; task kind `taskWages` (4); gate receipts in `ResolveRounds`; `validateFinance`; `Finances(club)`; `SquadPlayer.Contract`; `WorldSnapshot.Finance` | save `SchemaVersion` 8 |
| `cmd/play` | Balance in `status`, wage and contract in `squad`, `finances [N]` | – |

**Goldens.** The worldgen version is part of every generation stream's key, so bumping it re-derives the whole generated world, club names included. The world fingerprint (`27d691ea…`) and the season golden (`7e0c0217…`) were updated deliberately, and the CLI tests use the new club 3 (Quillford FC) and player IDs. The fixture draw depends only on team IDs, so the schedule hash is unchanged.

### Decisions

- **Money** is int64 minor units with overflow-checked arithmetic everywhere it's summed: wage bills, balances, plans. `Units` panics on overflow and is for constants only. It's formatted without a currency symbol.
- **Contracts belong to `employment`**, because employment owns the relationship.
  - Generation doesn't know the calendar, so worldgen draws `Years` and the wage, and `app` anchors them at creation. A contract of N years ends at 00:00 on the first of the epoch's month, N years on (2027-07-01 for 2 years). The interval is half-open, per the architecture.
  - `Snapshot.Assignments` from worldgen have a zero contract; `withContracts` fills it in.
- **Wages:** `WageReference × (overall / 60)²`, varied uniformly by ±20%, rounded to the nearest 10 units, at least 10. A 60-overall player costs about 1,600 a week, and the default squad about 33,000.
- **The finance ledger** is the only owner of money.
  - A club's balance is the sum of its entries; it's rebuilt on restore and never stored.
  - Each account opens with an opening entry at instant 0.
  - Kinds carry sign rules: wages are negative; gate receipts are positive and name the fixture; only gate receipts name a fixture.
  - Balances may go negative. Financial difficulty is an allowed outcome, but nothing reacts to it yet.
- **Weekly wages:** one task (kind 4), in the Preparation phase with `StableOrder` 1, due on whole weeks since the epoch. The first run is at instant `Week`.
  - It posts one entry per club for its current `WageBill`: plan, then schedule next week, then apply.
  - It emits one `LedgerPosted` event, with no inbox message: a weekly message would crowd out results, and the balance is always in `status`.
  - Invariant: exactly one wage task, due in `(Now, Now+Week]` on a whole week.
- **Cohorts by kind.** Wages and daily recovery fall due at the same midnight in the same phase. Previously a cohort was all tasks at one (instant, phase), and mixing kinds was an error. Now each kind runs as its own cohort, in queue order, each all-or-nothing. A failure in a later cohort leaves earlier ones committed (they're independent). Kickoffs of several competitions are still one cohort.
- **Gate receipts** pay the home club `GatePerHomeMatch` for every resolved league match. The plan is made after the outcomes are checked, then `CompleteRounds`, then medical and finance apply. The whole batch stays all-or-nothing, and one `LedgerPosted` follows the batch's `MatchCompleted` events.
- **Balance of the economy:**
  - 7 home games × 250,000 = 1.75 million a season, against about 1.71 million a year in wages (52 weeks of an average squad);
  - the seed-42 club 3 ends season 1 at 3.16 million, and the off-season weeks bring it back toward break-even.
- **Validation** (`validateFinance`, on `Validate` and load):
  - accounts exactly match the registered clubs, and each opened at 0 with the content's opening balance;
  - no entry lies in the future;
  - wage entries fall on whole weeks, at most one per club and week;
  - every gate entry pays the content's amount once, to the home club of a fixture with an official result, at the time the result was recorded, and every official result is paid;
  - exactly one wage task is queued;
  - every `LedgerPosted` event matches its ledger entries, including the balance after each.

### Verification

- **`money`:** overflow detection on `Add`, `Neg` and `Sum`, a `Units` panic, and formatting including both int64 extremes.
- **`employment`:** 4 new invalid-contract cases, and a wage bill with an overflow case.
- **`content`:** 5 invalid economies, and the wage formula at known points (60 → 1,600; ±20%; 30 → 400; 90 → 3,600; floor 10; rounding to 10s).
- **`worldgen`:** every contract is within the year range and wage band, and all lengths 1–4 occur.
- **`finance`:**
  - balances equal the entry sums, including negatives, and survive a restore;
  - 10 invalid posting sets and a posting back in time are rejected without change, including overflow and "later posting overflows";
  - stale and replayed plans are rejected;
  - 6 invalid ledgers are rejected.
- **Scheduler:** same-instant tasks of three kinds form three cohorts in queue order, and a failing third cohort leaves the first two committed.
- **App:**
  - five weeks give exactly five wage entries per club at weeks 1–5, each equal to the bill, with the right balance and 5 wage events of 8 entries;
  - a season gives exactly 7 home receipts per club, and retrying three resolve commands pays nothing;
  - the off-season has at least 35 wage runs;
  - balances always equal the entry sums, the world validates and round-trips;
  - a wage bill overflow fails the run with no entry, no event and no reschedule, and pays exactly once after repair;
  - the squad shows contracts ending on 1 July of 2026–2029;
  - `Finances` agrees with the modules;
  - 11 invalid finance states are rejected on load: an edited opening or gate amount, repeated wages, a gate paid twice, a gate for an unplayed fixture, a missing gate, an account for no club, a missing or off-schedule wage task, a contract without a wage, and an edited ledger event.
- **The existing atomicity tests** (8 resolution faults, failed cohorts, season ends) now also compare the finance snapshot.
- **Deliberate-bug checks** each made tests fail: a gate paid twice, wages not rescheduled, `validateFinance` removed, the wrong club's wage bill.
- **CLI:** the money views (balance, the squad's wage and contract columns, ledger lines, bad usage), plus every earlier CLI test with the new world.

### Limitations

- **Contracts never expire:** expiry is only shown, and a player past it stays and is still paid. There are no renewals and no free agents. (Resolved in Milestone 13.)
- **Income is only gate receipts**, a flat amount per home match. There's no prize money, TV money, sponsorship or attendance model.
- **Nothing reacts to money:** no board, no budget and no debt consequences. The AI ignores finances.
- **No inbox messages for money.** The ledger grows by about 60 entries per club per year, and there's no compaction.

## Milestone 13: contract expiry, renewals and free agents (done)

Contracts now end. Every 1 July (the contract-year end), each contract ending then is renewed or expires. AI clubs keep their better players and let the weaker ones go, then refill their squads from the free agents. The manager renews players in their final year, signs free agents, and gets a safety net that keeps the squad legal.

```text
Contracts: 7 end on Wed 2026-07-01 00:00 UTC unless renewed (type contracts).
> renew 56
Callum Ibsen signed a new contract until 1 July 2029 at 2,640.00 a week.
  Wed 2026-07-01 00:00 UTC  contract: Oscar Bellamy left the club as a free agent
  Wed 2026-07-01 00:00 UTC  signing: Oscar Pereira joined until 1 July 2027 at 1,500.00 a week
> sign 105 1
Tomas Costa joined until 1 July 2027 at 1,160.00 a week.
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/content` | `Quota.Min` (GK 2, DF 5, MF 5, FW 3; `Count` is now also the maximum); `Economy.OfferCeilingPct` 200; `Economy.Demand`, `Economy.OfferCeiling`; `Definitions.Quota` | – (generated output unchanged) |
| `internal/employment` | A player without an assignment is a free agent. `Changes{Renewals, Departures, Signings}` → `Plan` → `Apply` (all-or-nothing, stale plans rejected) | – |
| `internal/ai` | `contracts.go`: `Renew` (keep if overall ≥ squad average − 3), `ContractLength` (seeded 1–4 years), `Signings` (round-robin allocation of free agents to needs) | `ai.ContractsVersion` 1 (new) |
| `internal/events` | `ContractRenewed` (7), `ContractExpired` (8), `PlayerSigned` (9) | – |
| `internal/inbox` | Messages `KindRenewed`, `KindPlayerLeft`, `KindPlayerJoined` for the managed team's players | – |
| `internal/app` | Task kind `taskContractYear` (5); `RenewContract`, `SignPlayer`, `SuggestContract`, `FreeAgents`, `ContractYearEnd`; `SquadPlayer.Demand`; `Summary.FreeAgents`; `validateContracts`; `Versions.Contracts`; `RenewCommands`, `SignCommands` in saves | save `SchemaVersion` 9 |
| `cmd/play` | `contracts`, `renew ID [YEARS [WAGE]]`, `free`, `sign ID [YEARS [WAGE]]`; the expiring count in `status`; `continue` stops the day before the contract-year end while players would leave; inbox lines for the new messages | – |
| `cmd/simulate` | The summary line shows the number of free agents | – |

**Goldens are unchanged.** The first contract year (2026-07-01) comes after the season-1 golden, and the world fingerprint doesn't depend on contracts after creation. Season 2 onward changes, which the new `ai.ContractsVersion` covers.

### Decisions

- **Contract years.** Every contract ends at a contract-year end: 00:00 UTC on the first day of the epoch's month (1 July).
  - A contract of N years signed during a contract year ends at the Nth contract-year end from then.
  - A renewal of N years moves the current end N years on.
  - Generated contracts already followed this rule.
- **Free agency is the absence of an assignment**, not a new record. The registry keeps every player: player IDs are never removed, reused or added in this milestone. Free agents keep their condition record and recover daily. They are paid nothing.
- **One yearly task, not one per contract** (kind 5, Expiries phase, no payload). It runs before anything else due at that instant and reschedules itself a year later. Because every contract ends at a contract-year end, one task sees them all, and a renewal never needs to cancel a task.
  - Invariant: exactly one task is queued, due at the next contract-year end.
- **The contract-year task is one all-or-nothing change.** It decides every ending contract, then runs the signings, then checks every squad against the minimums. Only then does it plan the whole employment change, queue next year's task and apply. It emits one event per change: renewals, then departures, then signings.
- **AI renewals:** keep a player whose overall is at least the squad average minus 3 (`ai.RenewalMargin`), measured before anyone leaves.
  - Terms: `ai.ContractLength` years, drawn from a stream keyed by (seed, player, the calendar year the contract starts), at the player's demand.
  - Being a pure function of those keys lets `SuggestContract` show the manager exactly what the AI would offer.
- **Signings** (`ai.Signings`), after departures:
  - The pool is every free agent. AI clubs want the full roster count per position; the user club only the roster minimum, as a safety net, and the manager signs the rest.
  - Picks go in rounds, weakest squad first (ties: club ID). Each pick is the best free agent (overall, then ID) for the club's largest need that can still be filled.
  - A club prefers players it did not just release. Without this, about a quarter of departures were re-signed by the same club ("left" then "joined"). It takes its own back only when nothing else fits, so a need is never left open while the pool can fill it.
- **Why the minimums always hold.** Each position has 8 × Count players in the world, and no club may hold more than Count. So the pool can always fill every club's needs, and a failure means broken state. `contractYear` still checks and fails cleanly (tested by forcing an impossible minimum).
- **Manager commands.** Both check the command ID and revision the usual way, need a user club, and apply two rules to the offer:
  - the length is within `ContractYears` (1–4);
  - the wage is from the player's demand (the no-variation wage for their overall) up to 200% of it. The ceiling keeps absurd wages, and wage-bill overflow, out.
  - **`RenewContract`:** only for your own players in their final year. It takes effect at once, so the next weekly run pays the new wage.
  - **`SignPlayer`:** only for free agents, only while the position is below its roster count, and not while rounds await results. Squads are frozen then, so pending selections and live replays can't change.
- **Validation** (`validateContracts`):
  - the minimums allow a legal lineup (at least one goalkeeper, and 11 players);
  - every senior squad is within [Min, Count] for each position;
  - players are employed only by their club's senior team;
  - every contract ends at a contract-year end in [next, next + 4 years];
  - the contract-year task is queued as described.
  - Squads matching the roster exactly and every player being employed are now checked only for a generated world (`checkGeneratedSquads`).
- **Restore** checks renewal and signing records against the rules (fresh ID, revision range, same player, a valid offer, a contract matching it that ends at a contract-year end, and a signing into the user team). It doesn't compare them with the current contract, because later changes may have replaced it.
- **The CLI** stops `continue` once, the day before the contract-year end, while players in their final year would leave. Nothing slips away unnoticed.

### Verification

- **`employment`:** a combined renewal, departure and signing batch (including re-signing a departed player) applies together and restores; 9 invalid change sets are rejected without change; a stale plan is rejected.
- **`ai`:**
  - the renewal threshold;
  - contract lengths are deterministic, cover 1–4 and depend on the start year;
  - signings: weakest club first, largest need, role order, best player, the preference for other clubs' releases, an exhausted pool, independence from input order, and 6 invalid inputs.
- **`events`, `inbox`:** new payload validation and clone cases. Player messages are kept only for the managed team, restore validates them, and an unmanaged inbox keeps none.
- **App:**
  - **The contract year:** every ending contract is renewed exactly when `ai.Renew` says so, on `aiOffer` terms. Every other contract is untouched. Events match the changes, AI clubs refill to 20, no free agents remain without a user club, and every player is employed or free exactly once.
  - **Several contract years**, with a save before the first: identical to never saving. The registry never changes (IDs never reused), and every season plays with legal lineups, including the user's 15-player squad.
  - **Manager decisions:** renewed players stay, unrenewed ones leave, and the safety net fills exactly to the minimum. The inbox reports each renewal, departure and signing.
  - **Paid from the next week:** a renewal at the ceiling changes the wage bill at once, and the next weekly run pays it.
  - **Suggested terms are the AI's decision:** in a world without a user club, club 3's renewals equal the suggestions exactly.
  - **Retries and saves:** retries (also after a round trip) return the recorded result and change nothing. A reused ID is rejected, and a player can't be signed twice.
  - **Rejections:** 9 renewal rejections and 3 signing rejections (employed player, full position, rounds pending), none of which change the world.
  - **A failed contract year** (impossible minimum) changes nothing, keeps the task queued and succeeds when retried.
  - **Invalid saves:** 11 contract states are rejected on load, and a different `ai.ContractsVersion` is `ErrIncompatibleSave`.
- **Deliberate-bug checks,** 7, each caught:
  - renewal years counted from the current year end;
  - the user filled to the full roster;
  - a renewal not applied;
  - retries not recognized;
  - the task's due time unchecked;
  - the AI renewing everyone;
  - the offer ceiling ignored.
- **CLI:**
  - the season-to-season flow with the new stop;
  - `contracts`, and `renew` with default terms;
  - 5 bad offers or IDs;
  - the inbox lines after the contract year;
  - `free`, and signing refused on a matchday then accepted after it.

### Limitations

- **Players never change:** no ages, development or retirement. The same 160 players circulate forever, and a player a club lets go stays equally good. (Resolved in Milestone 14.)
- **No transfers between clubs**, no fees and no loans. Players move only as free agents at the contract-year end, or when the manager signs one.
- **AI clubs ignore money** when renewing and signing, and wages don't depend on the club's finances or the player's form.
- **The manager can't release a player**, renew outside the final year or negotiate; the player accepts any offer within the rules.
- **AI clubs' squads are always full after the contract year,** so a free agent at rest is always one that the manager's club let go and the others didn't need.

## Milestone 14: player ages, development and retirement (done)

Players now have birth dates and a career. Every year, on the eve of the contract-year end (30 June), young players improve, players in their late twenties hold level and older ones decline. Some older players retire, and every club replaces each of its own retirees with a youth player at the same position. Clubs and the manager then make contract decisions on the new abilities.

```text
  Tue 2026-06-30 00:00 UTC  development: 8 of your players improved and 11 declined over the year (type squad)
  Wed 2027-06-30 00:00 UTC  retirement: Hugo Kowal retired at 36
  Wed 2027-06-30 00:00 UTC  youth: Yannick Okafor joined from the youth ranks until 1 July 2030 at 430.00 a week
  ID  POS NAME                     AGE   OVR    WAGE/WEEK     ENDS     ASKS FOR
  56  MF  Callum Ibsen              27    76     2,660.00     2026     2,570.00  <- final year
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/core/sim` | `Calendar.WholeYears(from, to)`: whole years as ages are counted (a 29 February anniversary completes on 1 March) | – |
| `internal/registry` | `Player.Born` (a `GameInstant`, the start of the birth day); `Init.LastPlayer`, the player ID allocator; `PlanPlayers` → `Apply` adds identities with IDs continuing the allocator in order | – |
| `internal/players` | `Profile.Retired`; `Changes{Developments, Retirements, Additions}` → `Plan` → `Apply`. `development.go`: `Growth`, `Develop`, `RetirementPermille`, `Retires` | `players.DevelopmentVersion` 1 (new) |
| `internal/medical` | `PlanRoster(admit, discharge)`: youth players join fully fit; retired players' records go | – |
| `internal/content` | `Ages` (generated players 17–34 at the career start); `Youth{Ages 16–17, RatingGap 13, ContractYears 3}`, `Youth.Range` | `content.Version` 4 |
| `internal/worldgen` | Birth dates, drawn last in each player's stream; `Youth(defs, seed, id, pos, at)` | `worldgen.Version` 3; `worldgen.YouthVersion` 1 (new) |
| `internal/ai` | `Renew(overall, average, age)`: a player under 24 gets 2 points more margin for each year short of it | `ai.ContractsVersion` 2 |
| `internal/events` | `PlayerRetired` (10), `YouthJoined` (11), `PlayersDeveloped` (12) | – |
| `internal/inbox` | `KindRetired`, `KindYouthJoined`, `KindDeveloped` (one summary: `Improved`, `Declined`) for the managed team | – |
| `internal/app` | Task kind `taskPlayerYear` (6) and `playerYear` (`lifecycle.go`); `validateLifecycle`; retired players are neither employed nor free agents; `SquadPlayer.Age`; `Summary.Players` counts active players, `Summary.Retired`; `Versions.Development`, `Versions.Youth` | save `SchemaVersion` 10 |
| `cmd/play` | An AGE column in `squad`, `contracts` and `free`; inbox lines for the new messages | – |
| `cmd/simulate` | AGE in the squad, `retired=N` in the summary; inbox lines for every player message (contract messages printed an empty line before) | – |

**Goldens.** The world fingerprint changed deliberately (`5682a824…`): the snapshot now has birth dates, and the version header changed. Names, attributes and contracts did not. The club and player streams are now keyed by `streamVersion` (2), the generator version that last changed their existing draws, and the birth date is appended to each player's stream. So seed 42 is the same world as before, and the season-1 golden (`7e0c0217…`) is unchanged. Season 2 onward changes, covered by the new versions.

### Decisions

- **Birth dates belong to the registry** (per the architecture's ownership table), as a `GameInstant`. Generation doesn't know the calendar, so worldgen draws a number of days before the career start. The day range `[366·min, 365·(max+1) − 1]` guarantees the age range whatever the epoch and its leap days.
- **Retirement belongs to `players`,** as a flag on the profile. A retired player keeps their identity and final profile forever. Their contract ends, their condition record goes, and they never become a free agent, play, recover or sign again.
- **The player ID allocator is explicit** (`registry.Init.LastPlayer`), not derived from the highest ID. New IDs must continue it in order, and it is saved and restored, so an ID is never issued twice even if players were ever removed.
- **The player year is its own yearly task,** one day before the contract-year end, in the Expiries phase. Kind 6, no payload; it reschedules itself.
  - It runs at a separate instant, so each yearly task stays independent and all-or-nothing, and no validation has to handle a state between two cohorts at one instant.
  - Clubs and the manager decide contracts on the new abilities. The CLI's existing contract stop (the day before the contract-year end) now falls right after the player year, so the manager sees the development summary, retirements and new asking wages before renewing.
- **One all-or-nothing change.** Each active player, in ID order, retires or develops. Then youth players are generated. Then registry, players, employment and medical each plan their part and next year's task is queued. Only then is everything applied. Events follow: one `PlayersDeveloped`, then one `PlayerRetired` per retiree, then one `YouthJoined` per youth.
- **Development** (`players.Develop`, keyed by seed, player and calendar year):
  - each attribute changes by `Growth(age)`: +4 to 18, +3 to 20, +2 to 22, +1 to 24, 0 to 28, −1 to 30, −2 to 32, −3 after that. Pace and stamina lose a further point a year from 29;
  - plus a form for the year of −2..2, shared by all of the player's attributes;
  - plus −1..1 per attribute;
  - clamped to 1..100.
  - Free agents develop too. A retiring player doesn't develop in their last year.
- **Retirement** (`players.Retires`, keyed the same way):
  - every player retires at 37;
  - a free agent retires from 31;
  - an employed player retires with 10%, 25%, 45% or 70% chance at 33–36.
  - So no active player is ever older than 37, which validation checks.
- **Youth replace retirees one for one.** Each club gets one youth player per retiree of its own, at the same position. Squads therefore keep their exact shape, and the contract year's guarantee (the pool can always fill every club's needs) still holds by the same argument as in Milestone 13. Retired free agents aren't replaced: they were surplus.
  - Youth players are 16–17. Their attributes come from the position ranges lowered by `RatingGap` (never below 1).
  - Their contract runs 3 contract years from the coming contract-year end, at their demand.
  - IDs are allocated club by club, retirees in ID order.
- **Calibration.** A youth `RatingGap` of 13 keeps the long-run average overall at the generated world's level. Over 30 simulated years for seeds 42 and 7:
  - the employed average stays 57–60, against 58–59 at the start;
  - about 10 players retire a year; the average age stays near 25;
  - wage bills stay in the same band.
  - A gap of 20 let the average sink to about 51 and cut wage bills by a quarter.
  - The generated world's attributes don't depend on age, so the first decade has some young stars who grow into the 90s. Later generations are shaped by the lifecycle.
- **AI renewals allow for youth** (`ai.ContractsVersion` 2). Without this, almost every youth player was released after their first contract, because they were still below the squad average.
- **Restored contract commands** are no longer compared with the player's current demand, because development changes it. Recorded offers are checked for an allowed length and a positive wage, and the contract must still match the offer.
- **Validation** (`validateLifecycle`, on `Validate` and load):
  - every player was born before now;
  - active players are at most 37 and have a condition record;
  - retired players have neither a condition record nor a club;
  - condition records exist exactly for active players;
  - exactly one player-year task is queued, due at the next eve.
  - Events: a retirement names a retired player (and the employer's senior team); a youth arrival names a senior team; developed players are registered.

### Verification

- **`sim`:** whole years at the boundaries: the same minute, one minute before, 29 February, negative spans, out of range.
- **`registry`:** new IDs must continue the allocator (reused, skipped, out-of-order, repeated IDs are rejected); stale plans; a restored registry never reissues an ID; birth instants out of range; players above the allocator.
- **`players`:**
  - plans develop, retire and add together, and survive a snapshot;
  - 11 invalid change sets are rejected without change, including developing a retired player and adding a retired one;
  - development is repeatable, depends on the year and seed, and stays within 1..100 for extremes over a century;
  - on average 17-year-olds gain about 4 points, 26-year-olds hold and 34-year-olds lose about 3, with pace falling faster than passing at 30;
  - retirement is certain at 37 and for free agents from 31, never below 33 with a club, and the 33–36 rates match within tolerance.
- **`medical`:** roster plans admit fully fit, discharge, and reject 5 invalid cases and stale plans.
- **`worldgen`:**
  - every generated player is 17–34 at the career start, for three epochs including 29 February and a minute before midnight, and the whole range occurs;
  - youth players are repeatable, keyed by ID, within their ages and ranges.
- **`events`, `inbox`:** payload validation (8 new cases) and clones; lifecycle messages only for the managed team, with the development summary counting only the team's players.
- **App (`lifecycle_test.go`):**
  - **The first player year applies the rules exactly:** every player retires exactly when `players.Retires` says so, unchanged; the others develop exactly by `players.Develop`. Each retiree is replaced at the same position, club by club, with the next IDs, a 3-year contract at demand, full condition and a youth age. Squads keep their shape, events match, and the next task is due on the next eve.
  - **Chunking and saves:** three years continued in one call, in uneven chunks, and with saves right before and after each player year give identical registries, profiles, employment and conditions.
  - **Retired players never return:** over three managed years (the manager renews no one, so free agents build up), retired players are never employed, free, recovering or scoring. No free agent of 31 or more survives a player year, and free agents do retire. Signing or suggesting terms for a retired player is `ErrNotFreeAgent`.
  - **Fifteen years,** AI-only and with a manager who never renews:
    - the world validates every year (squad limits included);
    - AI squads are always full, and the active population stays 150–160 plus free agents;
    - the average overall stays within 6 points of the start, and the average age stays 22–28.
  - **A failed player year** (no youth can be generated) changes nothing, keeps its task and, retried, equals a world where it never failed.
  - **The manager** sees ages in the squad, one development summary, and a retirement and youth message per retiree of their own.
  - **Invalid saves:** 10 lifecycle states are rejected, and a different development or youth version is `ErrIncompatibleSave`.
- **Existing tests adjusted to the lifecycle:**
  - contract-year tests take their "before" state after the player year;
  - the registry may only grow, with earlier identities unchanged;
  - profiles keep their positions;
  - there is one more queued task.
- **Deliberate-bug checks,** each caught:
  - youth at the wrong position;
  - retirees keeping condition records;
  - retired players counted as free agents;
  - free agents never forced to retire (caught after adding the free-agent check above);
  - youth contracts one year short;
  - `validateLifecycle` removed;
  - lifecycle event checks removed.
- **CLI:**
  - the contract flow with the new ages, abilities and demands;
  - the development summary, a retirement and its youth replacement in `cmd/play`;
  - the retired count in `cmd/simulate`.

### Limitations

- **No potential.** Every player follows the same age curve plus noise. Youth quality depends only on the intake draw, and there are no late bloomers beyond their yearly form.
- **The generated world ignores age,** so early seasons have unusually strong youngsters and old players who decline from a high level.
- **Development is yearly and ignores playing time,** training and condition. The architecture's monthly rule and exposure-driven growth are later work.
- **Youth intake only replaces retirees.** A club never gets extra youth, and there are no youth or reserve teams: youth players join the senior squad at once.
- **Retired players stay in the registry forever** (about 10 a year), with their profiles, which grows the save slowly.
- **AI signings still take the best overall regardless of age.**

## Web client: `cmd/web` (done)

The career can now be played in the browser. `go run ./cmd/web` serves a local web app at `http://127.0.0.1:8080`. It takes the same flags as `cmd/play` (`-seed`, `-club`, `-load`), plus `-addr` and `-save`. Without `-club`, the first page lets you choose a club.

Pages: **Home** (the season, the next match or waiting matchday, the latest result with scorers, and recent inbox messages), **Squad** (ages, ratings, attributes, contracts, and renewal forms for final-year players), **Lineup** (each player's selection and the mentality), **Table**, **Fixtures**, **Free agents** (signing forms), **Inbox** and **Finances**. The header always offers Continue (or Play match) and Save.

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | New. `main.go` (flags), `server.go` (routes, guards, commands), `views.go` (view models), `templates/*.html` (embedded; no JavaScript) |
| `internal/app` | `Content()`: a copy of the career's pinned content definitions (roster quotas and contract lengths for the forms) |
| `internal/app/boundaries_test.go` | `cmd/web` may import app, storage and the contract types the queries return, like `cmd/play` |

### Decisions

- **A presentation adapter only.** Every page is built from app queries, and every change is an app command (`ResolveRounds`, `SubmitLineup`, `RenewContract`, `SignPlayer`) or `storage.Save`. The web client owns no football rules. The progression policy (continue to the club's next matchday, resolving other batches on the way; stop once on the day before the contract-year end while contracts would lapse) is the same as `cmd/play`'s, reimplemented in the client.
- **Standard library only:** `net/http` with method-and-path patterns, `html/template` (auto-escaping), and `embed`. Pages render on the server and changes are plain HTML forms, so no JavaScript is needed.
- **One career, one writer.** The server holds one world and handles one request at a time under a mutex. It listens on localhost by default.
- **Changes are POST forms, then a redirect** (post/redirect/get), so reloading a page never repeats a change. Notes about the outcome ("Lineup saved", errors) are shown once on the next page.
- **Stale pages are refused.** Every career form carries the revision the page was rendered at, and a form from another revision changes nothing. This covers a second tab, the back button, or a double submit, the same way `ExpectedRevision` protects commands.
- **Cross-site requests are refused** (`Sec-Fetch-Site: cross-site`, or an `Origin` of another host). This matters because the server changes a game on the user's machine.
- **The lineup form submits the whole lineup** as one `SubmitLineup`, instead of the terminal's local draft edited step by step. Starters are ordered goalkeeper, defenders, midfielders, forwards. The form starts from the submitted lineup, or else the AI's suggestion.

### Verification (`cmd/web/main_test.go`, over HTTP with `httptest`)

- choosing a club (and rejecting an unknown one, or a second career);
- a matchday: continue, the lineup form (20 players), submitting an attacking lineup, playing it, and the result on Home, Fixtures and the Table;
- 5 lineup rejections that submit nothing (10 starters, two goalkeepers, an unknown mentality, the wrong fixture, a bench of 9);
- stale forms (old or malformed revisions) and cross-site requests change nothing; GET on an action is 405, and an unknown path is 404;
- the contract flow of `cmd/play`'s test: the contract stop, 7 renewal forms, a renewal at the suggested terms, 3 bad offers, the contract year, a signing refused on a matchday and accepted after it, and signing the same player twice refused;
- saving, loading the save (seed and revision kept), and refusing a save without a managed club;
- every page renders at the start, on a matchday, after it, in the off-season and in two new seasons (with retirements, youth and development in the inbox);
- flags: the address and seed, `-load` with `-seed`, stray arguments, a missing file, a failed seed draw.

### Limitations

- **No live matches in the browser.** A match is played in one step. A live match started in `cmd/play` and saved is finished by Play match.
- **One career per server, one player.** No sessions or accounts; the server is meant for localhost.
- **The progression policy is duplicated** in `cmd/play` and `cmd/web`. If a third client appears, it belongs in an app query ("next stop").
- **Pages refresh only on navigation.** Nothing updates on its own.

## Web client update: other team squads and game reports (done)

The web frontend now allows viewing the squads of any team and viewing full game reports by clicking on match scores.

### Changes

- **View squads of other teams:**
  - `GET /squad?club=ID` displays the complete squad of any club (names, age, ratings, condition, attributes, contract expiry, and weekly wage). Contract renewal forms are only displayed for the user's managed club.
  - A club switcher bar on the Squad page allows navigating between all clubs in the league.
  - Club names in the Table (`/table`) and opponent names in Fixtures (`/fixtures`) link directly to that club's squad.
- **Game reports when clicking on scores:**
  - `GET /report?fixture=ID` displays the detailed game report for any completed fixture: competition, round, kickoff time, teams, score, manager vs AI selection status, and goal timeline with minute and scorer name.
  - In Fixtures (`/fixtures`), all played scores link directly to their game report.
  - On Home (`/`), the Latest Result score and all Other Results link directly to their respective game reports.
  - Navigation links on the game report allow jumping directly to both teams' squads, the league table, or back to fixtures.
- **App queries:**
  - `w.MatchReport(fixture)` returns the recorded match report for any resolved fixture.
  - `w.PlayerName(id)` retrieves any registered player's full name.
  - `w.ClubLabel(club)` retrieves a club's senior team label.

## Milestone 15: a second league and the Continental Cup (done)

The world now has two nations of eight clubs, each with its own league (the Founders League, as before, and the new Harbour League), played on the same calendar. When both league seasons end, the top four of each meet in the Continental Cup: single-match quarter-finals, semi-finals and a final, a week apart, starting two weeks after the leagues' last round. A match level after 90 minutes is decided by a penalty shootout.

```text
Continental Cup quarter-final  Sat 2025-11-22 15:00 UTC  (competition 3)
  F225 GRY 2-4 GLE  Greyfen United 2-4 Glenrock Town
  F226 FOX 1-4 BRK  Foxmere Town 1-4 Brackenmoor Town
  F227 ASH 0-1 DUN  Ashcombe City 0-1 Dunmarrow Albion
  F228 ELD 1-2 LAR  Eldhaven United 1-2 Larkspur Rovers
Continental Cup semi-final  Sat 2025-11-29 15:00 UTC  (competition 3)
  F229 GLE 0-0 BRK  Glenrock Town 0-0 Brackenmoor Town (3-1 on penalties)
  F230 DUN 1-0 LAR  Dunmarrow Albion 1-0 Larkspur Rovers
Continental Cup 1 winner: Glenrock Town (GLE)
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/matches` | `Rules.Knockout`; `ResolutionPenalties` (3); `MatchOutcome.Shootout` and `MatchView.Shootout`; `MatchOutcome.Winner` | – |
| `internal/matches/simple` | A penalty shootout after a level knockout match; shootout params; `Capabilities.Penalties` | – (league matches unchanged; see Decisions) |
| `internal/competitions` | `Format` (league 1, knockout 2) per season; `CreateSeasons` for both formats; knockout brackets (`knockout.go`); penalties in `Score`, `Result` (`Winner`) and snapshots; `Ranking`, `Champion`, `Format` queries; knockout rounds need their fixtures to kick off | – |
| `internal/content` | `Nation{Name, Clubs, Towns}` replaces `ClubCount` and `Towns`; Eastmarch's twelve towns; `DefaultLeagues` (adds the Harbour League, ID 2); `Cup`, `Qualifier`, `Cup.Seeding`, `DefaultCups` (Continental Cup, ID 3) | `content.Version` 5, `content.LeagueVersion` 3 |
| `internal/worldgen` | Clubs are generated nation by nation, each nation from its own stream (the first nation keeps the earlier stream) | `worldgen.Version` 4 |
| `internal/events` | `MatchCompleted.HomePenalties`/`AwayPenalties` (omitted when zero); `SeasonEnded` and `SeasonStarted` also cover cup editions | – |
| `internal/inbox` | `Message.Shootout` (for, against) | – |
| `internal/app` | Pinned cup definitions (`WorldSnapshot.Cups`, in the content fingerprint); cup editions drawn in `endSeasons`; cup match rules (knockout); shootouts through resolution, reports, events and restore; `validateSeasons` covers cups; views `Cups`, `Cup`, `FixtureInfo`, `RoundName`; `InboxItem.Cup`, `RoundName`, `Stage`; `History` includes cup editions | save `SchemaVersion` 11 |
| `cmd/play` | `cup` shows the bracket; cup matchdays and results named after their round, with penalties; the next fixture may be a cup tie; `season` also runs the season end; the table shows last season's in the off-season | – |
| `cmd/simulate` | `-season` also plays the cup edition that follows and names its winner | – |
| `cmd/web` | A Cup page (bracket); cup ties on Home and Fixtures; both league tables; penalties in reports, results and the inbox; fixtures for a club of either league | – |

**Goldens.**
- The world fingerprint changed deliberately (`cbc055bb…`): a second nation, and new versions.
- The first nation's clubs, players and contracts are unchanged, and so is the Founders League's season 1: its golden (`7e0c0217…`) now covers that league alone, and still matches.
- New goldens pin the Harbour League's season 1 (`99110526…`) and the first cup edition (`fbb6d1a2…`).

### Decisions

- **Two parallel leagues, not divisions.** Each nation's clubs form one league. There is no promotion or relegation; the cup is where they meet.
- **The first nation is unchanged.** Each nation draws its towns from its own stream; the first uses the stream of earlier generator versions, and player streams are keyed by player ID, so Westmark's clubs and players are exactly those of a one-nation world (tested). League 1's fixtures keep IDs 1–56 (fixture IDs are allocated in competition order), so its first season plays exactly as before.
- **A knockout season is a competitions format, not a new module.** Competitions already owns seasons, fixtures, rounds and official results. A knockout season has a fixed bracket (its entrants, in order); each round's fixtures are created when the previous round's results are recorded, inside the same all-or-nothing `CompleteRounds`, from its winners in bracket order. Fixture IDs follow everything allocated before, so they are never reused. Validation replays the bracket from the results.
- **Seeding is the cup's rule, in content** (`Cup.Seeding`): the qualifiers are interleaved by position (both champions, both runners-up, and so on) and placed in the standard bracket. With two leagues of four: A1 v B4, B2 v A3, B1 v A4, A2 v B3. The two champions can meet only in the final. The first team of each tie is at home: the better seed in the quarter-finals, then the winner of the upper tie.
- **Cup editions follow the leagues.** Edition N is drawn in the season-end cohort that moves the last qualifying league past season N (with the default calendar, both end together), from their final rankings, in the same `CreateSeasons` call as the next league seasons. Its first round is `FirstRoundDelay` (two weeks) after the latest qualifying season's last kickoff. It is caused by the cohort's last season-end task. An edition's own season end closes it without creating anything.
  - Nothing new is stored for this: the latest edition is derived from the competitions store, and validation checks that edition N exists exactly when every qualifying league's current season is past N, with the bracket and timing `cupEdition` would build.
- **Penalties belong to the match engine.** A tie needs a winner, and deciding results is the engine's job, so the contract's reserved penalties resolution is now implemented. `Rules.Knockout` asks for a shootout when the match is level after 90 minutes. The simple engine takes it after regulation, from the same stream: five kicks each, alternating, ending early once one side cannot be caught, then sudden death. The takers are the players on the pitch, best finishing first. A kick scores with 76% for equal taker and keeper, scaled by finishing against goalkeeping and clamped to 50–93%. Level after 50 sudden-death pairs (practically impossible), one even draw decides.
  - **`simple.ModelVersion` stays 3:** a league match (no knockout rule) produces exactly the same outcome as before, and the shootout's draws come after all 90 minutes, so even a knockout's regulation play is unchanged (tested). The knockout rule is new input, not changed behavior. The app refuses knockout matches with an engine that lacks the Penalties capability.
- **Official results carry the shootout** (`HomePenalties`, `AwayPenalties`): a league result never has one; a knockout result has one exactly when the goals are level, and it must decide the tie. Standings ignore it (knockouts have no table). `Ranking` orders a knockout by the round each team reached (the champion, the runner-up, the semi-final losers, then the quarter-final losers, each in bracket order), and it is what `SeasonEnded` carries for a cup.
- **Gate receipts** are paid for cup matches too, to the home club.
- **Clients** name rounds from `RoundName` ("round 3", "quarter-final", "semi-final", "final") and describe any fixture with `FixtureInfo`, since a cup tie is in no league schedule. The inbox says how far the managed club went ("you went out in the semi-final", "your club won it!").
- **"Play the season" also runs the season end** (in `cmd/play` and `cmd/web`), so the next season and the cup draw are visible at once. In the off-season the table shows last season's final table.

### Verification

- **Engine:** in 400 fixtures, the knockout rule never changes regulation play; every knockout match has a winner, and every shootout is well formed (best of five, or sudden death won by exactly one) and repeatable. Better takers against a weak goalkeeper win at least 65% of shootouts. `Winner`; 4 invalid shootout params.
- **Competitions:**
  - an eight-team bracket through to the champion, with penalty winners progressing, fixture IDs continuing the allocator, and a round without fixtures refusing to kick off;
  - the ranking (champion, runner-up, semi-final and quarter-final losers, each in bracket order);
  - 4 invalid knockout results and a league result with penalties are rejected without change;
  - 5 invalid brackets and a season without a format are rejected atomically;
  - mixed-format creation allocates IDs in competition order;
  - the bracket round-trips a snapshot, and 10 invalid knockout snapshots are rejected (wrong or early fixtures, missing rounds, level results, penalties after a win, a league with penalties).
- **Content and worldgen:** nations validated (unique town names and codes across nations); cup validation (9 cases) and seeding (8 and 4 teams); the first nation is identical to a one-nation world.
- **App (`cup_test.go`):**
  - the first edition is drawn when both leagues end, with exactly the promised bracket, home teams and timing, and a matching `SeasonStarted`;
  - over four seeds, every tie has a winner, winners meet in the next round, the champion won the final, reports and events agree with the official shootouts, the ranking ends the edition, and at least one tie went to penalties;
  - only cup fixtures reach the engine as knockouts;
  - saving before every cup round and while each awaits results continues exactly like never saving;
  - a managed club in the cup gets named matchdays, results with penalties and the stage it reached, and can submit cup lineups;
  - 7 invalid cup saves are rejected (edited or removed definitions, a clashing ID, a reseeded bracket, an edited shootout, a missing season-end task).
- **Existing tests** now cover two leagues: 16 clubs, both leagues' batches together (8 matches, 2 rounds), 28 kickoff tasks, doubled events, cup gate receipts, and history with cup editions. `playSeason` plays a season and its cup (`playLeagues` and `playCup` separately).
- **Deliberate-bug checks,** each caught:
  - cups not flagged as knockouts;
  - the cup bracket check removed from validation.
- **CLI and web:**
  - `cup` before and after the draw;
  - a managed cup run won on penalties (the outcome shows W, a real bug fixed along the way);
  - `-season` plays the cup and prints its winner;
  - the browser cup run: the cup page, the cup tie as the next match and the matchday, a penalty win, the bracket, fixtures, both tables and the winner's inbox message.

### Limitations

- **The Continental Cup is the only cup,** single matches only: no two-legged ties, no extra time and no away goals.
- **Leagues are parallel:** no promotion or relegation, and every league has eight clubs (`competitions.SupportedEntrants`).
- **No prize money** for the cup; only gate receipts.
- **Players move between nations freely** as free agents; nations have no other meaning yet (no registration rules, no national identity for players).
- **A change to `Cup.Seeding`** would alter future editions of existing saves without a must-match version; it is covered only by `content.LeagueVersion`, which saves record for provenance.

## Milestone 16: transfers between clubs with fees (done)

Every year the transfer window opens at the contract-year end (1 July) and closes four weeks later. The manager bids a fixed fee for another club's player; the club answers the next day, and an accepted bid completes at once. A club that sold then fills its vacancy: it bids for the best player it can afford who is clearly better than the best free agent, or signs that free agent. So one sale sets off a short chain of transfers. AI clubs also bid for the manager's players, and the manager accepts or rejects within three days.

```text
Wed 2026-07-01 00:00 UTC: The transfer window is open until Wed 2026-07-29 00:00 UTC; clubs answer bids made before Tue 2026-07-28 00:00 UTC.
> bid 37
You bid 240,000.00 for Jonas Gallo (offer 4), offering 1 year at 2,430.00 a week. The club answers on Thu 2026-07-02 00:00 UTC.
  Thu 2026-07-02 00:00 UTC  transfer: Jonas Gallo joined from Greyfen United for 240,000.00, until 1 July 2027 at 2,430.00 a week
  Thu 2026-07-02 00:00 UTC  transfer: your bid of 500,000.00 for Oscar Adeyemi of Ironbridge Wanderers was rejected
  Sat 2026-07-04 00:00 UTC  bid: Ironbridge Wanderers bid 1,200,000.00 for Elias Gallo (offer 14); answer before Tue 2026-07-07 00:00 UTC (accept/reject)
> accept 14
Accepted: the transfer is complete.
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/core/ids` | `OfferID` | – |
| `internal/transfers` | New module. `Offer{ID, Player, Seller, Buyer, Fee, Terms, MadeAt, Deadline, Status, ClosedAt}`; statuses open 1, completed 2, rejected 3, expired 4, collapsed 5; `Changes{Close, Bids}` → `Plan` → `Apply`; `Snapshot{Offers, LastOffer}` | – |
| `internal/ai` | `transfers.go`: `Valuation`, `AcceptBid`, `TransferBudget`, `LargestNeed`, `ChooseTarget` | `ai.TransfersVersion` 1 (new) |
| `internal/finance` | `KindTransfer` (4): non-zero, names its offer (`Entry.Offer`, `Posting.Offer`) | – |
| `internal/content` | `Transfers{WindowDays 28, ResponseDays 3}` | – (generated output unchanged) |
| `internal/events` | `Deal`; `TransferOffered` (13), `TransferCompleted` (14), `OfferClosed` (15); `LedgerEntry.Offer` | – |
| `internal/inbox` | `KindBidReceived`, `KindTransferIn`, `KindTransferOut`, `KindOfferClosed`; `Message.Offer`, `Club`, `Fee`, `Deadline`, `Outcome`, `Selling` | – |
| `internal/app` | Task kind `taskTransferRun` (7) and `transferRun` (`transfers.go`); `MakeTransferOffer`, `RespondToOffer`; `TransferWindow`, `Offers`; `SquadPlayer.Value` (the asking price); `SuggestContract` for other clubs' players; `InboxItem.ClubName`; `validateTransfers`; `Versions.Transfers`; `WorldSnapshot.Transfers`, `OfferCommands`, `ResponseCommands` | save `SchemaVersion` 12 |
| `cmd/play` | `transfers`, `market POS`, `bid ID [FEE [YEARS [WAGE]]]`, `accept OFFER`, `reject OFFER`; the window in `status`; `continue` stops when the window opens and then goes one day at a time to the next transfer news; offers in `finances` | – |
| `cmd/web` | A Transfers page (bids to answer, your bids, a market by position with bid forms, this window's transfers); the window on Home; asking prices on other clubs' squads; `Continue` steps through the window like the terminal | – |

**Goldens are unchanged.** Worlds without a manager never make an offer (see Decisions), so the world fingerprint, the season goldens and every AI-only season are as before.

### Decisions

- **A new module for the workflow.** `transfers` owns offers and where each stands: open, then completed, rejected, expired or collapsed. Employment still owns who employs the player, and finance owns the money. Only `app` completes an accepted offer, planning all three modules and applying them together.
- **The window** opens at each contract-year end and lasts `WindowDays`: [open, close), half-open as the architecture asks. The career start is a contract-year end, so the first window is open from day one.
  - **Transfer runs** happen at 00:00 on each day after the opening, up to and including the close. Each is task kind 7 in the Decisions phase, with no payload, and reschedules itself: exactly one is always queued, due at the next run.
  - **A bid is answered at the first run after it.** So bids close one day before the window (`BidsClose`); a bid that could only be answered at the close is refused.
  - **The manager** answers a bid within `ResponseDays` (3), never beyond the close. The explicit default is expiry: an unanswered bid expires at its deadline, so nothing blocks the game.
  - The window ends before the next player year, so no player retires or develops while an offer is open.
- **Fixed-price bids.** An AI club accepts a fee at or above its valuation (`ai.AcceptBid`). That valuation is the asking price clients show, and the fee AI clubs bid. Because age only rises during a window, a bid at the asking price stays acceptable.
- **Valuation** (`ai.Valuation`): 600,000 × (overall / 60)³, then:
  - by age: ×1.2 to 21, ×1 to 27, ×0.8 to 29, ×0.55 to 31, ×0.35 to 33, ×0.2 after;
  - by contract years left: ×0.6 for one, ×0.85 for two, ×1 for three or more;
  - rounded to 10,000, integer arithmetic throughout.

  A 60 in his prime costs 600,000, a 75 about 1.2 million and a 90 about 2 million, against balances of about 2 million.
- **Completion is one unit of work, revalidated.** When a bid is accepted, `complete` checks the rules again:
  - the window is open;
  - the seller still employs the player, and he has not moved in this window;
  - the seller keeps at least its minimum at his position;
  - the buyer has room there;
  - the buyer's balance covers the fee.

  If a rule fails, the offer collapses and nothing moves. Otherwise, in one commit:
  - employment ends the old contract (a departure) and starts the new one (a signing on the offered terms: that many contract years, the current one included, like `SignPlayer`);
  - finance posts the fee as a pair of `KindTransfer` entries, −fee to the buyer and +fee to the seller;
  - every other open offer for the player collapses;
  - `TransferCompleted` and one `LedgerPosted` are emitted.
- **The manager's bids** are refused up front for:
  - no open window, or no run left before the close;
  - a player who is at no other club or has moved in this window;
  - a second bid for the same player in the window;
  - no room at his position;
  - a selling club at its minimum there;
  - a fee above the balance;
  - terms outside the contract rules;
  - squads locked by a pending matchday.

  One bid per player per window means the valuation can't be found by bidding repeatedly; the asking price is shown instead.
- **Answering a bid** (`RespondToOffer`) must come before its deadline. Accepting completes the transfer at once. Two failures are handled differently:
  - an acceptance that would take the manager's squad below its minimum is refused, and nothing changes;
  - if the buyer can no longer pay or make room, the offer collapses.
- **AI clubs buy only to fill vacancies** (positions below the roster count). Each run goes in club ID order: an AI club with a vacancy and no open bid acts once for its largest need.
  - It bids its valuation for the best player it can afford whose overall is at least `TransferMargin` (5) above the best free agent at the position. Its budget is the balance minus 26 weeks of wages (`ai.ChooseTarget`, `ai.TransferBudget`).
  - Otherwise, or once it has bought in this window, or on the last day, it signs that free agent.
  - At the close, AI clubs fill any vacancy left from the free agents, as at the contract year (`ai.Signings`).
  - It bids only for players the rules let it buy, and never twice for the same player in a window. An AI club bid for the manager's player waits for the manager's answer.
- **Why AI clubs don't bid for upgrades.** Squads hold at most the roster count per position, and AI squads are full after the contract year. So an AI club can only buy with a vacancy, and only a sale creates one; in a world without a manager, no offer is ever made. Letting AI clubs buy when full would require releasing a player. A released older free agent would retire at the next player year without a youth replacement, so the population could shrink year after year. That needs its own rule (see the next task).
- **One purchase per AI club per window.** Without it, one sale by an AI club rippled through the league in a chain of 22 transfers, each seller replacing its loss with the next club's player. With the rule, a sale causes a chain of about 8 transfers, which ends with a free-agent signing.
- **Offers are kept for the whole career.** Completed ones justify their ledger entries, which validation checks forever. Rejected, expired and collapsed ones cost little, perhaps a few dozen a year.
- **Validation** (`validateTransfers`, on `Validate` and load):
  - every offer names registered clubs and a player, and was made inside a window, before now, with a deadline no later than that window's close;
  - an open offer:
    - belongs to the window under way and is due after now;
    - its seller still employs the player;
    - its deadline is the next run for an AI seller, or `ResponseDays` later (at most the close) for the manager;
    - a buyer has at most one open offer per player;
  - a closed offer closed by its deadline, and an expired one exactly at it;
  - a player completes at most one transfer per window;
  - every transfer fee belongs to a completed offer, and every completed offer moved its fee exactly once, from the buyer to the seller when it completed;
  - exactly one run task is queued;
  - events match their offers, and restored commands match the offers they made or answered.
- **Clients.** `continue` (and the web's Continue) stops when the window opens, then goes one run at a time and stops at the first transfer news for the manager. So an answer or an AI bid is never passed over.

### Verification

- **`transfers`:** offers open, close and survive a snapshot, and the allocator continues after a restore. 14 invalid change sets are rejected without change, stale plans are rejected, and 10 invalid snapshots are rejected.
- **`ai`:** valuation rises with overall and falls with age and contract length, is a positive whole step, and a 90 is worth 3–4 times a 60. Also tested: the acceptance rule, the budget including overflow, the largest need, and target choice (the best, then the cheapest, independent of order, with an inclusive margin).
- **`finance`, `content`, `events`, `inbox`:**
  - transfer fees move between ledgers, and 4 invalid postings are rejected;
  - 4 invalid windows are rejected;
  - 12 invalid transfer events are rejected, and clones share nothing;
  - bids received, transfers in and out, and closed offers become messages, the team's own bids and other clubs' business don't, and 3 invalid messages are rejected.
- **App (`transfers_test.go`):**
  - **The window:** it is open from the career start; runs are daily and the last is at the close; a bid on the last day and after the close is refused without change; the next window follows; a world without a manager makes no offer.
  - **A purchase:** the bid is answered at the next run. The player joins on the offered contract, the fee moves from buyer to seller and the total of all balances is unchanged. Both squads are within limits, and the events (a command-caused offer, then a task-caused completion and ledger) and the inbox match.
  - **A bid below the valuation** is rejected; nothing moves, and a second bid is `ErrAlreadyBid`.
  - **15 refused bids** change nothing (compared snapshot by snapshot): zero ID, stale revision, no manager, own player, free agent, unknown player, full squad, seller at its minimum, no fee, a fee above the balance, a wage below demand, too many years, a second bid, a player who moved in this window, and a pending matchday.
  - **AI bids for the manager's players** (a scenario found by simulation) come at the valuation with a three-day deadline:
    - accepting completes the transfer, pays the manager and is retried safely;
    - declining rejects it;
    - an unanswered bid expires exactly at its deadline;
    - answering an unknown or another club's offer, or accepting at the minimum, is refused without change.
  - **Competing offers collapse** when the manager accepts one of two, and when two bids for one player are answered at the same run.
  - **Saves and retries:** saving and loading before every day of a window, and retrying the bid each time, ends exactly like never saving; nothing completes twice.
  - **A whole window with four purchases:** every day the fees sum to zero, the world validates and every player is at most at one club. Each AI club buys at most once, and every AI squad is full after the close.
  - **A failing run** (a fee that would overflow the seller's balance) changes no offer, contract, ledger, event or inbox message, and stays queued.
  - **Invalid saves:** 13 are rejected:
    - an edited fee, or a completed offer without a fee;
    - an offer open past its deadline, or one expired early;
    - a fee for no offer, or the allocator behind;
    - a missing or late run task;
    - mismatched bid and answer records (3 cases);
    - an edited event, and an invalid window.

    A different `ai.TransfersVersion` is `ErrIncompatibleSave`.
- **Deliberate-bug checks,** each caught:
  - competing offers not collapsing;
  - `validateTransfers` removed;
  - the seller not paid;
  - AI accepting any fee;
  - retried answers re-applied;
  - no one-purchase rule;
  - the seller's minimum ignored;
  - AI bidding for players who already moved.

  Two real bugs were found along the way: `LedgerPosted` did not carry the offer (and validation did not compare it), and the first AI rule let one sale ripple through 22 transfers.
- **CLI and web:**
  - the season-to-season flow with the new stop at the window;
  - `market` and the Transfers page;
  - bids at and below the asking price;
  - a second bid refused;
  - the answers arriving with `continue`;
  - an AI bid received, accepted after loading a save and rejected in another run;
  - the fee in the ledger;
  - asking prices on other clubs' squads.

### Limitations

- **AI clubs never start a transfer:** they buy only to fill a vacancy, so the market moves only when the manager buys (or an AI club is short after the contract year). Nobody can release a player.
- **At the career start every squad is full,** so the first window is useful only after the manager has room; in practice, from the second summer on, once contracts end.
- **One window a year,** fixed prices and no negotiation. There are no counter-offers, loans, installments, sell-on clauses, or player refusals (a player accepts any terms within the contract rules), and no reserved funds: affordability is checked at bid and at completion.
- **AI valuations ignore form and the club's needs,** and AI clubs don't sell to raise money or refuse to sell their best player.
- **The window's free-agent signings** don't prefer players the club did not release, unlike the contract year.

## ui: sortable lists in web and CLI (done)

All lists and tables in the application are now sortable by column in both the web interface (`cmd/web`) and the interactive CLI (`cmd/play`).

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | Added `sort.go` with `SortState`, `sortHeader` template helper, and stable multi-column sorting functions for squad, table, fixtures, finances, transfers (market, received, mine, done), free agents, choose club, lineup, cup, and inbox. Updated all list templates with clickable column header links (`?sort=...&dir=...`), CSS indicator arrows, and progressive enhancement JS. |
| `cmd/play` | Added optional sorting arguments to `squad`, `table`, `fixtures`, `finances`, `contracts`, `free`, `market`, `transfers`, `inbox`, and `cup`. |
| `cmd/web/main_test.go` | Added `TestSortableLists` testing sorting URL parameters and output ordering across all views. |
| `cmd/play/main_test.go` | Added `TestSortableLists` testing CLI sort commands and error messages for invalid sort arguments. |

### Decisions

- **No-JS first with progressive enhancement:** In `cmd/web`, all column headers are standard HTML anchor links carrying `?sort=<col>&dir=<asc|desc>`. Sorting is fully server-side rendered. For users with JavaScript enabled, a lightweight client-side table sorter intercepts clicks to sort instantly and update the URL via `history.replaceState`.
- **Both clients rule:** Every list that can be sorted in `cmd/web` can also be sorted in `cmd/play` via optional arguments (e.g. `squad ovr desc`, `table club asc`, `fixtures opp`, `market fw price desc`).
- **Stable sorting:** Slices are sorted with `slices.SortStableFunc` and deterministic tie-breakers (e.g., ID or round) to maintain consistent order across repeated sorts.
- **Strict backward compatibility:** In `cmd/play`, omitting sort arguments retains previous default ordering and output verbatim, and existing command syntax is preserved without regressions.

## squad: squad sizes, free transfers and releases (done)

Squads are no longer fixed at 20. The manager can hold up to 25 players, at any positions, as long as each position keeps its minimum. So the manager can sign or buy anyone while the squad has room, not only to fill a vacancy. The manager can also release a player: the club pays the rest of his contract, and he becomes a free agent.

```text
> release 60
Releasing Rafael Okafor costs 179,400.00: his wages until his contract ends on 1 July 2028. He becomes a free agent.
Type release 60 yes to release him.
> release 60 yes
Rafael Okafor was released and is now a free agent. You paid 179,400.00.
> release 42 yes
! app: the squad would fall below its minimum at that position: 2 GK, minimum 2
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/content` | `Definitions.SquadLimit` (25, at least `SquadSize`). `Quota.Count` is the generated size and what AI clubs keep, no longer a maximum | – (generated output unchanged) |
| `internal/finance` | `KindPayoff` (5): negative, names `Entry.Player` / `Posting.Player` | – |
| `internal/events` | `PlayerReleased` (16); `LedgerEntry.Player` | – |
| `internal/inbox` | `KindReleased` (15); `Message.Compensation` | – |
| `internal/app` | `ReleasePlayer` command, `PlayerReleased` result, `ReleaseRecord`; `SquadPlayer.Payoff`; `hasRoom`; the squad limit in `SignPlayer`, `MakeTransferOffer` and completions; the player year fills AI vacancies with youth; `validateReleases`; `WorldSnapshot.ReleaseCommands` | save `SchemaVersion` 13 |
| `cmd/play` | `release ID [yes]`; the squad limit and minimums under `squad`; the release inbox line; `P<player>` on payoff ledger lines | – |

**Goldens are unchanged**, including the world fingerprint and every season golden: no existing seeded run reaches a state the new rules treat differently (see Decisions).

### Decisions

- **One limit for the whole squad, minimums per position.** Per-position maximums made squads exactly 20 and blocked any purchase without a vacancy. Now the user club may take a player at any position while its squad is below `SquadLimit`. The minimums stay, because they guarantee a legal lineup. 25 is the usual size of a registered squad.
- **AI clubs keep their shape.** An AI club still signs and buys only to fill a vacancy (a position below its roster count) (`hasRoom`), so AI behaviour and every AI-only world are unchanged.
- **Releasing** (`ReleasePlayer`):
  - **The payoff** is the rest of the contract: the weekly wage for every wage run after now and before the expiry. The contract year runs before the wages at the expiry instant, so a run at the expiry would not have paid him. A contract with no run left costs nothing and posts no entry.
  - **Refused** for a player of another club (or none), below the position's minimum, a payoff above the balance (as with transfer fees), and while a matchday is pending (squads are locked).
  - **One unit of work, through the market commit:** employment (a departure), finance (one `KindPayoff` entry naming the player), and transfers (every open bid for him collapses). Events: `PlayerReleased`, then the closed offers, then `LedgerPosted`.
  - **The money leaves the game.** Players have no ledger, so a payoff is not a pair of entries. It is justified by its release record. `validateReleases` matches every payoff entry to exactly one recorded release (same player and amount, the user club) and every paying release to exactly one entry. The command log is kept forever, unlike the journal.
- **Keeping squads fillable.** Milestone 13 proved the contract year can always refill every club because no club held more than its roster count. With the manager able to hold extra players, sellers may be left with vacancies that the free agents can't fill, and the contract year could then fail.
  - The rule chosen is that the player year also gives every AI club a youth player for each vacancy, after replacing its retirees, in roster order. Each AI club is then full before the contract year. The contract year's demand at each position is at most that year's departures (the user club only refills to its minimum), and every departure joins the pool, so every need can be met again.
  - The alternatives were worse. Not refilling lets AI squads shrink year after year. Topping up at the contract year would put youth generation in a second task.
  - Under the old rules a vacancy could not survive to the player year, so no existing seeded run changes. No version was bumped for it, and saves from before are rejected by the schema version anyway.
- **The user club gets no vacancy youth.** Its size is the manager's choice, and the contract year's safety net still refills it to the minimum.

### Verification

- **`content`, `finance`, `events`, `inbox`:** a missing or too-small squad limit is rejected; payoffs post, restore and name their player, and 3 invalid payoff postings are rejected; 3 invalid release events are rejected, and clones share nothing; release messages only for the managed team, and 2 invalid messages are rejected.
- **App (`release_test.go`):**
  - **A release** pays exactly wage × runs left (checked against a week-by-week count), makes the player a free agent, posts one payoff and moves the total of all balances by exactly that amount. `PlayerReleased`, `LedgerPosted` and the inbox match. The retry returns the recorded result, before and after a save. The next wage run no longer pays him.
  - **The payoff formula** at 12 expiry instants around week boundaries, and overflow.
  - **9 refused releases** change nothing (compared snapshot by snapshot): zero ID, stale revision, no manager, another club's player, an unknown player, a free agent, the position's minimum, a payoff above the balance, and a pending matchday.
  - **Open bids collapse:** an AI bid for the released player closes as collapsed in the same commit.
  - **Growing to the limit:** from 20 players the manager buys past the roster count to 25, and the 26th bid is `ErrSquadFull`. The sellers are left short (there are no free agents in the first window) and get youth at the player year. Every AI squad is full after the contract year.
  - **Thirty years with a hoarding manager** (seed 7): every summer the manager releases the two oldest players allowed and buys the cheapest players up to the limit. After every contract year the world validates, every AI squad is full and transfer fees sum to zero.
  - **Invalid saves:** 10 are rejected: 8 with the journal trimmed past the release (so only `validateReleases` and `restoreRelease` can catch them) and 2 edited events.
- **Existing tests adjusted:** "full position" became "full squad" for signing and bidding, and "squad above roster" became "squad above the limit".
- **Deliberate-bug checks,** each caught: no vacancy youth (both long tests: an AI club ends the contract year with 5 midfielders); `validateReleases` removed (3 invalid saves load); a release that leaves bids open.
- **CLI:** the preview, confirmation and refusals of `release`, the payoff in `finances` and the inbox, the squad limit under `squad`, and a purchase at a full position.

### Limitations

- **The web client** still applies the old per-position cap and cannot release players. That is a `ui` note.
- **AI clubs never release players** or buy upgrades, so the market still moves only through the manager and vacancies.
- **A payoff needs the money in hand,** so a club deep in debt cannot release anyone. There are no negotiated or mutual terminations.
- **The manager may re-sign a released player** at once, on new terms.

## ui: release players and whole-squad limit in web client (done)

The web client (`cmd/web`) now supports releasing players and respects the whole-squad limit `SquadLimit` (25) instead of per-position caps.

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | POST `/release` action; release buttons with payoff preview and browser confirmation dialog on Squad page; whole-squad limit `Room` check on Free agents and Transfers pages; `SquadText` in squad header; `inbox.KindReleased` inbox text; player name on `KindPayoff` ledger lines; sorting by payoff |

### Decisions

- **Release action in squad table:** Shown only for the managed club (`IsUserClub`), with the payoff amount displayed on the button and an `onsubmit` confirmation dialog. Releasing redirects back to `/squad` with the confirmation note, matching `cmd/play`.
- **Whole-squad room:** Both Free agents and Market room checks use `len(squad) < defs.SquadLimit`. When full, Market shows "Your squad is full: a squad holds at most 25 players." and disables Bid buttons; Free agents shows "squad full".
- **Finances:** Payoff entries name the player through `s.name(e.Player)` ("contract payoff, Name"), conforming to the display convention that players are named through app queries rather than ID alone.

### Verification

- `TestReleaseAndSquadLimitInTheBrowser`:
  - Squad page shows squad limit and payoff preview with confirmation.
  - Releasing a player pays the payoff, adds an inbox message, records the payoff on the finances ledger with the player's name, and moves the player to the free agents list.
  - Position minimum rejection is enforced (releasing below 2 GK is refused).
  - Releasing while a matchday is pending is refused.
  - With 20 players and 4 forwards, bidding for another forward succeeds.
  - When the squad reaches 25 players, the Bid buttons are disabled, the squad full message is shown, and the Free agents page displays "squad full".
  - Squad list is sortable by payoff.
- All three checks (`gofmt -l .`, `go vet ./...`, `go test ./...`) pass.

## ui: full-width left-aligned layout in web client (done)

The web client (`cmd/web`) now utilizes the full available screenspace and aligns content to the left instead of clamping to a centered 1,100px container.

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | Removed `max-width: 1100px; margin: 0 auto;` on `.bar`, `nav`, and `main` in `layout.html`; added `TestFullWidthLayout` |

### Decisions

- **Full width and left alignment:** By removing the centered 1,100px maximum width constraint on `.bar`, `nav`, and `main`, content starts at the left padding edge and tables and grids expand across the full screenspace on desktop and wide screens while preserving responsive padding and phone usability.

### Verification

- `TestFullWidthLayout` confirms that the rendered HTML contains no `max-width: 1100px` or `margin: 0 auto` centering rules on `.bar`, `nav`, or `main`.
- All checks pass (`gofmt -l .`, `go vet ./...`, `go test ./...`).

## squad: the transfer list and AI transfers (done)

Clubs can now put players they don't need on a **transfer list** at an asking price, and clubs that need a player at that position bid for them. AI clubs use it both ways. They buy **upgrades**: a player clearly better than their weakest at a position. They then list the player he replaces, at a discount. Clubs with a vacancy look at the list first. So the market now moves every summer without the manager: about 25 transfers per window in an AI-only world, and every squad is back to full size at the close. The manager lists players with `list ID [PRICE]`. AI clubs bid for the manager's players only when they are listed.

```text
Wed 2026-07-01 00:00 UTC: The transfer window is open until Wed 2026-07-29 00:00 UTC; clubs answer bids made before Tue 2026-07-28 00:00 UTC.
> list 44
! app: the squad would fall below its minimum at that position: 5 DF, minimum 5
> continue
  Thu 2026-07-02 00:00 UTC  transfer: Gareth Abbott joined from Dunmarrow Albion for 250,000.00, until 1 July 2030 at 2,000.00 a week
> list 44
Elias Gallo is on the transfer list at 1,200,000.00 until the window closes; clubs that need a defender may bid.
> continue
  Fri 2026-07-03 00:00 UTC  bid: Hollowick Town bid 1,200,000.00 for Elias Gallo (offer 49); answer before Mon 2026-07-06 00:00 UTC (accept/reject)
> accept 49
Accepted: the transfer is complete.
```

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/transfers` | The transfer list: `Listing{Player, Club, Asking, ListedAt}`; `Changes.Unlist`, `Changes.List`; `Plan.Listed`, `Plan.Unlisted`; `Store.Listing`, `Store.Listings`; `Snapshot.Listings` | – |
| `internal/ai` | `ListingPrice` (`ListingPermille` 800), `Member`, `Surplus`, `ChooseUpgrade` (`UpgradeMargin` 8); `TransferCandidate.Role`, `.Listed`; `ChooseTarget` and `ChooseUpgrade` prefer listed players; `AcceptBid` judges a fee against the club's price | `ai.TransfersVersion` 2 |
| `internal/events` | `PlayerListed` (17), `PlayerUnlisted` (18) | – |
| `internal/app` | `ListPlayer` command (`Asking` zero takes a player off the list), `PlayerListed` result, `ListingRecord`; `TransferList()` and `ListedPlayer`; `SquadPlayer.Listed` (and `Value` is the asking price of a listed player); `ErrNotListed`, `ErrInvalidPrice`; the transfer run lists surplus and buys upgrades; `hasRoom` lets an AI club take one player beyond its roster count; listing checks in `validateTransfers` and the journal; `WorldSnapshot.ListingCommands` | save `SchemaVersion` 14 |
| `cmd/play` | `list` (the transfer list), `list ID [PRICE]`, `unlist ID`; your listed players under `transfers`; `listed` marks in `market` | – |

**Goldens changed on purpose.** AI clubs now trade in the first window, so the seed-42 season goldens (both leagues and the cup) moved with `ai.TransfersVersion` 2. The world fingerprint is unchanged.

### Decisions

- **The list belongs to `transfers`.** A listing is a transfer-market fact, like an offer. It is planned and applied with the offers in the same `market.commit`, so one run or command changes the list, the offers, employment and the ledgers together.
- **A listing lasts one window.** It is made while bids can still be answered, like a bid, and only above the squad's minimum at the player's position. No club may buy him otherwise, and a listing that could never draw a bid would only confuse. It ends without an event of its own when the player leaves the club (a sale or a release) or when the window closes. The close run clears the list. So the contract year and the player year never see a listing, and no player who moved in the window can be listed (he can't be sold again in it). Only an explicit withdrawal is announced (`PlayerUnlisted`). Relisting at a new price replaces the listing and emits `PlayerListed` again.
- **A club's price** is the asking price of a listed player, and its valuation otherwise. AI clubs accept any fee at or above it, and bid it. `SquadPlayer.Value` shows it, so the clients' "asking price" columns are always the fee that is accepted.
- **AI upgrades.** An AI club without a vacancy may bid, once per window, for a player at least `UpgradeMargin` (8) better than its weakest in his role, choosing the largest improvement it can afford (`ai.ChooseUpgrade`, same budget as before). The player joins as an extra. At its next run the club lists everyone at a position beyond its best `Count` (`ai.Surplus`), at 80% of their valuation (`ai.ListingPrice`), and withdraws a listed player it needs again.
  - **The bound:** `hasRoom` lets an AI club take a player beyond its roster count only while it holds no surplus, so an AI squad is never more than one player over its generated size (21). A club with an unsold surplus buys no upgrade.
  - **Why list instead of releasing** (the plan in the lane doc): a listed player stays employed and is sold for a fee, so no one is pushed into the free-agent pool to retire unreplaced, and no money leaves the game.
- **The list comes first.** An AI club picks listed players whenever one qualifies, both for a vacancy and for an upgrade (`listedFirst`). A club that has already bought in the window may still buy a *listed* player for a vacancy. That can't start a chain, because the seller only loses a surplus player. It is what empties the list: in 30-year runs every surplus player was sold within his window.
- **No herding.** An AI club leaves alone a player another club already has an open bid for, since the earlier bid is answered first. Before this rule, ten clubs bid for the same player on day one and nine bids collapsed. Now almost every bid completes.
- **The manager's players get bids only when listed.** Before, an AI club with a vacancy could bid for any of the manager's players. With upgrades, vacancies are common, and the manager was flooded with bids from the second day of every window. Listing is now how the manager sells, and AI clubs bid the asking price for listed players.
  - Consequence: a passive manager's season is no longer the AI-only season, since the manager's club takes no part in the AI market. Tests that compared the two now compare with a managed world that submits nothing.
- **Free agents are scarcer.** Clubs that sold an upgrade fill the vacancy from the list or with the best free agent. The better free agents are signed during the window, so after it the manager often finds only a few.

### Verification

- **`transfers`:** listing, relisting and unlisting, player order, snapshots; 9 invalid change sets and 6 invalid snapshots rejected without change.
- **`ai`:** the listing price is a discounted whole step; surplus is everyone but the best, independent of order and without modifying the input; upgrade choice (the largest gain, then the cheapest; roles without players skipped; an inclusive margin; no budget); listed players first for vacancies and upgrades.
- **`events`:** 4 invalid listing events rejected; clones share nothing.
- **App (`listing_test.go`):**
  - **The manager's listing:** the result, the stored listing, the squad row (`Listed`, `Value`), `TransferList`, the command-caused event, retries before and after a save, relisting (one `PlayerListed`), unlisting (one `PlayerUnlisted`), and no inbox message.
  - **12 refused listings** change nothing: zero ID, stale revision, no manager, another club's player, an unknown player, a free agent, a negative price, unlisting an unlisted player, the last day of the window, after the close, a squad at its minimum at the player's position, and a player who moved in this window.
  - **A listing ends silently** when the player is sold or released, and at the close.
  - **AI listings, every day of a window:** an AI club lists exactly its surplus at `ListingPrice`; listed players are sold; a listed player the club needs again is withdrawn by a task-caused `PlayerUnlisted`. A club holding a surplus buys no upgrade and lists it, and no AI squad ever holds more than one extra player.
  - **The list comes first:** the manager's best player, listed at a nominal price, draws exactly one bid, from the first club to act.
  - **The manager buys a listed player** at the asking price, below his valuation.
  - **8 invalid saves** rejected: a listing by a club that doesn't employ the player, a future listing, a duplicate, a moved player listed, two mismatched listing records, an edited listing event, and a run that listed the manager's player.
  - **Thirty AI-only years** (seed 7): every window completes transfers and ends with an empty list and full AI squads; fees sum to zero; the population stays put; the world validates every year.
- **Existing tests adjusted:**
  - "AI-only worlds make no offer" became "AI-only worlds trade".
  - "each AI club buys at most once" allows further purchases of listed players.
  - "AI squads exactly full" became "full, with at most one extra player".
  - The AI-bid scenarios now list the manager's player first.
  - Tests that counted a passive world's ledger entries, events or free agents count around the market.
  - Tests that pinned seed-42 names, cup winners and offer IDs in `cmd/play`, `cmd/web` and `cmd/simulate` were updated.
- **Deliberate-bug checks,** each caught: a sale leaving the listing in place; the close not clearing the list; unlimited AI surplus; listing validation removed; AI clubs ignoring the list; AI clubs bidding for unlisted manager players.
- **Simulation** (30 years, AI-only, seeds 7 and 42, measured after each window): 22–31 transfers per window, bids roughly equal to completions, every AI squad back at exactly 20 and the population at 320 every year.

### Limitations

- **AI clubs list only surplus.** They don't list declining or unwanted players to raise money, and they sell unlisted players to anyone who pays the valuation, their best included.
- **Listings don't carry over** to the next window, and a listing has no minimum fee below the asking price: a bid is accepted at the asking price or not at all.
- **Balances keep diverging** over decades (as before this change): AI clubs spend at most one upgrade a year, so rich clubs keep growing. Money use belongs to the "AI money" backlog item.

## ui: the transfer list in the web client (done)

The web client now lets the manager put players on the transfer list, change their asking prices and withdraw them. The Transfers page shows every listed player, offers bid forms for other clubs' listings, identifies the manager's own listings, and marks listed players in the wider positional market.

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | Added `POST /list`; Squad-page list, reprice and unlist controls; a Transfer list section with bids at the asking price; listed markers in the market; an explanation that incoming bids require a listing |

### Decisions

- **One action for listing changes.** `POST /list` submits a positive whole-unit asking price to list or reprice a player; an explicit `unlist=yes` submits the app contract's zero asking price to withdraw him.
- **App facts are displayed directly.** Asking prices and listed state come from `SquadPlayer.Value` and `SquadPlayer.Listed`; the transfer-list rows come from `World.TransferList`. The client adds no transfer rules.
- **The page works without JavaScript.** Every list, reprice, unlist and bid action is a server-rendered form carrying the world revision.

### Verification

- `cmd/web/main_test.go`: `TestTransferListInTheBrowser` drives the forms on seed 42, club 3. It lists, reprices and unlists; checks the minimum-position refusal; receives Hollowick Town's 700,000.00 bid for Callum Ibsen; and verifies AI listings, bid forms and market markers.
- `TestTransfersInTheBrowser` now lists Elias Gallo through the browser action instead of calling `World.ListPlayer` directly.
- All checks pass (`gofmt -l .`, `go vet ./...`, `go test ./...`).

## match: the manager's lineup carries over (done)

A lineup the manager submits now stands until they change it. A pending fixture with no lineup submitted is played with the lineup the user club played in its previous match, with the same tactics. Before this, one "Play match" without resubmitting dropped the manager back to the AI's pick. If a player from that lineup has left the squad, his starting place is refilled by the AI and he is reported as dropped. `ResolveRounds` and `PlayMatch` field exactly what the new `MatchdayLineup` query shows, and the report says `SelectedByManager`. This answers `ui`'s note `match--preserve-lineup-selection`. The same session reviewed and closed `squad`'s note `match--managed-season-baselines` without changes: the tests' match-lane invariants survived. A passive manager is still reported as AI-selected, and submitting the suggestion still plays the passive season. The invariant "a passive manager plays exactly the AI-only season" belongs to the transfer market.

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/ai` | `Slot`, `RefillLineup`: fills a saved lineup's vacancies as `SelectTeam` fills a role and cuts the bench to `MaxBench`; `SelectTeam`'s candidate checks moved to `canonicalPool` (same behavior) | – (`SelectTeam` unchanged) |
| `internal/app` | `LineupSource` (`LineupFromSubmission` 1, `LineupCarriedOver` 2, `LineupSuggested` 3), `MatchdayLineup{Fixture, Lineup, Source, From, Dropped}`, `World.MatchdayLineup`; `sideSelection` plays the carried lineup, and `ResolveRounds` stores it for the fixture once the results are official | – (no save change) |

### Decisions

- **Derived, not stored as a new fact.** The standing lineup is the user team's stored lineup for its latest earlier fixture, by kickoff and then fixture ID. So the save schema doesn't change and old saves carry over at once.
- **The lineup played is stored.** When a carried lineup is played, `ResolveRounds` stores it in `selection` for that fixture. Two things follow. The next match carries on from the lineup that was actually fielded, refills included. And the save rule "a report says `SelectedByManager` exactly when a lineup is stored" still holds. The store is written after the competitions commit, like the medical and finance plans, and it can't fail there because `lineupInput` already validated the lineup. No event is emitted: the manager didn't submit anything, and `MatchCompleted` covers the commit.
- **Refill, don't discard.** A lineup with a player who left is kept and only his slot is refilled. Otherwise a single summer sale would silently throw away the manager's whole selection and mentality. If a slot can't be filled (no goalkeeper left), the AI selects the whole side, and the query reports `LineupSuggested`.
- **`SuggestLineup` is still the assistant's fresh pick**, so clients can offer "ask the assistant". `SelectedBy` gets no new value: a carried lineup is the manager's.
- **No version bump.** Seeded output changes only for careers in which the manager submitted a lineup and later didn't. No golden covers that case: the goldens submit nothing or submit every fixture, and all of them are unchanged.

### Verification

- **`ai`:** a full lineup comes back unchanged, with the bench cut to a smaller limit; only the vacancy is refilled, with the best natural player not starting (a bench player who moves up leaves the bench); input order doesn't matter and the inputs aren't modified; 5 lineups it cannot carry are rejected.
- **App:**
  - **A season carrying round 1's lineup** (seed 42, club 3, 14 matchdays): every later fixture reports `LineupCarriedOver` from the previous one. The results equal those of a world that submits the same lineup each time. Every user side is `SelectedByManager`, each lineup played is stored, and the world validates and round-trips through a save.
  - **A released starter:** the next match drops only him, refills his slot in the same role, keeps every other slot and the tactics, is played by the manager, and round-trips through a save.
- Every existing test and golden passes unchanged.

### Limitations

- **No way back to a weekly assistant pick.** Once the manager submits a lineup, it stands. Submitting the suggestion fixes that particular lineup rather than letting the assistant choose again every week. This is in the match backlog.
- **Condition doesn't move a carried starter.** A tired player keeps his place until the manager changes it. The clients show condition.
- **The clients** still show the suggestion instead of the carried lineup: that is the `ui` note `ui--matchday-lineup`.

## ui: lineup carryover and dropped players in clients (done)

Both the terminal (`cmd/play`) and web (`cmd/web`) clients now support lineup carryover across matchdays and name dropped players when a carried lineup is refilled.

### Changes

| Package | Change |
| --- | --- |
| `internal/app` | Created `views.go` with read-only view helpers `LineupSourceLabel`, `LineupDroppedMessages`, and `OpponentName` |
| `cmd/play` | Uses `MatchdayLineup` for draft initialization; displays lineup source ("your lineup for this match", "carried over from the last match (vs X)", "the assistant's suggestion"); reports dropped players and their replacements; reports "Lineup: your lineup" when played; added `assistant` command alias to `reset` |
| `cmd/web` | Populates `LineupSource` and `DroppedNotes` on home matchday panel, matchday announcement, and `/lineup`; added "Ask the assistant" / "Your saved lineup" toggle on `/lineup`; reports "Your lineup" in game reports |

### Decisions

- **Shared read-only view helpers in `internal/app/views.go`**: As designated by the `ui` lane charter, `views.go` houses read queries that only combine existing `app` queries (`MatchdayLineup`, `SubmittedLineup`, `PlayerName`, `Squad`, `userTeam`, `competitions.Fixture`).
- **Consistent source labeling across clients**: Both clients distinguish newly submitted lineups, carried-over lineups naming the opponent they were last played against, and assistant suggestions, keeping the same terminology.
- **Naming dropped starters and replacements**: When a carried lineup drops a player who left the squad, both clients name the departed player and the replacement player who takes his starting place.

### Verification

- `cmd/play/main_test.go`: `TestLineupCarriesOverToNextMatchday` verifies Round 1 submission carries into Round 2 with "your lineup", and releasing a starter between rounds names the dropped player and his replacement.
- `cmd/web/main_test.go`: `TestLineupCarriesOverInTheBrowser` verifies the Home screen matchday note, `/lineup` screen with carryover state, dropped starter notice, "Ask the assistant" toggle, and "Your lineup" on the game report.
- All checks pass (`gofmt -l .`, `go vet ./...`, `go test ./...`).

## balance: sweep of the AI transfer market (done)

Answers `squad`'s note `balance--ai-transfer-market`. Two sweeps run 30-year careers with the AI market at `ai.TransfersVersion` 2: 10 seeds AI-only, and 4 seeds with a passive manager at club 3. They measure every window, season and contract year, and log a report. The numbers and the command that reproduces them are in [docs/balance.md](balance.md#ai-transfer-market).

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/app` | `balance_test.go`: `TestBalanceAIMarketSweep`, `TestBalanceAIMarketWithPassiveManager`, and the `sweepMarket` and `reportMarket` helpers. They skip unless `ZIMBLE_BALANCE=1` (about 20 s) | – (tests only) |
| `docs` | `balance.md` created, with the market numbers | – |

### Findings

- **Volume:** about 27 transfers and 12 AI listings per window, and 0–3% of listings go unsold. Of 8,358 bids, none was rejected or expired. Squads stay full and the population stays at 315–321.
- **Churn:** the best players move every year. 12 of the 16 best players move in each window, one player moved in 16 consecutive windows, and 44% of transfers go to a weaker club. Filed as `squad--star-churn`.
- **Strength doesn't concentrate:** the top-to-bottom squad-average gap stays at 6–8 points, and each 8-club league has 7–8 different champions in 30 seasons.
- **Money:** no club goes below zero. The median balance grows from 2.2M to 8.5M in 30 years, and the spread between clubs grows from 2.8M to 13.2M, mostly from net transfer spend. No tuning requested yet.
- **Free agents:** the pool is empty at every window open and close in AI-only careers, and a manager finds nobody worth signing. Filed as `squad--free-agent-pool`.
- **Incoming offers:** AI clubs never bid for a manager's unlisted players (0 bids in 120 passive windows). That is the rule as designed.

### Decisions

- **Gated, not always on.** No always-on bound was added. `TestAIMarketKeepsSquadsFullForDecades` already guards squad sizes and the population, and a churn bound would fail today on the finding itself. The bound belongs with `squad`'s fix.
- **Events by ID.** The sweep reads listings from the journal by event ID, and fails if the journal (1,000 events retained) dropped any before they were read.
## ui: multiple saves and startup save selector in the web client (done)

The web client (`cmd/web`) now discovers saved careers and provides a save selector on startup so players can resume a saved career directly from the browser without needing the `-load` flag. It also supports multiple save files and "Save as" copies during gameplay.

### Changes

| Package | Change |
| --- | --- |
| `cmd/web` | Added `-saves` flag (default `saves/`); `server.listSaves()` scans `saves/` and root `career.json` for valid Zimble saves; added `POST /load` action with path traversal protection; updated `POST /save` to support custom save names ("Save as"); added Saved Careers table to `choose.html` and Saved Career management panel to `home.html`; added "Save as…" popover to header in `layout.html` |

### Decisions

- **Startup save selector**: When running `cmd/web` with no active career (`s.w == nil`), the initial page renders a "Saved careers" table above the "New career" club picker, showing file name, club name, in-game date, season, and last saved timestamp with a one-click "Load career" action.
- **Multiple saves and Save as**: Added "Save as" functionality in both the navigation header (via native HTML `<details>` popover) and on the Home dashboard panel, allowing players to save copies under custom names (e.g. `season-2.json`).
- **Security & directory isolation**: Save and load actions validate against directory traversal (`..` and path separators), keeping save files confined within the designated saves directory.
- **Backwards compatibility**: Existing root `career.json` and `-load`/`-save` flags continue to work seamlessly.

### Verification

- `cmd/web/main_test.go`: `TestSaveSelectorOnStartupAndMultipleSaves` verifies discovery of multiple saves, loading from the startup page, creating copies via Save As, path traversal rejection, and switching active careers.
- All checks pass (`gofmt -l .`, `go vet ./...`, `go test ./...`).

## match: a tick-based match engine (done)

A second match engine, `internal/matches/tick`, moves the ball and all 22 players across the pitch five times a second. Goals come out of play: formation spots that follow the ball, marking, pressing, passes to open teammates, dribbles, tackles, shots, saves, parries, deflections, throw-ins, corners, goal kicks and kickoffs. It implements the same session contract as `simple` (half time, substitutions, mentality, knockout shootouts). It is the first engine with positional frames: where the ball and every player are at each instant. No career match uses it yet; wiring it into `app` is phase 2 of the roadmap in [lanes/match.md](lanes/match.md#tick-engine-roadmap). To watch it: `go test ./internal/matches/tick -run TestWatch -v` prints a minute of play as text, one frame a second.

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/matches` | `AdvanceRequest.Frames`, `MatchStepResult.Frames`, `Frame`, `PitchPoint`, pitch geometry constants (`PitchLength`, `PitchWidth`, `GoalWidth`). Additive: zero values keep the old behavior | – |
| `internal/matches/simple` | rejects a frame request with `ErrUnsupported` and resets `dst.Frames`; tests now run the shared contract suite, and its own copies of those tests are gone | – (output unchanged) |
| `internal/matches/enginetest` (new) | the contract suite every engine runs: invariants, determinism, chunking, half time, substitutions, rejected commands and advances, input and buffer ownership, knockouts, frames. Also the shared test inputs | – |
| `internal/matches/tick` (new) | the engine, its `Params`, trend tests, a golden over outcomes and frames, a benchmark and a text pitch viewer | `tick.ModelVersion` 1 |
| `internal/app/boundaries_test.go` | entries for `tick` and `enginetest` (both: core and `matches` only) | – |

### Decisions

- **Behind the existing contract.** No caller changes. The contract suite moved into `enginetest` so "passes the same tests as `simple`" is a function call, not a copy. `simple` runs it too.
- **Frames are opt-in and never change the match.** An engine emits one frame per instant only when asked, and the suite checks that events and outcomes are identical with and without frames. Coordinates are centimetres on a 105 × 68 m pitch; the teams change ends at half time. Frames are presentation, not results.
- **Integer physics.** Positions in cm, speeds in cm per 200 ms tick, constant ball deceleration, integer square roots. No floats, as the project rules require. Distances are compared squared where possible, which halved the running time.
- **Same ratings, same fatigue.** The engine reads the six existing ratings. Readiness, fatigue and home advantage work as in `simple`; home advantage is 3% on every effective rating. Pace sets sprint speed. Dribbling is stood in for by the mean of Passing and Pace until `data` can provide an attribute (note `data--match-attributes`).
- **Fair contests.** Two players arriving on a ball in the same instant contest it evenly (a random order), not in team order. The team-order tie-break was a hidden home advantage of 94–54 wins in 200 even matches.
- **Mentality** moves the lines 2.5 m, changes shot eagerness and pass ambition by ±15%, and makes an attacking side press with two players in the opponent's third. The second presser anywhere in the opponent's half more than doubled goals, so it is limited to the final third.
- **Model detail stays inside the package.** Shot, pass, tackle and possession counters exist for tests only; exposing them is roadmap phase 3 (`DetailedStats`).

### Verification

- **Contract suite** on both engines: 300 matches for `simple`, 60 for `tick`, with 400 and 150 knockout pairs, plus frames: one frame per instant, in clock order, on the pitch, with the carrier on the pitch.
- **Trends** (`TestModelTrends`, 200 matches each, seed 42): equal teams 2.94 goals a match, home 44%, away 35%, draws 21%, 14 shots a side, 79% of about 830 passes completed. At 55 v 65 the stronger side wins 56%. Attacking 3.4 goals, defensive 1.45. A home side at condition 30 wins 15% (`TestConditionMatters`).
- **Physics:** no player moves faster than `MaxSprint` between two instants of play (`TestPlayersMoveAtHumanSpeed`); only the line-up for the second-half kickoff moves players at once.
- **Golden** over three matches' frames and outcomes, with substitutions, mentality changes and a shootout.
- **Speed:** about 20 ms and 13 allocations per match (`simple`: 0.03 ms). `go test -short` skips the trend tests.

### Limitations

- **Not in careers yet**, and too slow for every background match: a 380-match league season takes about 8 s.
- **No offside, fouls, cards, penalties in play, headers or crosses.** The ball never leaves the ground, runs stop level with the last defender, and throw-ins and corners are rare because players rarely miss the ball.
- **Simple decisions:** each tick a carrier shoots, passes or dribbles with a fixed hazard; nobody makes runs into space.
- **Statistics are not in the outcome** and there are no checkpoints, as in `simple`.

## balance: tick engine against simple engine (done)

Answers `match`'s note `balance--tick-engine-profile`. A gated sweep plays both engines on the same `enginetest` teams and seeds: 17 scenarios × 3,000 knockout matches each, covering equal teams, rating gaps, quality levels and mentality pairings. The numbers and the command are in [docs/balance.md](balance.md#match-engines-tick-against-simple).

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/matches/tick` | `balance_test.go`: `TestBalanceEngineComparison` runs `simple` and `tick` in parallel and logs markdown tables. It skips unless `ZIMBLE_BALANCE=1` (about 60 s on 32 cores). It checks that knockout shootouts equal level matches | – (tests only) |
| `docs` | `balance.md`: section "Match engines: tick against simple" | – |

### Findings

- **Equal teams:** `tick` 2.92 goals and 44 / 25 / 31 home / draw / away; `simple` 2.75 goals and 39 / 27 / 35. `tick`'s draws are not low: the 21% `match` saw was 200-match noise. `simple`'s home edge is weak but plausible.
- **Mentality in `tick`:** attacking against balanced adds 20 points of win rate at no cost, and a defensive side against a non-defensive one plays in 4.7-goal matches. Filed as `match--tick-mentality`.
- **Goals by level in `tick`:** equal teams score 2.1 goals at 40, 2.9 at 60 and 4.6 at 80, and a 20-point mismatch averages 4.3. Filed as `match--tick-goals-by-level`.
- **Shootouts, both engines:** the stronger side wins 74–80% of shootouts at 65 v 55 and 88–90% at 70 v 50. Filed as `match--shootout-favourite`.

### Decisions

- **One run for league and cup:** every sweep match is a knockout. The contract guarantees the 90 minutes are unchanged by the knockout rule, so regulation results and shootouts come from the same matches.
- **Synthetic teams, not a career:** no career match can use `tick` until `app` chooses the engine per match (`match` roadmap phase 2). The league-season comparison waits for that and is in the balance backlog.
- **No always-on bound:** `tick`'s `TestModelTrends` already guards the signs, and a mentality or shootout bound would fail today on the findings themselves. Those bounds belong with `match`'s fixes.

## ui: player profile (done)

Any player, at any club, free or retired, has a profile page in both clients.

### Changes

| Package | Change | Version |
| --- | --- | --- |
| `internal/app` | `views.go`: `PlayerProfile(id)` returns the `SquadPlayer` row plus club and retired flag. It only combines existing queries | – |
| `cmd/web` | `/player?id=N` page; player names in the squad, lineup, free agents and transfer market link to it | – |
| `cmd/play` | `player ID` command (`p`) | – |

### Decisions

- **No new state:** the profile shows what `SquadPlayer` already holds. Career history (past clubs, goals, transfers) needs stored data and waits for a note to `data`.
- **Unknown IDs are a message, not an error page,** so the web page always renders with status 200.

## data: persisted inbox read state (done)

`World.MarkInboxRead` acknowledges one retained inbox message by its source event ID. `World.Inbox()` exposes `InboxItem.Read`, and `World.UnreadInboxCount()` counts unread retained messages. Queries remain read-only; new arrivals start unread. The command also supports league-wide messages in careers without a managed club.

### Changes

- Added durable event kind `KindInboxRead = 19` and the command's saved request/result records. Acknowledgements update the existing message rather than creating another message. Retries return the original result without changing the revision or journal, including after the message is evicted.
- Inbox application stages all changes before committing, including read flags, and trims in event order so batch and single-event replay agree. Command records validate retained read flags after the journal has been trimmed.
- Save schema **15** persists read flags and retry records. Schema 14 and earlier are explicitly rejected by the codec with `ErrUnsupportedSave`.
- Moved the shared `commandRecord` declaration from `resolve.go` into the `world.go` hub; existing command variants and match behavior are unchanged. Future command additions can use the hub rule.
- Clarified the events version policy: new kinds and optional fields preserving existing meaning need a storage schema bump; changing or removing existing fields needs a new events version. `events.SchemaVersion` stays 1.

### Verification

Tests cover command retries and ID collisions, rejected-command atomicity, independent read flags and new arrivals, deterministic save continuation, codec round trips, explicit old-schema rejection, tampered records/events/read flags, journal retention, inbox eviction, batch replay equivalence and rollback on an invalid event after a staged read.

### Handoffs

UI receives the command/query contract and display work in `ui--inbox-read-state.md`. Match and squad receive the shared command declaration's new location. The accepted five-attribute request still waits for squad's answer.

## ui: season history (done)

### Changes

| Client | Command / page | Uses |
| --- | --- | --- |
| `cmd/web` | `/history` (History tab); `?competition=&season=` shows one season | `World.History`, `Table`, `Cup` |
| `cmd/play` | `history [COMP SEASON]` | same |

### Decisions

- **No new state:** every season and its champion come from `World.History`; a league season shows its final table, a cup edition its bracket. In-progress seasons are listed too.
- **Shared rendering:** the cup bracket template (`cupedition.html`) and `printEdition`/`printTable` serve both the current-cup page and history.
- `cmd/simulate` already prints champions, so it is unchanged.

## ui: inbox read state (done)

Unread and read messages in both clients, using `InboxItem.Read`, `UnreadInboxCount` and `MarkInboxRead`.

| Client | Where | Uses |
| --- | --- | --- |
| `cmd/web` | unread count in the Inbox tab, unread styling on the Home and Inbox pages, "Mark read" per message and "Mark all read" (`POST /inbox/read`) | the three `app` calls above |
| `cmd/play` | `*` marks unread messages in `inbox`; `read` marks all read | same |

### Decisions

- **Viewing does not acknowledge:** the web pages and `inbox` only list. `cmd/play` still acknowledges what it prints as "New in your inbox" and what its own commands caused, replacing the client-local `lastSeen` marker with the stored flag, so a saved career keeps its read state.
- Mark all issues one command per unread message, each at the revision after the previous one; a failure stops there and reports the error.

## data: lane backlog review (done)

Reviewed all six lane documents and added scoped proposals for player comparisons and a decision overview, editable team plans and lineup explanations, season review and scheduling/discipline rules, contract and finance planning, durable player history and save recovery, and policy comparisons and career endurance measurements. Dependencies are named in each item; these are backlog proposals, not implemented features or changes to the lanes' current priorities.

Corrected references to closed inbox/transfer-list/balance handoffs and noted that match outcomes already contain participant minutes. Existing UI requests for season-end stopping and lineup editing outside matchday remain the basis for the corresponding domain proposals.

## competitions: promotion and relegation, rule and movement function (done)

The rule is decided, built and wired: at every season end the bottom two of each first division swap leagues with the top two of its second division, and a career over any number of seasons keeps every club in exactly one league.

| Package | Change |
| --- | --- |
| `internal/competitions` | `Link{Upper, Lower, Places}` and `NextEntrants(rankings, links)`: next season's entrants of every league from all final rankings |
| `internal/app` | `endSeasons` derives each league's next entrants with `NextEntrants` from the cohort's final rankings; `validateSeasons` replays it for every earlier season; `World.promotions` (from `content.DefaultPromotions`) is pinned in `WorldSnapshot.Promotions`, validated by `checkPromotions` (content rules, plus linked leagues share one calendar so they end in one cohort); `World.Promotions()` query. `storage.SchemaVersion` 17 |

### Decisions

- **Direct swap, no play-offs.** At a season's end the bottom N of the upper division's final ranking swap leagues with the top N of the lower's, so every division keeps its size. Play-offs would need extra fixtures between seasons and a knockout stage in the league calendar; they can come later as a different rule without changing the swap.
- **Two places per link** is the proposed default; a link may not move more than half of either league, so promoted and relegated teams never overlap.
- **Decided from the season just finished, for all leagues at once.** A team moves at most one division a year, even in a three-tier chain. Every league's next season is created in one `CreateSeasons` call from `NextEntrants`, once all linked leagues have ended, so no league's entrants depend on order.
- **Pure function of rankings and links.** Validation will replay movement from recorded results: a league's entrants in season N+1 must equal `NextEntrants` of season N's rankings. Rankings are already derived, so nothing new is stored, and history stays coherent because past seasons keep their own entrants and results.
- **Links are content pinned in the save**, like cups. `data` defines the type; competitions wires it.

### Verification

- Swaps keep sizes and move exactly the linked places across a three-league chain; no links leaves teams unchanged; 6 invalid configurations are rejected.
- `TestPromotionKeepsEveryTeamInOneLeague`: over five seasons every team is in one league, sizes hold, and each season's entrants equal `NextEntrants` of the previous rankings. `TestPromotionSaveRejections`: bad links, differing calendars, and links removed or edited after teams moved are rejected on restore.
- The seed-42 season 2 draw changed (the entrants moved), so `TestContractsInTheBrowser` expects a different first opponent.

### Limitations

- A failing test outside this lane: `TestAIMarketKeepsSquadsFullForDecades` (seed 7) now fails in year 12, when second-division club 31 sells a defender near the window close and ends short ([squad note](handoffs/squad--seller-left-short.md)).
- The calendar still drifts a day earlier each year (`SeasonInterval` is 52 weeks); left as it is, in the backlog.
- A link needs its two leagues to share a calendar; content that differs is rejected.

## data: second divisions (done)

Each nation now has a second division, and the default world has four leagues and 32 clubs. Delivers `competitions`' request; the movement between divisions is wired by `competitions`.

### Changes

| Package | Change |
| --- | --- |
| `internal/content` | `Nation` holds `Divisions []Division{Clubs, Towns}` instead of `Clubs` and `Towns`; the default nations have a second division with eight new towns each. `DefaultLeagues()` returns leagues 1, 2, 4 and 5 (the cup is 3). `Promotion{Upper, Lower, Places}`, `DefaultPromotions()` and `ValidatePromotions`. `Version` 6, `LeagueVersion` 4 |
| `internal/worldgen` | Generates division by division, nation by nation: every nation's top division first, then every second division. `Version` 5 |
| `internal/storage` | `SchemaVersion` 16: the content in a save has a new shape |
| tests | Goldens re-pinned; counts and seed-42 expectations updated in `internal/app` and `cmd/*` |

### Decisions

- **The top divisions are untouched.** Clubs 1-16 keep their names, players, contracts and birth dates for every seed: the new divisions come after them in ID order and draw from their own streams (`worldgen/clubs` keyed by nation and tier), and `streamVersion` did not change. `TestDivisionsAreGeneratedIndependently` proves a top-division-only world is a prefix of the full one. The generated world's fingerprint still moves (more clubs), and so do seeded careers: the AI market spans 32 clubs.
- **Content shape.** A division carries its own towns, so adding a tier never changes how the tiers above pick their towns. Every nation must define the same number of tiers, so tier-major generation lines up with the leagues (which take clubs in league ID order).
- **Links are validated in content.** Same size, at most half the places, one upper end and one lower end per league, no loops. A chain of divisions is allowed.
- **The cup stays with the first divisions** (leagues 1 and 2, four places each).
- **No rating gap yet.** Second-division squads are generated like first-division ones. A weaker profile is a possible content change once `balance` reports.

### Verification

- Content validates the default world and rejects uneven, missing and duplicated divisions and 10 broken promotion configurations; the default leagues take exactly the world's clubs.
- A world of top divisions only, or of one nation, is a prefix of the full world for the same seed.
- The full suite passes on the new world: 30-year AI market, several-season careers, save round trips and the CLI and web clients.

### Handoffs

[competitions](handoffs/competitions--second-divisions-delivered.md) (fields and two things to know), [ui](handoffs/ui--second-divisions.md), [squad](handoffs/squad--market-spans-32-clubs.md), [balance](handoffs/balance--rerun-baseline-32-clubs.md), [match](handoffs/match--goldens-32-clubs.md).

### Limitations

- The transfer window and the season calendar drift apart by one day a year (52-week seasons); from about year 28 a first round kicks off inside the window. Reported to `competitions`.

## squad: sellers keep needed players late in the window (done)

An AI club no longer sells a player it needs when the answer comes too late for it to replace him. Promotion and relegation had exposed the gap: in seed 7's twelfth window, second-division club 31 sold a defender at the second-to-last run, could no longer bid for a replacement, found no defender in the pool at the close and started the season one short (`TestAIMarketKeepsSquadsFullForDecades`). Closes the `squad--seller-left-short` note.

### Changes

| Package | Change |
| --- | --- |
| `internal/ai` | `AcceptBid(fee, price, spare, replaceable)`: a fee at or above the price, for a player the club can spare or still has time to replace. `TransfersVersion` 3 |
| `internal/app` | `World.replaceable`: a club selling at a run can bid for a replacement at that run and, should that bid fail, at the next. The transfer run passes it with `spare` (the club holds more than its roster count at the position); `candidates` leaves out players an AI club would refuse to sell when the bid is answered, so AI buyers don't waste a bid on them. `TransferWindow.NeededClose` tells clients when that starts |

### Decisions

- **One retry, not one bid.** Requiring only that the seller can still bid at the answering run would fix seed 7, but a replacement bid that collapses at the last answering run would leave the club short again. One more run covers that; with the 28-day window, needed players stop being sold three days before the close.
- **A timing rule, not a pool check.** Allowing the sale when the pool holds a free agent at the position would keep a few more late deals, but other clubs can take that free agent before the close.
- **The goldens didn't move.** Seed 42's first window has no late sale of a needed player, so the version bump changes no pinned season.

### Verification

- `TestLateBidForANeededPlayerIsRejected`: the manager's bid made at `NeededClose` for a forward his club needs is rejected and the player stays; it fails without the rule. `TestAcceptBidKeepsANeededPlayerLate` covers the decision.
- The full suite passes, the 30-year AI market (seed 7) and the long career tests included.

### Handoffs

[ui](handoffs/ui--ai-seller-rules.md): say when AI clubs stop selling needed players (with the rules of the next section).

### Limitations

- A replacement bid for a manager's listed player can wait up to `ResponseDays` for the answer; if the manager lets it expire at the close, the AI club may still end short when the pool is empty. The long tests have not hit it.

## squad: sellers keep their stars (done)

The best players no longer change clubs every summer. Before, an AI club sold any player at its valuation, so the league's best player was every club's largest upgrade and moved in every window, often to a weaker club. Now a seller has preferences of its own. Closes `balance`'s `squad--star-churn` note.

### Changes

| Package | Change |
| --- | --- |
| `internal/ai` | `SellingPrice`: an AI club asks its valuation plus `KeyPermillePerPoint` (60 permille, 6%) for each point a player rates above its squad average. `Joins`: a player `StarMargin` (10) or more above his club's average joins only a club at least as strong. `AcceptBid` takes a `Sale` (fee, price, spare, replaceable, listed, settling, overall, both averages). `TransfersVersion` 4 |
| `internal/app` | The market prices unlisted AI players with `SellingPrice` against squad averages taken before the run, knows which players joined their club by transfer in the previous window (`settling`), and passes both to `AcceptBid`; `candidates` leaves out the players a seller would refuse. `SquadPlayer.Value` is the selling price (`sellingPrice`), so the price the manager sees is the one the AI club answers against. `World.BidRefusal` tells a client which rule would refuse the manager's bid (`RefusalStar`, `RefusalSettling`, `RefusalNeeded`). `squadAverage` |
| tests | Seed-42 goldens re-pinned. `cmd/play`, `cmd/web` and `cmd/simulate` seed-42 scripts updated: the manager's club now reaches the cup final in season 1, and the old bid targets are stars of stronger clubs |

### Decisions

- **Three rules, each for a different failure.** The price premium makes a club's best player cost two to three times a typical upgrade. Settling in stops a player being sold on the year after he arrives, which caps a kept player's streak at one window. Stars choosing their club stops stars moving down. Price alone was not enough: with it and settling in, 7 of the 16 best players still moved per window, rising as balances grew, and 42% of moves went to weaker clubs.
- **Only stars choose.** With every above-average player refusing weaker clubs, the strength gap grew from 8 to 14 points: weak clubs lost all their good players upward. Limiting it to players 10 points above their club's average keeps the gap at 11.
- **Listed players are exempt.** A club that lists a player wants him gone, so neither settling in nor the premium applies; the listing discount still does.
- **The manager is not an AI seller.** His players show their valuation and he answers bids himself. The star rule does apply to his bids: a star of a stronger club refuses him. That is a gameplay change the UI should explain ([note](handoffs/ui--ai-seller-rules.md)).
- **Squad averages are taken at the start of a run** (before its staged changes), so every decision in a run sees the same prices; a bid answered at the next run meets that run's prices.

### Verification

- `TestAStarRefusesAWeakerClub`, `TestANewSigningIsNotSoldOn`: the manager's bid at the shown price for a star of a stronger club, or for a player bought in the previous window, is rejected, and no AI bid breaks either rule; both fail without the rules. `ai` tests cover `SellingPrice`, `Joins` and each `AcceptBid` condition.
- The balance sweep (10 seeds, 30 years): 4 of the 16 best players move per window (was 15); no player who was ever among the 16 best moved in two consecutive windows; 31% of moves go to a weaker club (was 42%); the squad-average gap is 11 (was 8-9); every league still has 10-15 champions in 30 seasons. Details in the [balance note](handoffs/balance--star-churn-delivered.md).
- The full suite passes, the 30-year AI market and the long career tests included.

### Handoffs

[balance](handoffs/balance--star-churn-delivered.md): rerun the sweep. [ui](handoffs/ui--ai-seller-rules.md): explain the refusals (replaces the note filed for late sales).

### Limitations

- Stars move more as AI balances grow (5-6 of 16 per window by year 30): late in a career rich clubs meet any price. The fix belongs to `AI money` (a money sink, budgets).
- A few listed journeymen still move in four to seven consecutive windows.
- Selling price and the star rule use a squad's mean overall, so a club's quality in one position doesn't count.

## data: five match attributes (done)

Players now carry Dribbling, Heading, Strength, Acceleration and Positioning on the 1–100 scale, for the `tick` engine's next phase. Delivers `match`'s request (`data--match-attributes`) with `squad`'s rules for `players` (`squad--match-attributes`).

### Changes

| Package | Change |
| --- | --- |
| `internal/players` | `Dribbling` 6, `Heading` 7, `Strength` 8, `Acceleration` 9, `Positioning` 10; every `Attribute` constant has an explicit value and `NumAttributes` is 11. `Growth`: acceleration declines with pace (a point a year faster from 29); strength and positioning a point a year more slowly from 29 through 32 (0, 0, −1, −1), then like the rest; dribbling and heading follow the base curve. `DevelopmentVersion` stays 1 |
| `internal/content` | A range per position for each new attribute (GK / DF / MF / FW, from the note; youth ranges are lowered by `RatingGap` as before). `Version` 7 |
| `internal/worldgen` | The five are drawn last in each player's stream (after the birth date) and in each youth stream. `Version` 6, `YouthVersion` 2; `streamVersion` stays 2 and the new `youthStreamVersion` keeps the youth streams at 1 |
| `internal/storage` | `SchemaVersion` 18: `Attributes` has eleven values. Schema 17 saves are refused with `ErrUnsupportedSave` |
| `cmd/web` | The squad, lineup and player tables show the six ratings they have headers for (`ratings` template function); they ranged over every attribute. How to show the new five is `ui`'s design |

### Decisions

- **Appended draws, not a reshuffle.** Names, the first six attributes, contracts and birth dates are unchanged for every seed, and so are youth players' names, birth dates and first six attributes. Only the world fingerprint moves.
- **`Overall` is unchanged** (`keyAttributes`, squad's call), so wages, valuations and AI selection don't move, and `simple` ignores the new attributes. Seeded careers play out exactly as before.
- **`DevelopmentVersion` stays 1.** It keys the development and retirement streams. `Develop` draws the five after the first six, which develop exactly as before, and the schema bump already refuses older saves.
- **Old saves are refused, not migrated.** A schema 17 save has no values for the five, and restore must not generate them from the seed.

### Verification

- `TestMatchAttributesKeepEarlierDraws`: the seed-42 world, with the five dropped and encoded as worldgen v5 did, hashes to the old golden. `TestYouth` pins four youth players' v1 names, birth dates and first six attributes.
- `TestDevelopKeepsTheFirstSixDraws` pins `Develop`'s output from before the change. `TestGrowthOfTheMatchAttributes` covers each new growth rule.
- The seed-42 season, second-league and cup goldens are unchanged. Three seasons of `cmd/simulate -seed 42` (save and load between seasons) print the same output as the previous build, apart from the version line and the fingerprint. The final saves are equal field by field, all 665 players included, once the five values are dropped.
- Restore rejects a profile with a zero Positioning. `TestTableRowsMatchTheirHeaders` (cmd/web) failed before the template fix: 21 cells under 16 columns.

### Handoffs

[match](handoffs/match--match-attributes-delivered.md): the `matches.Ratings` fields and `World.candidate`. [ui](handoffs/ui--match-attributes.md): show the five. [balance](handoffs/balance--match-attribute-spreads.md): check the ranges. `squad` already has "Overall with the new attributes" in its backlog.

### Limitations

- Nothing uses the five yet: `matches.Ratings` doesn't carry them until `match` adds the fields.
- The ranges are the note's first proposal and haven't been measured.

## data: content fingerprint covers promotions (done)

Found reviewing the second-divisions wiring: `WorldSnapshot.Promotions` is pinned by the save, but `contentFingerprint` hashed only the definitions, leagues and cups. A save with its promotion places edited mid-season still loaded, and the career then ran under different rules. The fingerprint now includes the links. It rides schema 18 from the section above, so no save written by an earlier build meets the new format.

### Changes

| Package | Change |
| --- | --- |
| `internal/app` | `contentFingerprint(defs, leagues, cups, promotions)`; `Snapshot` and `Restore` pass the links. Tests that re-sign a mutated snapshot pass them too, and `TestPromotionSaveRejections` re-signs every case, so each is rejected by the rule it names, not by the fingerprint |

### Verification

- `TestRestoreRejectsInvalidState` "promotions changed": one place fewer on the first link, three rounds into season 1. It is rejected by the fingerprint, and it loads once re-signed, which is how every save loaded before this change.

## squad: free agents wait for the manager (done)

Delivers `squad--free-agent-pool` (from `balance`): the pool was empty at every window's open and best about 40 at the close, so the manager had nobody to sign.

### Decisions

- **The best free agents are held back.** At the contract-year end AI clubs still fill their rosters with `ai.Signings`, but `holdBack` then drops up to `freeAgentReserve` (4) of the best-rated signings that only fill a vacancy above a club's roster minimum (never the user club's, never one below the minimum). Those players stay in the pool and their clubs stay short.
- **The manager has the first half of the window.** In the window's daily runs an AI club signs a free agent for a vacancy only from `freeAgentGrace` (`WindowDays / 2`) after the opening. It can still bid for another club's player earlier. At the close `fillSquads` signs whatever is left, as before.
- **A manager who takes free agents leaves AI clubs short.** Supply equals demand, so a club stays short until the next contract year refills it. Squads never fall below their minimums.
- `ai.TransfersVersion` 5. `ai.ContractsVersion` stays 2: renewals, contract lengths and the `ai.Signings` allocation are unchanged.

### Changes

| Package | Change |
| --- | --- |
| `internal/app` | `holdBack` and `freeAgentReserve` in `contracts.go` (called by `contractYear`); `freeAgentGrace` in `transfers.go`, applied to the window's AI signings |
| `internal/ai` | `TransfersVersion` 5 |

### Verification

- `TestFreeAgentPoolAtTheWindowsOpen`: eight seasons, the pool holds at least 4 players at every open and the best is within 10 points of the average player.
- `TestContractYearRenewsReleasesAndSigns` expects exactly the reserve in the pool and matching vacancies. The squad-full tests (`TestSquadsStayLegalAndBalancedOverTheYears`, `TestManagerSquadGrowsToTheLimit`, `TestAIMarketKeepsSquadsFullForDecades`, `TestSquadsSurviveAHoardingManager`) now check after the window closes; the hoarding manager may leave up to the reserve missing.
- 10-seed, 30-year sweep: 4 free agents at every open, none at the close in years 1–20, 50 transfers a window, no negative balances.
- The web and play transfer tests name a new bidder for the listed player (Eldhaven United, offer 87).

## squad: injuries (done)

The first backlog item of the squad lane: matches can hurt players, and injured players do not play.

### Decisions

- **`medical` owns injuries.** A `Record` gains `DaysOut`: the recovery days left, zero when fit. Each daily recovery takes one off; the day that takes the last one makes the player fit and emits `PlayerRecovered`. Days, not instants, so the store imports no calendar.
- **Injuries are drawn from a match's exposure, not from the engine.** `simple` has no `Injuries` capability, so `ResolveRounds` rolls them after the outcome is checked (`medical.Store.Roll`, one stream per fixture: `random.Derive(seed, "medical/injury", medical.Version, fixture)`). Chance per player is `minutes * (InjuryBase + missing condition * InjuryFatigueStep)` ppm, so tired players are hurt more and rotation pays. Defaults: about 1.4% a match at full condition and 3.2% at 60. Layoffs: 60% minor (2–7 days), 30% moderate (8–28), 10% serious (29–120). About 70 injuries a season across four 8-team leagues. An injury does not stop the match being played, and there are no forced substitutions. When an engine reports injury incidents, `Roll` is replaced by them.
- **Availability is app's rule (`injuries.go`).** `availableSquad(team)` is the squad without the injured. The AI selects from it, `lineupInput` rejects an injured player (`ErrInvalidLineup`), and a carried-over lineup drops him (`MatchdayLineup.Dropped`) and refills his place.
- **A club can always field a team.** A rolled injury that would leave a club without a goalkeeper and ten outfield players fit is dropped. A squad already that short (a sale or a release does not look at injuries) fields its injured too, so `ResolveRounds` cannot fail for want of players.
- **Events and messages.** `PlayerInjured` (20: player, club, team, days) and `PlayerRecovered` (21; club and team zero for a free agent); inbox `KindInjured` (16, `Days`) and `KindRecovered` (17) for the managed team.
- `medical.Version` 3 (the world now has injuries), `storage.SchemaVersion` 19 (`medical.Record.DaysOut`, the new inbox field and events). The season goldens for seed 42 moved.

### Changes

| Package | Change |
| --- | --- |
| `internal/medical` | `Record.DaysOut`, `Injury`, `Store.Roll`, `Store.DaysOut`, `PlanExposure(exposures, injuries)`, `Plan.Injured`, `Plan.Recovered`; injury params; imports `core/random` |
| `internal/app` | `injuries.go` (`canField`, `availableSquad`, `injuryRolls`, `World.Injury`); `resolve.go` rolls and applies injuries and emits `PlayerInjured`; `continue.go`'s recovery emits `PlayerRecovered`; `lineup.go` uses `availableSquad`; `SquadPlayer.DaysOut`; journal fact checks |
| `internal/events`, `internal/inbox` | the two kinds and messages above (hub files, additive) |

### Verification

- `medical`: the roll is deterministic and bounded, the tired are hurt more, layoffs count down and end with the last recovery day, plans reject bad injuries.
- `TestInjuredPlayersAreNotSelected`, `TestInjuriesHealAndSurviveSaves`, `TestASquadTooHurtToFieldPlaysItsInjured`, `TestInjuriesOverSeasons` (two seeds, three seasons: no club left unable to field a team, everyone heals), `TestInjuriesAreDeterministic`; the journal and inbox tests count injuries and recoveries.
- The squad-full tests now allow one AI club to be one player short at the close (`assertAISquadsFullBut(t, w, 1)`). Changed results change promotion and money, and the trajectory of seeds 7 and 42 then hits a market gap that was there before: a club with no transfer budget loses the race for the last free agent at a position to lower-numbered clubs, a surplus player of another club stays listed but unaffordable, and the next player year refills the club with youth. It is on the backlog.

## data: nationalities (done)

Every club plays in a nation and every player has a nationality: the first of the richer identities in the architecture's world registry.

### Changes

| Package | Change |
| --- | --- |
| `internal/core/ids` | `NationID` (zero invalid) |
| `internal/registry` | `Nation{ID, Name}` with `Nations()` / `Nation(id)`; `Club.Nation`; `Player.Nationality`. `New` and `PlanPlayers` reject a duplicate or empty nation, a club of an unknown nation and a player of an unknown one (a zero nationality included) |
| `internal/content` | `Definitions.ForeignPct` (15) and `Youth.ForeignPct` (5), each 0..100 and validated. `Version` 8 |
| `internal/worldgen` | Nations get IDs by content order (`NationID(i)`, from 1). A player is of their club's nation, except `ForeignPct` percent of the time, when it is one of the other nations, equally likely. Two draws at the end of each player's stream (after the match attributes) and of each youth stream. `Youth` takes the joining club's nation. `Version` 7, `YouthVersion` 3 |
| `internal/storage` | `SchemaVersion` 20: `registry.Init` has nations and the new fields. Schema 19 saves are refused |
| `internal/app` | `ClubSummary.Nation` and `SquadPlayer.Nationality` (names); the player year passes the club's nation to `worldgen.Youth` (one argument in `lifecycle.go`); restore validates through `registry.New` |

### Decisions

- **Appended draws.** Names, attributes, contracts and birth dates are unchanged for every seed. Only the world fingerprint moves (the two app goldens and `goldenSeed42` are updated); the seed-42 season, second-league and cup goldens did not move.
- **Both draws are always made,** even with one nation or a 0% share, so the stream never depends on the number of nations. Adding a nation does change who is foreign among existing players.
- **Names are still global pools.** A player's name does not depend on their nationality yet. Per-nation name pools would move every name and so every lane's goldens; that is a separate, announced change (lane backlog).
- **Old saves are refused, not migrated:** a schema 19 registry has no nationalities, and restore must not generate them from the seed.

### Verification

- `TestEarlierDrawsUnchanged`: the seed-42 world without nations and nationalities, encoded as worldgen v6 did, hashes to the v6 golden, and without the match attributes too to the v5 golden.
- `TestNationalities` (worldgen): nations and club nations in content order, every nationality valid, the foreign share about `ForeignPct`, none at 0%, and every other draw the same with a different share. `TestYouth` covers the youth share and rejects an unknown home nation.
- `TestNationalities` (app): summaries show the names, youth players joining in a player year have valid nationalities and are mostly their club's, and a restored world keeps them. `TestRestoreRejectsInvalidState` rejects a player or club of an unknown nation.

## balance: AI/player rules audit (done)

Reviewed architecture, progress, balance measurements, all lane backlogs and handoffs against the latest pulled code (`80ded24`). [The audit](ai-manager-parity.md) records ten findings with code evidence, priorities, ownership and acceptance criteria; all six lane backlogs now include their part.

Present differences include AI-only vacancy youth, player-only free-agent grace/reservation, controller-dependent capacity validation, uneven consent revalidation, market-policy restrictions, renewal/response timing and missing AI in-match decisions. The planned mixed-engine setup and future scouting knowledge are preventive items, not claims of current engine or hidden-information advantages. Shared injury, condition, finance and match rules are recorded as already shared.

Filed focused handoffs for intake, admission checks, consent and decision stops. Accepted the five pending balance measurement notes into the backlog without claiming their sweeps complete. No football behavior changed; two misformatted map entries in `cmd/simulate/main_test.go` were normalized to clear a pre-existing formatting failure, with an informational ui note. Verification: `gofmt -l .` is empty, `go vet ./...` and `go test ./...` pass, and new audit/handoff file links resolve.

## ui: eight handoffs answered (done)

Both clients now show what the last four feature lanes added: injuries, the five new attributes, nationalities, second divisions with promotion and relegation, the AI sellers' refusals and the free-agent pool.

### Changes

| Area | Change |
| --- | --- |
| `internal/app/views.go` | Read-only `PromotionPlaces(league)` (top places that go up, bottom places that go down, from the pinned links) and `SeasonMove(ref, position)`. They only read `Promotions()` and `Table()` |
| Attributes | Squad, lineup and player pages show all eleven (`DRI HEA STR ACC PSN` after the old six), sortable in the web; a legend from one template/constant in each client. `cmd/play` lineup and player use the wider `ratings()` |
| Nationality | A `Nat` column in the squad, lineup, free-agent, transfer-list and market tables (sortable in the web), on the player page, the club's nation next to a club in the market and on the club pickers. The club chooser lists clubs by league (grouped in `cmd/play`, a sortable League column in the web) |
| Injuries | "out 12 d" beside condition on every page that shows condition; inbox kinds `KindInjured` and `KindRecovered` rendered; `cmd/simulate`'s squad shows them too. The lineup editors still list injured players: `SubmitLineup` refuses one and the error is shown (the "too few fit players" exception is `app`'s, not a client rule) |
| Promotion and relegation | Tables (both clients, and the web history page) mark the places (▲/▼ in the web, `up`/`down` in the terminal) with a legend, in the current table as "would go" and in a finished season's as "were"; the season-end inbox message says "promoted to the division above" or "relegated to the division below"; `cmd/simulate` marks its tables |
| Second divisions | `cmd/play` history sizes its name column from the names; only the first divisions say "the top four play in the Continental Cup" |
| Transfers | Market and list rows say why `BidRefusal` refuses a bid; from `TransferWindow().NeededClose` both clients say AI clubs sell only listed players and players they can spare |
| Free agents | The window notice (status, home, the free-agent page) points at the pool and says the best free agents may go to AI clubs from the middle of the window, without computing a date |

### Decisions

- **Marks come from `app`.** Clients only turn `PromotionPlaces` and a table's ranks into marks; nothing computes who goes up.
- **One stand-in.** "Only the first divisions send their top four to the cup" is derived from "a league with no promotion places". It is wrong as soon as a second division qualifies for a cup: `competitions--cup-qualifiers.md` asks for the qualifiers.
- **The exact instant** from which AI clubs may sign free agents is not shown: `squad--free-agent-reservation-instant.md` asks for it.
- **`cmd/play` `table`** still shows only the managed club's league (the web shows all four); listed in the lane backlog.

### Verification

Web and terminal tests drive the pages as a player does: `TestAttributesAndNationalities`, `TestInjuriesInTheBrowser`, `TestPromotionAndRelegationMarks`, `TestMarketRefusalsInTheBrowser` and `TestSeasonEndSaysRelegation` for the web; `TestNationalitiesAndAttributes`, `TestPromotionMarksAndHistory`, `TestInjuriesInTheTerminal` and `TestSeasonEndSaysRelegation` for `cmd/play`; `TestPromotionViews` for the new queries. `TestTableRowsMatchTheirHeaders` now covers the table, transfers and history pages too.

## ui: all league tables in the terminal (done)

Added a `tables` command to `cmd/play`; it displays every league standings table with the same promotion/relegation marks and managed-club marker as `table`. During the off-season it shows each league's final table from the preceding season. The web client already exposed all four leagues.

### Verification

- `TestTablesShowsEveryLeague` drives the terminal command, checks all four league names and headers, verifies the managed-club marker, and checks invalid arguments.
- `go test ./cmd/play` passes.
- Match report polish needs ordered match events to survive resolution and saves; filed [match--report-events.md](handoffs/match--report-events.md) for the match lane.

## ui: player comparison (done)

Added side-by-side comparison to the terminal (`compare ID ID`) and browser (`/compare`), with a link from each player profile. Both use the existing `PlayerProfile` query and show position, age, overall, all attributes, condition, weekly wage, asking price and wage demand. Wage and price fields show `n/a` when they do not apply to free agents or retired players. No suitability score is calculated.

### Verification

- `TestComparePlayersCommand` checks the terminal output, unknown players and invalid arguments.
- `TestComparePlayersPage` checks the browser comparison, profile link and unknown IDs.
- Targeted `go test ./cmd/play ./cmd/web` passes.

The next UI backlog item, durable player career history, needs the data archive. Filed blocking request [data--player-career-history.md](handoffs/data--player-career-history.md) for a save/load-safe app query.

## match: attributes, closer shootouts, report events (done)

Four handoffs delivered and three acknowledged.

### Changes

- **Match attributes.** `matches.Ratings` carries all eleven player attributes (Dribbling, Heading, Strength, Acceleration and Positioning are new), validated like the others; `World.candidate` copies them and the `enginetest` fixtures set them. `tick`'s tackle duel reads Dribbling instead of the mean of Passing and Pace (`tick.ModelVersion` 2). `simple` reads none of the new fields.
- **Closer shootouts** in both engines: a kick scores with 75% plus 0.15 points per effective rating point of taker Finishing above keeper Goalkeeping, clamped to 60–90%, instead of a ratio clamped to 50–93%. `simple.ModelVersion` 4, `tick.ModelVersion` 3.
- **Report events.** `MatchReport.Events` keeps every match event of every resolved fixture (goals, substitutions, mentality changes, period ends; a live match's replayed events first). They are checked against the goals when resolving and on restore, and cloned in and out. `storage.SchemaVersion` 21.
- `SubmitLineup`'s doc now states that injured players are rejected (squad's injury edits reviewed and kept).

### Decisions

- **Additive shootout rule.** A ratio of taker to keeper multiplies the stronger side's two advantages (better takers, better keeper). A small additive effect around 75% keeps shootouts close to a coin toss: in balance's sweep the stronger home side wins 55% (simple) / 54% (tick) at 65 v 55 and 61% / 62% at 70 v 50, down from 74/82 and 88/93. Penalties scored per shootout fell from about 9.5 to 8.3, matching real conversion of 70–80%.
- **Every engine bump reshuffles every career,** because the engine version seeds each fixture's stream. The seed-42 goldens moved, and the client tests that name a seed-42 story were re-pinned to equivalent scenarios ([note to ui](handoffs/ui--seed-stories-moved-shootouts.md)).
- **Events are stored, not re-simulated.** A report could be rebuilt by replaying the match, but that ties old reports to the engine version that played them. Storing them costs about 190 KB per season in a save (+15%).
- **tick fixtures give defenders lower Dribbling,** as the generated career does. That raises `tick`'s goals (60 v 60: 3.26, from 2.92), which folds into the accepted goals-by-level note.

### Verification

- `TestCandidateCopiesEveryAttribute` pins one `matches.Ratings` field per `players.Attribute`, copied in order; the contract suite rejects a zero Dribbling and a Positioning of 101.
- `TestShootoutsStayClose` (simple, always on) bounds the stronger side at 62% of 65 v 55 shootouts; `ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v` gives the table above.
- `TestMatchReportKeepsEvents` plays a live match with a half-time substitution and compares the report's events with the live view, through the command result, `MatchReport` and a save, and checks for aliasing. `TestRestoreRejectsInvalidReportEvents` covers missing, reordered, unknown, mismatched and out-of-time events.

### Handoffs

- Delivered: `match--match-attributes-delivered`, `match--shootout-favourite`, `match--report-events`. Acknowledged: `match--command-record-hub`, `match--goldens-32-clubs`, `match--injuries-eligibility`.
- Accepted: `match--tick-mentality`, `match--tick-goals-by-level`.
- Filed: [ui--report-events.md](handoffs/ui--report-events.md), [ui--seed-stories-moved-shootouts.md](handoffs/ui--seed-stories-moved-shootouts.md).

## competitions: cup qualifiers query (done)

`World.CupQualifiers(league)` lists the cups a league qualifies teams for (`CupQualifier{Cup, Name, Places}`, in cup ID order), so the clients can stop guessing from `PromotionPlaces` which tables send teams to the Continental Cup.

### Verification

- `TestCupQualifiers`: the first divisions each send four teams and the second divisions, the cup itself and an unknown ID none; a returned slice is a copy; the first edition's entrants are exactly the qualifying places of every league.

### Handoffs

- Delivered: `competitions--cup-qualifiers`. Filed: [ui--cup-qualifiers.md](handoffs/ui--cup-qualifiers.md).
- Accepted: `competitions--shared-manager-decision-stops` (the P2 decision-opportunities backlog item).

## competitions: seasons follow the civil calendar (done)

A league's season N kicks off on its first kickoff's weekday and time, in the week nearest that kickoff's (N−1)th anniversary: `competitions.SeasonKickoff(cal, first, season)`. Seasons used to start 52 weeks apart, a day or two earlier each year, until after about 28 years a first round fell inside the transfer window.

### Changes

| Package | Change |
| --- | --- |
| `internal/competitions` | `SeasonKickoff`; `ScheduleVersion` 2, now also covering the season calendar; the pairing draw keeps its stream key (`pairingDraw` = 1) |
| `internal/app` | `endSeasons` and `validateSeasons` use `SeasonKickoff` instead of `SeasonInterval` |
| `cmd/simulate` | test expects the `schedule=v2` header |

### Decisions

- **Nearest week, not the date.** Seasons keep their weekday and kickoff time and move at most three days either side of the anniversary, so consecutive seasons are 52 or 53 weeks apart. A 29 February anniversary is 1 March in common years.
- **No content change.** The rule reads only `FirstKickoff`, so `LeagueVersion`, content fingerprints and the world fingerprint are unchanged. `SeasonInterval` no longer sets the calendar; whether to drop it is `data`'s call ([note](handoffs/data--season-interval-unused.md)).
- **Pairings kept.** `ScheduleVersion` also keyed the fixture draw's stream, so a plain bump would have reshuffled every career. The draw is keyed by `pairingDraw` (the last version that changed pairings), so the seed-42 schedule golden and every season 1–3 result are unchanged. The bump is still needed: it makes saves from the old calendar (`Schedule` is a must-match version) fail as incompatible instead of as invalid.
- For the default leagues seasons 2 and 3 fall where they did (8 Aug 2026, 7 Aug 2027); season 4 moves from 5 to 12 August 2028, and later seasons differ from before.

### Verification

- `TestSeasonKickoffFollowsTheAnniversary`: 400 seasons from three first kickoffs (the default Saturday, 31 December, a leap day) keep weekday and time, stay within three days of the anniversary (recomputed with `time.Date`) and are 52 or 53 weeks apart. `TestSeasonKickoffDefaultDates` pins seasons 1–4 and 29 and rejects season 0, an invalid date, an out-of-range season and a zero calendar.
- `TestSeasonsStayInsideTheContractYear`: for 100 seasons of every default league, the first round is after the transfer window closes and the cup final before the next player year.
- Deliberate-bug check: validating each season against its kickoff plus one minute failed the consecutive-season and save tests.
- Also rewrapped a comment in `continue.go` (steward review of squad's injury change; no behavior change).

## data: player careers (done)

Every player's clubs since the career began: how and when he joined each (with the fee for a transfer) and how and when he left. Delivers `ui`'s blocking request for profile history (`data--player-career-history`).

### Changes

| Package | Change |
| --- | --- |
| `internal/careers` (new) | A read model like the inbox: `Store` with a consumer offset, `Start(at, employed)` for a new world, `New(Snapshot)`, `Apply(events)` (all-or-nothing), `Career(player)`, `Careers()`, `Initial()`. A `Spell` is `{Club, From, Joined, Fee, Until, Left}`; `Joined` is at start, youth, free or transfer, `Left` is not yet, transfer, expired, released or retired (durable values) |
| `internal/app` | `World.careers`, started from the generated employment at load and fed by `publish` with the inbox. Query `PlayerCareer(id) ([]CareerSpell, bool)` (spells with the club's name). `validateCareers` in `Validate` |
| `internal/storage` | `SchemaVersion` 22: `WorldSnapshot.Careers`. Schema 21 saves are refused |
| `internal/app/boundaries_test.go` | `careers` may import core and `events`; `app`, `cmd/play` and `cmd/web` may import `careers` |

### Decisions

- **Built from events, not a second owner.** Employment still owns where a player is now. The events that already describe every employment change (youth joined, signed, transfer completed, contract expired, released, retired) carry every fact a spell needs, so no event changed and no workflow gained a write. The careers are saved because the journal is trimmed (about three to four years), like the inbox.
- **Validation ties it to employment.** A player has a current spell exactly when he is employed, at his employer. So a future workflow that moves a player without its event fails `Validate` (and every save/load test). While the journal is complete, the careers must also equal a rebuild from their start and the journal; a spell ending in retirement needs a retired player.
- **Spells before the career have no date.** Players on the books at the start get a `JoinedAtStart` spell from the career's start: clients should say "before" that date, not "joined". Worldgen draws no join dates, so no generated output changed (no version bump, no golden moved).
- **Clubs only.** Teams, appearances and goals are not kept. Appearances and goals need `match`'s agreement first (lane backlog).
- **Squad's tests kept honest.** Four tests moved players straight in `employment` with no event, a state no workflow reaches. A shared test helper, `commitMoves`, now commits such moves with `ContractExpired`/`PlayerSigned` events. Three rejection cases refresh their expected revision, and `TestManagerBuysAPlayer` skips the released player's inbox message ([note](handoffs/squad--careers-follow-employment-events.md)).

### Verification

- `careers`: a story of sales both ways, an expiry and free signing, a release, a free agent's and a youth player's retirement builds the expected spells; the snapshot restores and `Initial()` plus the journal rebuilds it; redelivery is a no-op and queries copy. Seven contradicting events (wrong seller, a signing while employed, a youth ID reused, signing after retiring, …) are refused with nothing changed, as are gaps and invalid events; `New` refuses 18 malformed snapshots.
- `TestCareersRecordReleasesAndTransfers`: a real release and a transfer with fee, checked before and after save/load and again after the journal is trimmed to five events.
- `TestRestoreRejectsInvalidCareers`: an offset behind the journal, a missing career, a wrong current club, an edited fee, a forgotten transfer and an unknown club.
- A seed-42 run of two seasons with save/load in between: 665 careers, 810 spells (104 transfers, 41 free signings, 25 youth), offset 1515 with the journal trimmed.

## data: `SeasonInterval` dropped (done)

Answers `competitions`' note `data--season-interval-unused`. Since seasons follow the civil calendar (`competitions.SeasonKickoff`), `League.SeasonInterval` no longer set anything, and its comments described the old 52-week rule.

### Changes

| Package | Change |
| --- | --- |
| `internal/content` | `League.SeasonInterval` removed. `MaxSeasonSpan` (51 weeks): `League.Validate` requires the last kickoff within it of the first (and a round interval no longer than it, which keeps the product from overflowing). The `League` comment points at `SeasonKickoff`. `LeagueVersion` 5 |
| `internal/app` | `checkPromotions` compares linked leagues' `FirstKickoff` and `RoundInterval` only |
| `internal/storage` | `SchemaVersion` 23 (pinned league definitions lost a field). Schema 22 saves are refused |

### Decisions

- **Dropped, not kept as a bound.** A field nothing reads for its meaning invites someone to change it and expect seasons to move. The bound it stood for is now a constant beside the rule it follows: seasons start within three days of the anniversary, so at least 359 days apart, and a 357-day season ends before the next one.
- **No generated or scheduled output changed:** the default leagues run 13 weeks, and no seed-dependent golden moved.

### Verification

`TestLeagueValidation` rejects a season a minute too long, a round interval beyond a season and one that would overflow, and accepts exactly `MaxSeasonSpan`. `TestRestoreRejectsInvalidSeasonState` still refuses an edited league timing (its round interval).

## Next tasks

Work is split into parallel lanes (see [AGENTS.md](../AGENTS.md)). Each lane keeps its current task and backlog in its own doc: [ui](lanes/ui.md), [match](lanes/match.md), [competitions](lanes/competitions.md), [squad](lanes/squad.md), [data](lanes/data.md), [balance](lanes/balance.md). Requests between lanes are in [handoffs/](handoffs/README.md).

From now on, a lane records completed work in a section titled `## <lane>: <feature> (done)`, placed just above this one, with the same subsections as the milestones above. Milestone numbers end at 16 because parallel lanes would claim the same number. If two lanes append at the same time, keep both sections in merge order.

## UI: cup qualifiers and match timelines (done)

### Changes

League tables in both clients now use `World.CupQualifiers` to say which places enter each cup; the web and terminal no longer infer qualification from promotion places or hard-code a cup name. Completed match reports show stored match events in order, including goals, substitutions, mentality changes, half time and full time. The web timeline reads from `World.MatchReport`, so it survives save/load.

### Verification

`cmd/play` and `cmd/web` tests check qualification labels and the report timeline; `gofmt -l .`, `go vet ./...` and `go test ./...` pass.

## UI: player club history (done)

Player profiles in the terminal and browser now show a player's club spells from the saved career history: named clubs, UTC start and end dates, how each spell began (including transfer fees), and how it ended. A current spell is marked as present; players already at a club when the career began are shown as there before the career start date.

### Changes

- `cmd/play`: the `player ID` profile prints the career below the player's current status.
- `cmd/web`: the player profile has a club history table with links to each club's squad.
- Both clients use `World.PlayerCareer`; neither reconstructs past clubs from current employment or the event journal. Closed `ui--player-career-history.md`.

### Verification

- Terminal and web profile tests assert the career history section, including the initial spell's before-date wording and current-club status.

## UI: compare squad players by name (done)

The terminal and browser compare screens let the manager choose players from their own squad by name. The terminal accepts quoted full names (for example, `compare "Callum Ibsen" 1`); the browser suggests squad names while still accepting IDs for players at other clubs. Ambiguous first names ask for a full name or ID.

### Changes

- `cmd/play`: player comparison resolves a squad player's full name or unique first name and accepts quoted names.
- `cmd/web`: compare inputs suggest the managed squad through a native datalist and resolve names to app-owned squad records.
- Both clients keep direct player ID comparisons for all players.

### Verification

- Client interaction tests cover quoted terminal full-name selection and browser name lookup alongside existing ID comparisons.

## squad: consent at completion, the free-agent date (done)

A player's consent to a transfer is now a shared rule checked when the transfer completes, whoever the seller (audit PAR-04). `TransferWindow` also says when AI clubs may start signing free agents.

| Package | Change | Version |
| --- | --- | --- |
| `internal/transfers` | `StatusRefused` (6): accepted, but the player refused to join the buyer at completion | – |
| `internal/ai` | `AcceptBid` and `Sale` no longer judge `Joins`: the seller's price, settling-in and replacement preferences only | `ai.TransfersVersion` 6 |
| `internal/app` | `market.complete` checks `ai.Joins` against staged squad averages (`market.average`, `market.joins`) and fails with `ErrPlayerRefuses`; the offer closes as refused (`failedStatus`) in `transferRun` and `RespondToOffer`. AI candidates and `BidRefusal` use the same staged averages. `TransferWindow.FreeAgentsOpen` | – |
| `internal/events` | `OfferClosed.Outcome` may be 6 | – |

### Decisions

- **Consent is conditional until completion, for both controllers.** Completion happens when a seller accepts: at the AI seller's run, or when the manager accepts before the deadline. The squads can change in between, so consent is judged then, not when the bid is made. It is not binding at the bid for either.
- **Judged against the staged squads.** The market keeps each club's overall sum as it stages completions and signings. A club that completes a purchase earlier in the same run can make one of its players a star who then refuses a weaker buyer. The seed-42 first window shows this: the manager's bid for club 11's forward is refused after club 11 buys a player at the same run.
- **Refused, not rejected or collapsed.** The seller did accept, so "rejected" would misreport it; "collapsed" is kept for a buyer or seller who can no longer complete. A manager's bid for a star of a stronger club now ends refused, where it was rejected before. `BidRefusal` still predicts it (`RefusalStar`).
- **No schema bump.** No field changed. A save holding outcome 6 carries `ai.TransfersVersion` 6, which older builds refuse to load.
- **`FreeAgentsOpen`** is `Opens + freeAgentGrace`, the instant `aiActions` already used. It is exposed so that clients never compute it. PAR-02 may replace the grace; the field would then carry the new rule.

### Verification

- `TestConsentAtCompletion`: the manager accepts an AI bid after the buyer's squad has weakened below his own; and an AI club answers the manager's bid after the manager's squad has weakened below the seller's. Both end refused, move nobody and no money, and emit `OfferClosed` 6 and its inbox message. The manager's answer retries to the same result and survives a save.
- `TestConsentFollowsEarlierCompletions`: a same-run completion changes consent. `TestRestoreRejectsInvalidTransfers` restores a snapshot holding a refused offer.
- `TestFreeAgentPoolAtTheWindowsOpen`: over eight AI-only years the pool is untouched until `FreeAgentsOpen`, and AI clubs sign from it later.
- Deliberate-bug checks: without the consent check in `complete`, three tests fail; with a quarter of the grace, AI signings before `FreeAgentsOpen` are caught.
- Goldens updated deliberately: the seed-42 season, second-league and cup hashes. Seed 42's first window now completes 49 offers with 3 refusals (was 54 and 1 collapsed). The pinned client tests were updated for the moved careers.

### Limitations

- `TestSquadsStayLegalAndBalancedOverTheYears` now tolerates two missing AI players after a window (was one): the thin-market item, reproduced on a new path (seed 7, club 3 managed, year 7, club 14).
## data: refused offers reviewed (done)

Reviewed squad's widening of `OfferClosed.Outcome` to 3–6 in `internal/events` (`transfers.StatusRefused`). Accepted as is: no new kind, no snapshot field, so no `storage.SchemaVersion` bump; `journal` already checks the closure's status against the offer (`transfers.go` fact check), `inbox` passes the outcome through and `careers` ignores `OfferClosed`.

- `TestValidate` now accepts every outcome 3–6, and `TestTransferMessages` covers an outcome-6 message for the buyer.

## match: lineup eligibility query (done)

Delivered `ui`'s note `match--lineup-availability`: clients can filter the lineup editors by the app's selection rule instead of inferring it from injuries.

### Changes

- `World.SquadEligibility(fixture)` returns every player of the user club's squad for a pending user fixture with a durable `Eligibility`: `EligibleFit` (1), `EligibleInjured` (2: injured, but selectable because the fit players cannot field a legal eleven) or `IneligibleInjured` (3), and his days out. `Eligibility.Selectable()` says whether he may be named.
- `lineupInput`, which `SubmitLineup` and `validateSelections` use, checks players through the same `eligibility` helper, so preview and submission cannot disagree. A rejected injured player's message now says for how many more days.

### Decisions

- **Keyed by fixture, though the rule doesn't depend on it yet.** Suspensions per competition, when they arrive, will. The saved team plan outside matchday (accepted note `match--lineup-editing-outside-matchday`) will need a fixture-less variant.
- **No schema or version change.** The query is derived; no seeded output moved.

### Verification

- `TestSquadEligibilityAgreesWithSubmission`: a fit squad lists everyone fit in `Squad` order; a player injured after the preview is listed ineligible with his days and rejected at submission with the reason; the AI suggestion names only selectable players; unknown and played fixtures are refused.
- `TestSquadEligibilityShowsTheEmergencyRule`: with every goalkeeper injured, they are `EligibleInjured` and a lineup naming one is accepted.

### Handoffs

- Delivered: `match--lineup-availability`. Accepted, with a proposed shape: `match--lineup-editing-outside-matchday` (next after the tick mentality note).
- Filed: [ui--lineup-eligibility.md](handoffs/ui--lineup-eligibility.md).

## UI: player refusal and free-agent date (done)

The terminal and browser distinguish a player's refusal from a transfer that could not complete. Transfer inbox messages say the player refused to join, and accepting a refused bid names the player and buyer. Both free-agent views use `TransferWindow.FreeAgentsOpen` and the UTC game calendar to tell the manager when AI clubs may sign.

### Changes

- `cmd/play` and `cmd/web` show the refusal reason in bid answers and inbox messages.
- Both clients display the app-provided free-agent opening date instead of saying “the middle of the window.”
- Closed `ui--player-refuses-at-completion.md`.

### Verification

- Client tests cover refusal wording and the date in both clients.

## UI: lineup availability filter (done)

Both lineup editors use `World.SquadEligibility` to show which squad players can be named for the pending fixture. Managers can filter to selectable players, and both clients explain when the emergency rule makes injured players selectable because there are not enough fit players for a legal eleven.

### Changes

- `cmd/play`: `lineup available` hides players the app marks unselectable and prints the eligibility for the squad.
- `cmd/web`: the lineup page has an accessible GET filter and an availability column. Hidden rows keep their current form values when the manager saves.
- Closed `ui--lineup-eligibility.md`.

### Verification

- Terminal interaction tests cover the available-only lineup command. Browser tests verify an injured, unselectable player is hidden while his injury days remain explained in the full editor.

## data: per-nation name pools (done)

A player's names now fit their nationality: each nation has its own first and last name pools, and generated and youth players draw from the pools of their nationality (not their club's nation), so a foreign player stands out by name. Westmark's pools are Anglo-Celtic, Eastmarch's Nordic and Low German; the two share no name.

### Changes

| Package | Change |
| --- | --- |
| `internal/content` | `Nation.FirstNames` and `Nation.LastNames` (32 and 40 names each) replace the global `Definitions.FirstNames`/`LastNames`. `Validate` requires each pool to be non-empty, without an empty or repeated name, and still at least one club suffix. `Clone` copies the pools. `content.Version` 9 |
| `internal/worldgen` | `drawNames` draws the names last in each player's and youth player's stream, after the nationality. `skipNames` keeps the two draws that chose the names before, unused, so every other draw is unchanged. `worldgen.Version` 8, `YouthVersion` 4; `streamVersion` and `youthStreamVersion` stay |
| `internal/storage` | `SchemaVersion` 24 (the saved `Content` moved the name pools into the nations). Schema 23 saves are refused |
| world fingerprint | Seed 42 is now `f40190ce…`, in `worldgen`, `app/world_test.go` and `app/resolve_test.go` |
| `cmd/play`, `cmd/web` tests | Pinned player names updated, player by player (same IDs, same numbers) |

### Decisions

- **Only names moved.** Skipping the old draws, rather than bumping `streamVersion`, keeps every attribute, contract, birth date and nationality, so no season, cup, transfer or balance golden changed: only tests that print names did. Each skipped draw was one `IntN`, which takes one value from the stream except on a rejection (a chance under 2^-59 per player); `TestEarlierDrawsUnchanged` checks seed 42.
- **Names follow the nationality.** A Westmark club's Eastmarch player has an Eastmarch name, and so does a foreign youth player.
- **Fewer repeated names.** Each nation's pools give 1,280 combinations where the shared pools gave 768. Seed 42 now has 74 names borne by more than one player (80 extra players), down from 158 (214). Names are still not unique, and clients that accept a name must keep handling ambiguity.
- **Historic goldens rehashed without names.** The v5 and v6 worlds can no longer be rebuilt with their names, so `canonicalAs` hashes the snapshot as generator v7, v6 or v5 encoded it, without the names, against goldens taken from the v7 code before the change.

### Verification

- `TestEarlierDrawsUnchanged`: seed 42 without names is exactly the v7 world, and without nationalities and match attributes the v6 and v5 worlds.
- `TestNationalities` and `TestYouth`: every generated and youth player is named from their nationality's pools; the pinned youth players keep their v3 birth dates, all eleven attributes and nationalities. A foreign share changes nothing but the foreign players' nationalities and names.
- `TestValidateRejectsBrokenDefinitions`: a missing first or last name pool, an empty or repeated name and no club suffixes are refused; `Clone` does not share the pools.
- Every season, second-league and cup golden in `internal/app` is unchanged.

### UI handoff

Rebased the UI lane onto this commit. The pinned player names in both client test suites match the new generated names, and name-based comparison continues to handle duplicate names. `gofmt -l .`, `go vet ./...` and `go test ./...` pass; closed `ui--per-nation-names.md`.

## squad: one youth intake rule (done)

The player year's youth intake no longer depends on who manages a club (audit PAR-01). Every club replaces its retirees one for one. Its academy then fills each position below the roster count, in roster order, while the squad is below the squad limit. Before this change only AI clubs had their vacancies filled.

| Package | Change | Version |
| --- | --- | --- |
| `internal/app` | `playerYear`: the vacancy intake runs for every club, bounded by `SquadLimit`; the `c.ID != w.userClub` branch is removed | – |

### Decisions

- **Fill every club rather than no club.** Removing the top-up for everyone would starve the free-agent pool. Retirees are replaced one for one and free agents retire unreplaced, so nothing else refills the pool. The squad-limit section's deliberate-bug check showed it: without vacancy youth, an AI club ended a contract year with 5 midfielders. So the manager now gets the intake as well. This reverses the earlier "the user club gets no vacancy youth" decision (squad-limit section above). A manager who wants a position short keeps the squad at the limit, or releases the youth player.
- **Bounded by the squad limit.** AI squads never reach it, so AI-only careers are unchanged bit for bit and no golden moved. A managed club at 25 players with a positional vacancy gets no extra youth.
- **No version or schema bump.** `worldgen.Youth` is called the same way, and no saved field changed. Managed careers whose club ends a season short now draw extra youth, which shifts later player IDs. No golden covers managed multi-year careers. `data` confirmed: youth generation, content and the `YouthJoined` event keep their meaning, so no generation, content or event version moves; the `content.Quota` comment now names the academy target.

### Verification

- `TestYouthIntakeDoesNotDependOnTheController`: one seed-42 managed snapshot on the eve of the player year, with club 3 short one defender and one forward. It is restored once as managed and once with no user club (`withController` rebuilds the inbox for the controller). The registry, profiles, employment, condition, ledgers and journal after the player year are identical. Club 3 gets its retirees' replacements plus two vacancy youth and is back at the roster count.
- `TestYouthIntakeStopsAtTheSquadLimit`: club 3 at 25 players with a goalkeeper vacancy gets only its retirees' replacements, identically for both controllers.
- Deliberate-bug check: both tests fail against the previous `playerYear` ("the player year differs when club 3 is managed").
- `TestSquadsStayLegalAndBalancedOverTheYears` (AI-only and managed, fifteen years) and the rest of the suite pass unchanged.

## squad: shared signing capacity (done)

Delivered the accepted PAR-03 handoff. All signings use one app-level admission check against the staged squad counts: a signing is allowed at any rostered position while total squad size remains below `SquadLimit`. Position targets and surplus preferences stay in AI recruitment policy. The check covers human free-agent signings, completed transfers, AI window signings, contract-year recruitment and player-year youth intake. Youth replacement checks subtract staged retirements before admitting replacements.

### Verification

- `TestSharedAdmissionAllowsSurplusForHumanAndAI`: both actors may add a player to a 21-player squad that already has a surplus position.
- `TestSharedAdmissionRejectsFullSquadWithPositionVacancy`: both actors reject a signing at 25 players despite a goalkeeper vacancy; staged transfer completion rejects it without staging employment or fees.
- `gofmt -l .`, `go vet ./...` and `go test ./...` pass.

## match: tick mentality is a trade-off (done)

Delivered `balance`'s note `match--tick-mentality`. In `tick`, attacking no longer wins 20 points more for free, and defensive no longer turns a match into a shoot-out. `tick.ModelVersion` 4.

### Changes

- **The cause was a cliff, not a mentality rule.** Both sides' lines follow the ball by the same fraction, so every gap between an attacker's spot and a defender's spot was a fixed constant. Marking was all-or-nothing within `MarkRadius` (the nearest free opponent, in slot order, then stand goal-side of him), and a marked player is never a pass target. Moving a line by 2.5 m flipped whole lines between marked and free. At v3, `MarkRadius` 13 m gave 5.4 goals a match and 14 m gave 3.7. An in-possession shift of −2.5 m gave 5.2 goals, and −3 m gave 1.9. Mentality's ±2.5 m shift landed on these cliffs.
- **Continuous shape.** (1) Players of the side in possession drift around their spots, a new offset of up to 2.5 m every 5–10 s (`DriftDepth`, `DriftWidth`, `DriftTicks`). (2) Marking pairs defenders and opponents nearest first. A defender goes fully goal-side only within `TightMarkRadius` (8 m) of his spot. Farther out he holds a point partway, fading out at `MarkRadius` (18 m, pull on squared distance). Goals are now smooth and monotonic in every line depth.
- **Mentality moves two lines.** `MentalityDepth` is split into `MentalityDefendDepth` (−80 / 0 / +100 cm) and `MentalityAttackDepth` (−40 / 0 / +30 cm). Measured per metre and per side, a higher in-possession line scores more at no cost, and a deeper out-of-possession line concedes less at little cost. Each lever alone is a free lunch, so a mentality moves both. Attacking also no longer brings a second high presser (`Pressers` is 1 for every mentality): that alone was worth about 13 points of win rate.
- **Base lines recalibrated** to 2.8 goals between equal 60-rated teams (DF / MF / FW at 18.4 / 34.4 / 46.4 m out of possession, 37.7 / 57.7 / 76.7 m in it).
- `simulate` in `model_test.go` plays in parallel, so the package's tests take about 14 s instead of 34 s. New validation: `TightMarkRadius < MarkRadius`, `DriftTicks > 0`.

### Results

Balance's sweep, `ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v`, 3,000 matches a row, 60 v 60:

| Home v away | Goals | Home–away goals | H / D / A % | v3 goals | v3 H / D / A % |
| --- | --- | --- | --- | --- | --- |
| balanced v balanced | 2.80 | 1.52–1.28 | 42.8 / 25.3 / 31.9 | 3.28 | 43.2 / 23.9 / 32.9 |
| attacking v balanced | 3.62 | 1.94–1.69 | 44.2 / 23.0 / 32.8 | 4.17 | 69.8 / 15.0 / 15.2 |
| balanced v attacking | 3.55 | 1.95–1.60 | 45.3 / 22.1 / 32.6 | 4.08 | 22.8 / 18.6 / 58.6 |
| defensive v balanced | 1.97 | 1.08–0.88 | 40.1 / 30.2 / 29.7 | 5.15 | 42.3 / 18.9 / 38.9 |
| balanced v defensive | 1.98 | 1.09–0.89 | 39.8 / 30.9 / 29.3 | 5.22 | 52.0 / 16.4 / 31.6 |
| defensive v defensive | 1.52 | 0.82–0.69 | 35.4 / 37.1 / 27.5 | 1.74 | 37.4 / 33.8 / 28.8 |
| attacking v attacking | 4.25 | 2.27–1.98 | 45.5 / 19.9 / 34.6 | 4.52 | 47.2 / 18.8 / 34.0 |

(v3 figures are from my tuning harness on the same seeds and fixtures.) Every row meets the note's criteria. With a defensive side, goals fall below balanced, and with an attacking side they rise above it. A defensive side concedes 0.88–0.89 instead of 1.28–1.52. Attacking changes the attacking side's win rate by +1.4 points at home and +0.7 away. Points per 100 matches are within noise of each other: 150–155 at home and 116–121 away for every mentality.

### Decisions

- **Fix the cliff, then tune.** Tuning mentality offsets on the v3 model would have fitted it to one lucky configuration. With a continuous response, the same levers keep their meaning when ratings, formations or later rules change.
- **Drift only in possession.** Drifting defenders opened holes (5–8 goals a match whatever the marking); attackers looking for space is the football reason for it anyway.
- **Tighter marking concedes more.** A marker dragged far out of the block leaves room behind him, so the radii are a balance between shape and pressure, not "more is safer".
- **Slower:** 26 ms a match on one core, from 20 ms (the marking pairs). Roadmap phase 5 owns speed.
- **Goals by level got worse:** 40 v 40 scores 1.84 (v3 2.35) and 80 v 80 scores 4.46, and 70 v 50 still ends 95% home wins with 4.5 goals. That is the next tick note (`match--tick-goals-by-level`); its figures are updated there.

### Verification

- `TestMentalityTradeOff` (always on, 1,800 matches in about 3 s): against a balanced side, attacking scores and concedes more with home wins within 12 points, and defensive scores and concedes less and draws more.
- `TestModelTrends`, `TestConditionMatters` and the contract suite hold. The golden hash was re-pinned for v4. No career match uses `tick` yet, so no other golden moved.

### Handoffs

- Delivered: `match--tick-mentality`.
- Filed: [balance--tick-mentality-delivered.md](handoffs/balance--tick-mentality-delivered.md) to refresh `docs/balance.md`'s tick tables.

## data: save fixtures per schema version (done)

Old saves are now tested, not just promised. `internal/storage/testdata` keeps, per schema version, a gzipped save file (`schema-N.json.gz`) and the payload's JSON shape (`schema-N.shape`: every field path with its kind and tag options). Both are written once, by the build that introduces the version, and never rewritten.

| Package | Change | Version |
| --- | --- | --- |
| `internal/storage` | `TestSaveFixtures` policy test; the `-fixture` flag writes the current schema's fixture and shape if they are missing; `testdata/schema-24.*` | – |

### Decisions

- **Loads or is refused explicitly.** A fixture of an older schema must be refused with `ErrUnsupportedSave` (no migrations exist; a migration would make it load). The current schema's fixture must load, or be refused with `ErrIncompatibleSave` when a simulation version moved since it was written. Any other error means a rule change made existing saves invalid, and the change must bump `storage.SchemaVersion` or migrate.
- **Two schema guards.** The shape file catches a field added, removed, renamed or retyped anywhere in the snapshot types, even where the fixture holds no value (for example `Transfers.Listings`, empty once the window closes). The current fixture must also decode and re-encode to the same bytes.
- **A rich fixture.** Seed 42, club 3 managed: a release, a signing, a renewal, a bid, two listings, an inbox read, three answered AI bids, submitted lineups through the first season and its end, the contract and player years, into the second window, then a live match stopped at minute 30 after a change of mentality. Every command list, `Live`, past seasons, the cup, the payload records, the journal and both read models hold data. It is 130 KB gzipped (2.2 MB of JSON).
- **Frozen, not regenerated.** `-fixture` refuses to overwrite a fixture, because regenerating it would hide the drift it exists to catch. A lane that renumbers its schema version on a rebase drops its own fixture and writes it again (AGENTS.md).

### Verification

- Deliberate-bug checks: a field added to `transfers.Listing` fails the shape check; a top-level snapshot field fails both guards (the excerpt shows `"Extra": null`); a new Restore invariant the fixture breaks fails ("no longer loads"); a moved `ai.TransfersVersion` passes, logged as an explicit refusal; bumping `SchemaVersion` to 25 fails until `-fixture` writes schema 25, after which schema 24 is refused with `ErrUnsupportedSave`.
- Writing the fixture twice gives identical bytes.

## match: a saved team plan outside matchday (done)

Delivered `ui`'s note `match--lineup-editing-outside-matchday`. The manager can save a team plan at any time, matchday or not: starters with roles, a bench and tactics, with no fixture. Every user match without a submitted lineup plays the plan, fitted to that match. `storage.SchemaVersion` 25. No seeded output changes, so no version or golden moved.

### Changes

- **`selection`** keeps one `Plan{Team, Lineup}` per team beside the fixture entries. `Store.SetPlan`, `Plan` and `Plans`; the constructor takes a `selection.Snapshot{Entries, Plans}`.
- **Query** `World.TeamPlan() (TeamPlan, error)`: the plan (`Saved`), or when none is saved a starting point to edit (the lineup the club last fielded or had submitted, fitted to the squad, else the AI's selection, under the rules of the club's current league). `Unavailable` lists the plan's players who could not be named today (left the squad, or injured and not selectable), and `Squad` is the whole squad with `LineupEligibility`. `ErrNoUserClub` without a user club.
- **Command** `SetTeamPlan{ID, ExpectedRevision, Lineup}` → `TeamPlanSaved{Command, Revision, Team}`, with the usual retry rules. The lineup needs a valid shape and only players of the user club's squad; injured players may be named and the bench has no competition limit. Refused with `ErrMatchInProgress` while the manager's match is live. It emits the new event `events.KindTeamPlanSaved` (22), which no read model shows.
- **Order at a user fixture** (`MatchdayLineup`, `ResolveRounds`, `PlayMatch`): submitted for the fixture, else the plan (new `LineupSource` value `LineupFromPlan` = 4), else carried over from the last match, else the AI. The plan and a carried-over lineup are fitted the same way (`fitLineup`, factored out of the carry-over): unavailable players are dropped and reported in `Dropped`, their starting places refilled by `ai.RefillLineup`, and the bench cut to the competition's limit. The fitted lineup is stored for the fixture once it is played, as a carried-over one is, so `SelectedByManager` always has a stored lineup behind it.
- **Saves:** `WorldSnapshot.TeamPlans` and `TeamPlanCommands`, with the save fixture `internal/storage/testdata/schema-25.*`. The fixture career (`data`'s `fixtures_test.go`) now also saves a team plan, so it keeps using every command kind. Restore validates each plan's shape, that it is the user team's, and that it names registered players. Players who have since left are allowed: the plan is a preference, and matches leave them out.

### Decisions

- **The plan beats the carry-over.** A plan keeps its players while they are away: an injured starter misses the matches he cannot play and returns to his place when fit. A carried-over lineup replaces him for good. A lineup submitted for one fixture is a one-off, so the next fixture goes back to the plan.
- **Validate for shape when saved, fit at kickoff.** Squad membership is checked when the plan is saved, but not injuries or a competition's bench limit, since a league and a cup may allow different benches. Nothing is repaired in the stored plan.
- **Refused while live**, because a live match is replayed from world state and its lineup must not change under it.
- **No way to clear a plan yet.** Going back to the carry-over or to the AI belongs with the backlog item "Delegate lineups to the assistant again".

### Verification

- `TestTeamPlanIsPlayedWithoutAMatchday`: a plan saved before the first matchday is played in five rounds exactly as if it had been submitted each time, survives a save, and stays unchanged.
- `TestTeamPlanPrecedence`: a submission beats the plan, the plan beats a carried-over lineup, and saving a plan on matchday changes the pending lineup but no submitted or played lineup.
- `TestTeamPlanKeepsUnavailablePlayers`: an injured and a released player stay in the plan and are listed as unavailable, each match drops them and cuts a squad-long bench to the league's 7, and the injured player returns to his slot once fit.
- `TestSetTeamPlanRejections` (zero ID, stale revision, shape, a foreign player, reused ID, live match, no user club; retries before and after a save) and `TestRestoreRejectsInvalidTeamPlans`. `selection` tests cover plan storage, rejections and copies.

### Handoffs

- `ui--team-plan-editor`: the editor between matchdays in both clients, and the `LineupFromPlan` label and dropped-player messages in `views.go`.

## ui: saved team-plan editor in both clients (done)

Delivered the UI for `match`'s saved team plan. `lineup` opens the saved plan between matchdays; `teamplan` opens it on matchday too. The web lineup page has a team-plan mode alongside the one-off matchday editor. Both clients identify when a match uses the saved plan.

- The terminal saves each plan edit with `World.SetTeamPlan`; the browser submits its lineup form to the same command. Both show unavailable plan members and keep the app's fitting and eligibility rules.
- The team plan survives save/load and is used at the next user match without a submitted lineup.
- Added the missing `LineupFromPlan` display label to `World.LineupSourceLabel`.

### Verification

- `TestTeamPlanCanBeEditedBetweenMatchesAndIsUsed` in both client packages drives the editor as a player would, checks the saved plan and verifies the matchday source.
- `go test ./cmd/play` and `go test ./cmd/web` pass.

## ui: drag-and-drop formation editor on a pitch (done)

A priority request from the user: edit the lineup by dragging players around a pitch. The lineup and team-plan pages now start with a pitch that shows the formation (for example "4-4-2"). Lines run from attack at the top down to goal, and the bench and unselected players sit beside the pitch (below it on a phone). The terminal draws the same pitch above its lineup table.

- **Web.** The pitch is drawn on the server, and the lineup form is still what gets saved, so the page works without JavaScript through the Selection column. With JavaScript, the manager drags a player onto another to swap them, or into a gap in a line, the bench or the unselected shelf to move him there. Tap-to-pick then tap-to-place works for touch, and Enter/Space do the same from the keyboard. Drags use pointer events, so the mouse and touch share one path, and the page scrolls when a drag nears the top or bottom of the screen. Every move updates the players' Selection fields, the formation, the starter count and the out-of-position marks. Changing a Selection in the table moves the player on the pitch.
- **Order within a line matters.** The tick engine spreads each line across the width in slot order, so the pitch posts a new `order` field (player IDs, space-separated). The server orders starters by role and then by `order`, and orders the bench by `order` too. Players missing from `order` fall back to ID order, as before. Without JavaScript the field carries the current order, so a table edit no longer resets each line to ID order.
- **Terminal.** `lineup` and `teamplan` print `Formation 4-4-2, attacking upwards` and a text pitch with "ID Surname" cells in slot order, starred when out of position. The existing `swap` and `role` commands make every move a drag makes: swapping two players in a line swaps where they stand.
- **Shared views.** `app.FormationLabel(selection.Lineup)` names the shape. `app.NaturalRole(players.Position)` exports the existing position-to-role mapping, and play's duplicate of it is gone.
- **Decision:** the pitch draws the first slot on the left. Nothing yet says which flank `Y = 0` is. `match--line-slot-flank` asks `match` to pin it down, and the clients will mirror the pitch if needed.

### Verification

- `TestLineupPitch` (web) reads the pitch zones as a player sees them, reverses every line and the bench through `order`, moves a defender up front, and checks that the saved plan, the formation and the out-of-position mark come back.
- `TestLineupPitch` (play) checks the formation line and the attack line after `role` and `swap`. `TestFormationLabel` checks the label.
- A headless Chromium script (not committed: the repository uses the standard library only) drove the page with 18 checks: mouse drag into a gap and onto a player, tap-to-pick, keyboard swap, table-to-pitch sync, save and reload, a touch drag at 390 px with no horizontal scroll, the no-JS fallback and no script errors. Light, dark and phone screenshots were reviewed. They showed a four-player line wrapping over the next line on a phone, which was fixed.

### Handoffs

- `match--line-slot-flank`: which flank a line's first slot is on.
- Closes `ui--team-plan-editor` (delivered in the previous ui session; the note had not been deleted).


## ui: decision overview in both clients (done)

The manager can see what is ahead in one place: `World.Agenda()` (in `internal/app/views.go`, read-only, built from `Pending`, `Schedules`, `Cups`, `FixtureInfo`, `Offers`, `Squad` and `TransferWindow`) returns items in date order. Each has a kind, the instant it falls on, a `Now` flag for what must be dealt with before play goes on, and one sentence of text shared by both clients.

- **Items.** The waiting matchday (`Now`), bids for the club's players (`Now`, with the answer deadline from the offer), the next three matches across league and cups, every contract ending at the next contract-year end with what the player asks, the club's own open bids with their answer date, and the transfer window (its opening, or its last bidding day).
- **Web.** The home page has an "Ahead" panel. Each row links to where it is handled: lineup, transfers, the fixtures page or the player profile.
- **Terminal.** `agenda` (`todo`) prints the same rows, `NOW` marking what waits, each with the command that handles it (`lineup`, `accept 84 or reject 84`, `renew 56`, `transfers`).
- **Decision.** The existing home notes and `status` lines stay for now. The stop rules are unchanged: `competitions--shared-manager-decision-stops` will decide them.
- **Forecast vs. agreement.** The contract row shows the player's current ask, which is a forecast. When `squad` delivers its contract-planning view (which positions are at risk if contracts lapse), the row can show that too.

### Verification

- `TestAgendaOrdersWhatIsAhead` and `TestAgendaListsExpiringContractsAndBids` (app) check date order, the matchday as the one item that must be acted on, at most three matches, one item per final-year player, a bid to answer with its deadline, and that a read changes no revision.
- `TestAgenda` (play), and additions to `TestPlayingAMatchday` and `TestTransfersInTheBrowser` (web), check what a player sees.


## ui: the club's played formation in match reports (done)

The match report shows the lineup the user's club played: formation, starters by line on the same pitch as the lineup editor (read-only, names link to the player page), and the bench.

- **Web.** `/report` draws it below the timeline from `World.SubmittedLineup(fixture)`, with each player's name and position from `PlayerName` and `PlayerProfile`.
- **Terminal.** After `FULL TIME` the play report prints the same text pitch as `lineup` (`showPitch` takes the hint text as a parameter).
- **Limit.** Only the manager's lineups are stored (`sideSelection`). A match the club played on the assistant's suggestion, and the opponent's side, have no lineup to draw, so nothing is shown rather than a guess. `match--report-lineups` asks `match` for both sides' played lineups.
- Closes `ui--academy-fills-managed-vacancies` (declined earlier; the note is deleted).

### Verification

- `TestPlayingAMatchday` (web) submits a lineup and finds the formation, the attack line, the bench and player links on its report. `TestGameReportsWhenClickingOnScores` checks that an assistant-picked match shows none. `TestEditedLineupIsPlayedAndSaved` (play) checks the formation follows the full-time line.


## match: both sides' lineups in match reports (done)

`MatchReport.Lineups` holds what each side started a match with (starters in slot order with roles, bench, starting mentality), for the AI's side as well as the manager's. It is stored, not re-derived: the AI's pick depends on condition, injuries and transfers at kickoff, which all move afterwards. That made it authoritative state, so `storage.SchemaVersion` is 26. `restoreResolve` checks each lineup's shape, that the manager's side equals the stored lineup, and that goal scorers and substitutes belong to the side's lineup.

- **`World.ProbableLineup(team)`** shows what the AI would field for any senior team of a current league today, for other clubs' squad pages. The user club's team is rejected (its lineup is the manager's).
- **Flank.** The `matches` contract now says a line's first slot is on the team's left flank looking upfield (Y = 0 for a side attacking towards X = PitchLength, Y = PitchWidth for the other), so the pitch is not mirrored. `TestLineSlotsRunLeftToRight` pins it for both sides and both halves in `tick`; `simple` has no geometry.
- Notes: `ui--report-lineups-delivered`, `data--schema-26-report-lineups`.

### Verification

- `TestReportsKeepBothPlayedLineups`: the manager's side equals his stored lineup and the AI's side equals what `ProbableLineup` said just before the round; reports are equal after a save and load and are returned as copies.
- `TestRestoreRejectsInvalidReportLineups`, `TestProbableLineupRejections`.
