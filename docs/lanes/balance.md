# Lane: balance

Whether the game plays plausibly over time. This lane runs large seeded simulations, measures matches, money, markets and populations, writes the statistical and multi-season tests that protect them, and reports what it finds to the lanes that own the rules. It changes no production code.

## Owns

- Test files named `balance_test.go`, in any package, and their helpers in the same file.
- [docs/balance.md](../balance.md), created on the first measurement: the current numbers, with the seeds and versions used to get them, and the history of changes.
- A `cmd/balance` runner, only if tests stop being enough. It needs an entry in `boundaries_test.go` and a note to `data`.

## Rules for this lane

- **Measure, then report.** Don't tune parameters, formulas or content yourself. File a note to the owning lane with the measurement, the seeds and the range you expect. Tuning belongs with whoever owns the version constant.
- **Keep `go test ./...` fast.** An always-on balance test uses a few seeds and loose bounds that only a real regression breaks. Sweeps over many seeds or decades skip unless `ZIMBLE_BALANCE=1` is set. Write down the command that reproduces every number in `docs/balance.md`.
- **Bounds, not formulas.** Assert trends and invariants: the stronger team wins more often, attacking produces more goals, total money moves only through income and wages, squads stay full, the population doesn't grow or shrink over decades. Never restate a formula as its expected value.
- **Deterministic.** Seeds are listed explicitly, and results sorted by ID, so a failing test reproduces exactly. No wall clock and no `math/rand`.
- **Never update a golden.** If a golden moves, find the change that moved it and write a note to the owning lane.
- **Play the game.** Now and then, play a season in `cmd/play` or `cmd/web` as a manager would. Write up anything confusing, blocked or pointless as a note: to `ui` for presentation, and to the owning lane for rules.

## Depends on

Every lane: it measures what they build, and they answer its notes. After a lane changes a version constant, rerun the affected sweeps and update `docs/balance.md`.

## Now

**A baseline.** The market part is done: see [docs/balance.md, "AI transfer market"](../balance.md#ai-transfer-market) and its sweep in `internal/app/balance_test.go`. The match engines are profiled on synthetic teams: see ["Match engines: tick against simple"](../balance.md#match-engines-tick-against-simple) and `internal/matches/tick/balance_test.go`. Matches measured over career seasons, money over 10 seasons and the population remain.

Measure the current game over a fixed seed set and record it in `docs/balance.md`:

- **Matches:** home, draw and away rates, goals per match, win rate by overall gap, the effect of mentality, and how often cup ties go to penalties.
- **Money:** club balances over 10 seasons, wages against gate receipts, and how many clubs trend towards insolvency.
- **Population:** squad sizes, age and overall distributions over 30 years, retirements against youth intake.
- **Market:** transfers per window, fees against valuations, and how long vacancies stay open.

Then add always-on bounds for the most important of these, and file notes for anything implausible.

## Backlog

- Rerun the market sweep when `squad` answers `squad--star-churn` or `squad--free-agent-pool`, then add an always-on churn bound (a few seeds, loose limits).
- Rerun the baseline after each release of `squad`'s release-and-upgrade work, and after promotion and relegation lands.
- Rerun `TestBalanceEngineComparison` when `match` answers `match--tick-mentality`, `match--tick-goals-by-level` or `match--shootout-favourite`. Then propose always-on bounds for mentality and shootouts (a few seeds, loose limits) to `match`, whose trend tests they would sit beside.
- Once `app` can run a career on `tick` (`match` roadmap phase 2), compare the two engines over league seasons: goals, home and draw rates, upsets, final-table spread.
- A "manager's view" check: can a managed club realistically improve over 5 seasons?
- Timing: how long `Continue` takes for a full season and for 30 years. Report regressions to the lane that caused them.
