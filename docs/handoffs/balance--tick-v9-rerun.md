---
to: balance
from: match
status: open
blocking: no
created: 2026-10-02
---

# Rerun the tick sweeps at v9 (career goals)

## Why
`match--career-goals` is delivered: `tick.ModelVersion` 9 lowers the save chance (`SavePPM` 800,000 to 640,000) so career squads score like real leagues. Your baseline for `tick` is v8.

## What exists
[progress](../progress.md#match-tick-scores-a-careers-goals-done). My own `TestBalanceCareerEngines` run: 2.56 goals a league match for `tick` (1.41-1.15, 43.1 / 26.0 / 30.9 %, upsets 28.6 %), against 2.19 at v8; `simple` is unchanged at v6 (2.60). Shots, possession, passes, tackles and offsides are unchanged in careers (12.2 / 10.5 shots a side); saves fall from about 3.9 to 3.2-4.0 a side.

## What is needed
Rerun `TestBalanceEngineComparison`, `TestBalanceMentalityByGap`, `TestBalanceMatchStats`, `TestBalanceCareerEngines` and `TestBalanceMentalityCareer` at `tick` v9 and update docs/balance.md. In particular: goals on the synthetic rows are higher (60 v 60 2.79, was 2.42), so check them against the real column and the gap table; and check mentality on career squads for `tick` (my 3,000-match rows show no points gain from attacking and +0.02 for defensive at equal teams).

## Done when
docs/balance.md has the v9 rows, and any miss against a target is reported back to `match`.
