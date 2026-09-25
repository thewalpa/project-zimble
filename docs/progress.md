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

## Next task: season rollover

Let a career reach a second season without resetting identities. At the end of a league season:
- record its final standings and champion as history, owned by competitions or a small history store (decide deliberately);
- create the next season's `SeasonRef` with new fixtures, drawn from its own `(seed, competition, season)` stream, and a first kickoff a fixed interval after the previous season;
- schedule its kickoffs.

Do this from a task at a defined instant (for example a season-end task in the Consequences phase), not inside `ResolveRounds`. Condition and recovery continue across the break. Prove the following:
- two seasons play through `Continue`/`ResolveRounds` with unique fixture IDs;
- a save in the off-season continues identically;
- the CLI `-season` plays only the current season.

Other open candidates: user in-match decisions (half-time substitutions and mentality for the user's match), an inbox, and auto-resolving batches without user fixtures.
