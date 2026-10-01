# project-zimble

Football manager simulation core in Go. Design: [docs/architecture.md](docs/architecture.md). Status: [docs/progress.md](docs/progress.md).

Several agents work on this repository in parallel lanes (ui, match, competitions, squad, data, balance). Read [AGENTS.md](AGENTS.md) for lane ownership, handoff notes to other lanes and the git workflow. Your lane's current task is in `docs/lanes/<lane>.md`.

## Commands

```sh
gofmt -l .              # must print nothing
go vet ./...
go test ./...
go run ./cmd/simulate -content   # inspect built-in content without generating a career
go run ./cmd/simulate -seed 42
go run ./cmd/simulate -seed 42 -season   # play every round, print the final table
go run ./cmd/simulate -season           # no -seed: random seed, reported on stderr
go run ./cmd/simulate -seed 42 -rounds 7 -save career.json && go run ./cmd/simulate -load career.json -season
go run ./cmd/simulate -seed 42 -club 3 -mentality attacking -season   # manage club 3, submit lineups
go run ./cmd/simulate -seed 42 -season -save career.json && go run ./cmd/simulate -load career.json -season   # seasons 1 and 2
go run ./cmd/play                        # interactive career (type help); -seed 42 -club 3, or -load career.json
go run ./cmd/web                         # the career in the browser at http://127.0.0.1:8080; same flags as play, plus -addr and -save
```

Run all three checks before reporting work as done.

## Implementation rules

- **One owner per fact.** `registry` owns identities (clubs, teams, player names and birth dates) and the player ID allocator (IDs are never removed or reused); `players` owns positions, attributes and retirement, with the yearly development and retirement rules; `employment` owns player → club/team and contracts (expiry, weekly wage; a player without an assignment is a free agent); `finance` owns club ledgers (balances are derived from entries, never assigned); `transfers` owns transfer offers and where each stands (open, completed, rejected, expired, collapsed), and the transfer list (who is for sale at what asking price, until the window closes); `medical` owns active players' condition and injuries (condition drained by matches and restored by the daily recovery task; injuries drawn from a match's exposure and counted down by the same task; retired players have none); `competitions` owns competition seasons (past ones are kept) and their format (league or knockout bracket), entrants, fixtures (a knockout round's are created from the previous round's winners), kickoff times, round status and official results, penalties included (standings, rankings, champions and history are derived on demand, never stored); `selection` owns the manager's lineups per (fixture, team): submitted, or fitted from the team plan or carried over from the previous match and played; and one saved team plan per team; `ai` makes selection, contract (renewals, signings) and transfer (valuations, answers, bids) decisions from detached data only; `core/sim` owns the calendar, world clock and task queue (a cohort is the consecutive tasks sharing instant, phase and kind); `app` owns the user club, the career's match engine (chosen once, it plays every fixture), the manager's live match (a replay log of stops and decisions, never a stored session), each league's current season, the pinned league and cup definitions, season transitions (including drawing a cup edition from its qualifying leagues' final rankings), the yearly contract-year task (renewals, expiries and signings on the contract-year end), the yearly player-year task (development, retirement and youth intake on its eve), the transfer window with its daily transfer runs and the completion of accepted offers (employment, finance and the offer in one commit), task kinds, task payload records, the command log, the committed event journal and `Continue`. `events` defines typed domain events (a contract, like `matches`); `inbox` is a read model built only from events, and so is `careers` (every player's clubs, from the employment a career starts with and the events of each change since, which every change of employment must emit; and his appearances and goals at each, from `MatchCompleted`, which names who played and scored). Every commit emits its events in the same commit; failed commands, retries and reads emit nothing. Match engines (`internal/matches/*`) own only session-local state; they import `matches` and core, never `app`, `sim` or module stores, and never change the world. Other packages read them through exported queries only.
- **Domain modules import only `internal/core/*`**, except that `ai` and `selection` may import the `matches` contract for its roles and tactics (and `ai` also `core/money`), and `inbox` and `careers` import `events`. Only `internal/app` composes modules. `internal/app/boundaries_test.go` enforces the allowed import graph; every new package needs an entry there, chosen deliberately.
- **Create packages only when a milestone needs them.** No empty placeholder packages.
- **Typed IDs** from `internal/core/ids`; zero is invalid. Never use row indices or names as identity.
- **Modules copy on input and output.** Constructors clone input slices; queries return fresh slices or values. Never expose internal storage.
- **Content vs runtime state.** `content` holds read-only definitions; `worldgen` turns definitions + seed into a plain `Snapshot`; `app` loads the snapshot into module stores and does not keep it.
- **Game time.** Use `sim.GameInstant` (minutes since the career epoch) and `sim.Calendar` (UTC only). Never call `time.Now`/`Since`/`Until` or use `time.Local` in non-test code (`TestNoWallClockOrLocalTime`). Queued tasks are plain records (kind + payload ID), never closures.
- **Determinism.** Canonicalize (sort) inputs before seeded draws. Use `internal/core/random` (never `math/rand` or wall-clock time). Derive a stream per subsystem and entity: `random.Derive(seed, "domain/name", version, entityID)`. Never let map iteration order affect results or output; sort by ID.
- **Versions.** Changing output for the same seed requires bumping the version that covers it (`worldgen.Version`, `content.Version`, `random.Version`, `competitions.ScheduleVersion`, `content.LeagueVersion`, `ai.SelectionVersion`, `ai.ContractsVersion`, `ai.TransfersVersion`, `medical.Version`, `players.DevelopmentVersion`, `worldgen.YouthVersion`, `simple.ModelVersion`) and updating the golden hash in the package's tests. Competition work must never change the world fingerprint.
- **Enum values are durable.** Assign explicitly; never reorder.
- **One 100-point scale.** Attributes and overall are 1–100 and condition is 0–100 everywhere: content, storage, simulation, AI, saves and display. Finer rates (drain, readiness, fatigue) are internal fixed-point and are rounded to whole points once per result. Don't introduce another rating scale.
- **Integer arithmetic** for ratings, averages and match probabilities (ppm/permille). Money is `core/money.Money`: int64 minor units with overflow-checked `Add`/`Neg`/`Sum`. No floats in domain state or simulation calculations.
- **Validation.** Each module validates its own invariants in its constructor; `app.World.Validate` checks cross-module references. Report errors; never silently repair state.
- **Commands** that change the world validate and compute everything first, then commit through one all-or-nothing module call; nothing after that call may fail. They carry a `CommandID` (retries return the recorded result) and an `ExpectedRevision`.
- **Saves.** Every piece of authoritative state must appear in its module's snapshot type, be restored (never regenerated from the seed) and be validated on restore; derived views are rebuilt. Snapshot field names are the save schema: renaming or adding authoritative fields requires bumping `storage.SchemaVersion` and writing its save fixture (`go test ./internal/storage -run TestSaveFixtures -fixture`; fixtures are frozen, and the test fails when the snapshot types or a rule change the current schema's fixture). Restore must not run tasks, resolve matches, change the revision or allocate IDs.
- **Tests** sit beside the code. Prefer invariant, determinism and rejection tests over tests that restate formulas.
- Standard library only unless there is a clear reason.

## Keep docs current

After finishing a task, update `docs/progress.md` (completed work, decisions) and your lane's `docs/lanes/<lane>.md` (now, backlog). File a handoff note in `docs/handoffs/` for anything another lane must do, including the UI for any new player-facing feature.
