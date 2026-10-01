---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Attacking mentality is still a free upgrade in tick v6

## Why
`balance--tick-mentality-delivered` asked whether the smaller v6 effect still makes mentality a choice. It is a choice only in `simple`. In `tick` attacking beats balanced for every side and gap measured, and defensive never pays, so a manager on `tick` should always attack. Details and commands: [docs/balance.md, "Mentality"](../balance.md#mentality).

## What exists
`TestBalanceEngineComparison` and the new `TestBalanceMentalityByGap` in [internal/matches/tick/balance_test.go](../../internal/matches/tick/balance_test.go), 3,000 matches per row (seeds 1, 42, 2026), `tick` v6 against `simple` v4. Points per match for the side that changes mentality, other side balanced:

| Side | balanced | defensive | attacking |
| --- | --- | --- | --- |
| 60 v 60 at home | 1.54 | 1.47 | 1.78 |
| underdog 55 v 65 at home | 1.01 | 0.95 | 1.16 |
| favourite 65 v 55 at home | 2.08 | 1.97 | 2.28 |
| underdog 65 v 55 away | 0.70 | 0.66 | 0.81 |

Goals conceded move by only +0.02…+0.11 when a side attacks (about 1–3 standard errors), while goals scored rise by 0.26–0.45. `simple` concedes 0.2–0.3 more and gains 0.03–0.08 points. Neither `medical` nor `app` reads mentality, so there is no fatigue or injury cost either. A defensive underdog gets more draws but fewer wins, and loses 0.04–0.06 points a match.

## What is needed
A cost for attacking that the manager can see in results: for example more chances conceded on the counter when the back line is high (the offside line already moves), or a stamina cost. A fatigue cost would also need `squad` (condition drain is in `medical`): tell me which you choose. Defensive needs a payoff for the side that uses it, ideally when it is the weaker one or leading late; the sweep sets one mentality for 90 minutes, so I can't see that case. Tune it, bump `tick.ModelVersion`, and I rerun both tests.

## Done when
`TestBalanceMentalityByGap` shows attacking and balanced within about 0.05 points a match of each other for equal teams, and defensive ahead of balanced for the underdog (or a stated reason why not).
