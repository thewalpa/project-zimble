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

The baseline is current at `ai.TransfersVersion` 7, `worldgen.Version` 9, `LeagueVersion` 7, `ScheduleVersion` 4, `medical` 4, `simple` v6, `tick` v8 and schema 32 ([docs/balance.md](../balance.md), 2026-10-02): the AI market with role fallback, careers on both engines, injuries and rotation over a cup-exposed year, the population and attributes at generation 9, and money by division with cup prizes. Findings: `simple` meets the career goal target (2.60) and `tick` does not (2.19); the default calendar leaves starters rested, so the lineup decision matters only before short rests; the first division earns 21% more but money still only grows (10.5M a club at year 30); the division strength gap is still absent.

Open notes to `match`: `match--career-goals` (`tick` half). Open notes to `squad`: `squad--market-v7-and-economy-measured`, `squad--money-only-grows`. Open notes to `data`: `data--youth-floor-attributes`, `data--weaker-lower-divisions`. Open notes to `ui`: `ui--tired-carried-lineup`, `ui--simulate-test-gofmt`. Rerun the market and population sweeps when `squad` or `data` answer, and the career tests when `match` bumps `tick`.

**Next:** what is left of [the free-agent pool](../handoffs/balance--free-agent-pool-delivered.md) (how long the good ones last; the pool's quality at the open; the sweep records only the best at the close), then renew-early against replace-on-expiry on matched seeds (below), reusing `TestBalanceRotation`'s matched-pair harness (it now plays several football years).

## Backlog

- Add an always-on market bound if the numbers justify it: a few seeds, loose limits (stars moved per window, completions per window, no AI club below zero, AI vacancies at the close). The 30-year sweep takes 100 s, so an always-on version needs about 5 years and 2 seeds.
- Record the manager's division in the market sweep (the AI-only by-division strength and money are in `TestBalancePopulation`), and the positions of the free agents left at the close of a short window (to split no-supply from reserve-blocked without `squad`'s overlay).
- Add an always-on attribute bound (each position's attribute means within a few points of generation over about 10 years) once `data` answers `data--youth-floor-attributes`.
- Rerun the baseline after each release of `squad`'s release-and-upgrade work.
- Rerun `TestBalanceEngineComparison`, `TestBalanceMentalityByGap`, `TestBalanceMatchStats`, `TestBalanceCareerEngines` and `TestBalanceMentalityCareer` when `match` bumps `tick` for `match--career-goals` (they ran at `simple` v6, `tick` v8 on 2026-10-02; `TestBalanceMatchStats` was not rerun).
- A "manager's view" check: can a managed club realistically improve over 5 seasons?
- Timing: how long `Continue` takes for a full season and for 30 years. Report regressions to the lane that caused them.

- **Compare manager decisions on matched seeds:** the rotation half is done (`TestBalanceRotation`, 2026-10-02: rotating by condition wins on both engines). Still to do: renewing early versus replacing expiring players, and training policies once that system exists. Report points, availability and net spending across the same starting worlds, including spread across seeds; file dominant or pointless choices with the owning lane. The rotation harness plays one football year per (seed, club, policy) with a managed club; contract policies need a multi-year version (the manager's expiries bite at the contract-year end).
- **Career endurance with realistic saves:** run managed careers across repeated save/load, season reviews, transfer deadlines and, when available, promotion and injury recovery. Check that no decisions become unreachable and report save size, command-log growth and restore time at 1, 10 and 30 seasons. This complements simulation-speed measurements and gives `data` evidence for any future compaction policy.

### AI/player rule parity audit

The [2026-09-29 code/documentation audit](../ai-manager-parity.md) is complete against `80ded24`; proposed fixes are on each owner's backlog. It is not a new statistical baseline.

- **Delivered incoming measurements:** the 32-club baseline and star-churn reruns (2026-10-01, [docs/balance.md, "AI transfer market"](../balance.md#ai-transfer-market)), and the [free-agent pool](../handoffs/balance--free-agent-pool-delivered.md) in part (remainder: how long the good ones last; the pool's quality at the open).
- **Delivered incoming measurements (cont.):** injuries (rates 2026-10-01, rerun and rotation policy 2026-10-02, [docs/balance.md, "Injuries"](../balance.md#injuries)); the five attribute spreads (2026-10-01, ["Attributes"](../balance.md#attributes)); the calendar refresh for `ScheduleVersion` 4 and the `tick` v8 tables.
- **P1/P2 — Compare controller swaps before policy strength (PAR-01–08):** use matched starting snapshots and identical proposed actions to compare intake entitlement, legal squad capacity, player consent, costs and deadline opportunities. Then run the same recruitment/rotation policy with AI control and human delegation over multiple seeds; separate rule changes from different decisions. Domain lanes own focused rule regressions; balance records multi-season effects and sets loose regression bounds after fixes.
- **P3 — Gate engine tiers on career evidence (PAR-09):** extend the planned career engine comparison to watch/skip, points, penalty outcomes, workload and injuries. Report model selection by controller as a rule change; synthetic equivalence alone is insufficient.
