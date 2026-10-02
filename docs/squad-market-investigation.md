# Squad market investigation, 2026-10-02

The passive manager's unsold fringe listings and larger free-agent pool are expected under the current recruitment policy. Keep comparable free agents preferable to paying a fee, and keep the existing retirement rules. The recruiting manager's short windows expose a separate daily-action defect: a club can repeatedly consider an unfillable role while ignoring an affordable listed player at another vacant role. Fix that role fallback separately; changing club order or removing the wage reserve does not solve the wider shortage.

## Scenario and measurements

Baseline: main `2a9bf28` (subsequent UI fixture/review and squad documentation commits do not change domain behavior), worldgen 9, LeagueVersion 7, ScheduleVersion 4, TransfersVersion 6, ContractsVersion 2, medical 4, DevelopmentVersion 1; default simple engine. All careers play every football year, then advance to the contract-year end. Managed scenarios use club 3. The recruiting manager uses balance's existing `managerPolicy`; the passive manager makes no commands.

Reproduce the baseline without changing any source:

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run 'TestBalanceAIMarket(Sweep|WithPassiveManager|WithRecruitingManager)$' -v -count=1
```

| Scenario | Seeds / windows | Windows closing short | Total missing players | Unsold listings per seed | Active population range |
| --- | --- | --- | --- | --- | --- |
| AI-only | 1, 2, 3, 5, 7, 11, 13, 42, 99, 2026 / 300 | 14 | 14 | 3–9% | 640–643 |
| Passive manager | 7, 42, 99, 2026 / 120 | 3 | 3 | 52–59% | 640–660 |
| Recruiting manager | 7, 42, 99, 2026 / 120 | 27 | 40 | 24–29% | 640–652 |

Each seed covers 30 years. The previously reported recruiting result was 24 short windows / 31 missing players under older generation/calendar versions; it is not the current reproduction. All three current sweeps pass world validation and their accounting checks.

## Why fringe listings remain unsold

`market.aiActions` compares each vacancy with the deciding club's observed free-agent pool. `ai.ChooseTarget` requires a paid target to be at least `TransferMargin` (5) points better than the best free agent at that position and within the club's fee budget. A free agent therefore displaces an otherwise affordable fringe listing. A full club holding unsold surplus also makes no upgrade (`hasSurplus`), so it does not accumulate more replacements. Changing club ordering or the reserve does not make unwanted fringe players useful.

The pool remains part of the population rather than requiring an artificial disappearance rule. `playerYear` visits every nonretired profile, including unemployed players; `players.Retires` already retires free agents from age 31, with the same versioned lifecycle as everyone else. A young free agent can remain unemployed until then. The sweep measures population bounds, not individual unemployment spells; no claim about spell duration follows from the totals.

`TestVacancyRecruitmentComparesPoolAndListedPlayers` isolates one vacancy and one listed target with detached club observations. It covers preference for a comparable free agent, a bid for a materially better listed player, and a fee that cash can cover but the reserve budget cannot. Planning leaves the authoritative snapshot unchanged. `TestClosingRecruitmentFillsAvailableRoles` checks that the closing free-agent allocator fills an available smaller need even if a larger role has no supply.

## What causes the closing vacancies

A temporary diagnostic overlay observes `market` after `fillSquads` at the close, before listings are cleared. For every missing role it counts the remaining pool and same-position listings, asking prices within cash and within `TransferBudget`, and surplus/consent checks. It does not mutate plans. The diagnostic sweep reproduces exactly the baseline's 3 passive and 40 recruiting missing players.

- Passive: all three vacancies have no free agent at that position and a zero reserve budget, despite affordable listed surplus. Seed 7: club 19, year 3, FW; club 20, year 10, FW. Seed 2026: club 13, year 10, FW. This is the existing thin-market policy case, not a club unable to pay the actual fee.
- Recruiting: none of the 40 missing roles has remaining free-agent supply. Thirty-one have no same-position listing at the close; three have listed supply but a zero reserve budget; six have listed supply within the reserve budget. Availability at the close alone does not prove that a bid could previously complete, so the last group needs daily traces.
- A daily trace establishes one missed opportunity: seed 42, club 18, year 2. From game instant 547200 through 564480 it needs one MF and one FW. MF wins the role-order tie but has neither a free agent nor an eligible target. FW has the affordable listed target player 600, and `ai.ChooseTarget` accepts it under the unchanged budget and consent/candidate checks. `aiActions` considers only MF on each run. At the close (565920) both vacancies remain; there is no free agent in either role. This reproduction uses two years of `sweepMarket(t, 42, userClub3, 2, &managerPolicy{})`, with the observation before `ai.LargestNeed` and no pending bid at that point.

The daily role choice differs from `fillSquads`: the closing allocator already chooses the largest need that the pool can fill, whereas daily actions stop after trying the largest need even when there is no possible transaction. The next policy unit should try other vacant roles in descending need and canonical role order when the first cannot bid or sign. Preserve one action, existing response clocks, player consent, listed-only rules after a purchase, the shared squad limit and actual affordability.

## Policy comparisons

Two temporary Go overlays change only vacancy recruitment and rerun the same four managed seeds over 30 years. The production files, version constants, frozen fixtures and balance-owned tests remain unchanged.

| Vacancy policy experiment | Passive short windows / missing | Recruiting short windows / missing | Passive unsold listings |
| --- | --- | --- | --- |
| Current policy | 3 / 3 | 27 / 40 | 52–59% |
| Clubs ordered by total vacancies, descending, ID ties | 2 / 2 | 29 / 37 | 54–57% |
| Vacancy fee budget = cash balance; upgrade reserve unchanged | 1 / 1 | 24 / 33 | 53–59% |

For the ordering experiment, replace only the `aiActions` club loop with a stable ordering by `sum(m.needs(club).Count)` descending, then the existing club ID. For the budget experiment, replace only the vacancy branch's `ai.TransferBudget(m.balances[c.ID], m.wages[c.ID])` with `m.balances[c.ID]`. Run each using `go test -overlay <mapping.json>` and the passive/recruiting test selector above. These are whole-career counterfactuals: earlier changes alter later squads and markets, so totals do not assign a cause to any individual baseline vacancy. Neither is a demonstrated cure. The cash-budget experiment is not a production insolvency exemption or an adopted reserve policy.

Decision: close the passive-manager investigation with no rule/version change. Queue the proven role-fallback defect as the next narrow market change, with a TransfersVersion bump, deterministic/retry/save checks and a fresh seeded sweep. Evaluate the remaining positional supply and reserve cases after that change; keep them distinct from the unsold-fringe observation.
