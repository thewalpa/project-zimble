# project-zimble

Football manager simulation core in Go. Design: [docs/architecture.md](docs/architecture.md). Status and next task: [docs/progress.md](docs/progress.md).

## Commands

```sh
gofmt -l .              # must print nothing
go vet ./...
go test ./...
go run ./cmd/simulate -seed 42
go run ./cmd/simulate -seed 42 -season   # play every round, print the final table
go run ./cmd/simulate -season           # no -seed: random seed, reported on stderr
go run ./cmd/simulate -seed 42 -rounds 7 -save career.json && go run ./cmd/simulate -load career.json -season
go run ./cmd/simulate -seed 42 -club 3 -mentality attacking -season   # manage club 3, submit lineups
go run ./cmd/simulate -seed 42 -season -save career.json && go run ./cmd/simulate -load career.json -season   # seasons 1 and 2
go run ./cmd/play                        # interactive career (type help); -seed 42 -club 3, or -load career.json
```

Run all three checks before reporting work as done.

## Implementation rules

- **One owner per fact.** `registry` owns identities (clubs, teams, player names); `players` owns positions and attributes; `employment` owns player → club/team and contracts (expiry, weekly wage); `finance` owns club ledgers (balances are derived from entries, never assigned); `medical` owns player condition (drained by matches, restored by the daily recovery task); `competitions` owns competition seasons (past ones are kept), entrants, fixtures, kickoff times, round status and official results (standings, champions and history are derived on demand, never stored); `selection` owns submitted lineups per (fixture, team); `ai` makes selection decisions from detached data only; `core/sim` owns the calendar, world clock and task queue (a cohort is the consecutive tasks sharing instant, phase and kind); `app` owns the user club, the manager's live match (a replay log of stops and decisions, never a stored session), each league's current season, season transitions, task kinds, task payload records, the command log, the committed event journal and `Continue`. `events` defines typed domain events (a contract, like `matches`); `inbox` is a read model built only from events. Every commit emits its events in the same commit; failed commands, retries and reads emit nothing. Match engines (`internal/matches/*`) own only session-local state; they import `matches` and core, never `app`, `sim` or module stores, and never change the world. Other packages read them through exported queries only.
- **Domain modules import only `internal/core/*`**, except that `ai` and `selection` may import the `matches` contract for its roles and tactics, and `inbox` imports `events`. Only `internal/app` composes modules. `internal/app/boundaries_test.go` enforces the allowed import graph; every new package needs an entry there, chosen deliberately.
- **Create packages only when a milestone needs them.** No empty placeholder packages.
- **Typed IDs** from `internal/core/ids`; zero is invalid. Never use row indices or names as identity.
- **Modules copy on input and output.** Constructors clone input slices; queries return fresh slices or values. Never expose internal storage.
- **Content vs runtime state.** `content` holds read-only definitions; `worldgen` turns definitions + seed into a plain `Snapshot`; `app` loads the snapshot into module stores and does not keep it.
- **Game time.** Use `sim.GameInstant` (minutes since the career epoch) and `sim.Calendar` (UTC only). Never call `time.Now`/`Since`/`Until` or use `time.Local` in non-test code (`TestNoWallClockOrLocalTime`). Queued tasks are plain records (kind + payload ID), never closures.
- **Determinism.** Canonicalize (sort) inputs before seeded draws. Use `internal/core/random` (never `math/rand` or wall-clock time). Derive a stream per subsystem and entity: `random.Derive(seed, "domain/name", version, entityID)`. Never let map iteration order affect results or output; sort by ID.
- **Versions.** Changing output for the same seed requires bumping the version that covers it (`worldgen.Version`, `content.Version`, `random.Version`, `competitions.ScheduleVersion`, `content.LeagueVersion`, `ai.SelectionVersion`, `medical.Version`, `simple.ModelVersion`) and updating the golden hash in the package's tests. Competition work must never change the world fingerprint.
- **Enum values are durable.** Assign explicitly; never reorder.
- **One 100-point scale.** Attributes and overall are 1–100 and condition is 0–100 everywhere: content, storage, simulation, AI, saves and display. Finer rates (drain, readiness, fatigue) are internal fixed-point and are rounded to whole points once per result. Don't introduce another rating scale.
- **Integer arithmetic** for ratings, averages and match probabilities (ppm/permille). Money is `core/money.Money`: int64 minor units with overflow-checked `Add`/`Neg`/`Sum`. No floats in domain state or simulation calculations.
- **Validation.** Each module validates its own invariants in its constructor; `app.World.Validate` checks cross-module references. Report errors; never silently repair state.
- **Commands** that change the world validate and compute everything first, then commit through one all-or-nothing module call; nothing after that call may fail. They carry a `CommandID` (retries return the recorded result) and an `ExpectedRevision`.
- **Saves.** Every piece of authoritative state must appear in its module's snapshot type, be restored (never regenerated from the seed) and be validated on restore; derived views are rebuilt. Snapshot field names are the save schema: renaming or adding authoritative fields requires bumping `storage.SchemaVersion`. Restore must not run tasks, resolve matches, change the revision or allocate IDs.
- **Tests** sit beside the code. Prefer invariant, determinism and rejection tests over tests that restate formulas.
- Standard library only unless there is a clear reason.

## Keep docs current

After finishing a task, update `docs/progress.md` (completed work, decisions, next task).
