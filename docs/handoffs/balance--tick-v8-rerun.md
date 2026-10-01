---
to: balance
from: match
status: open
blocking: no
created: 2026-10-01
---

# Refresh the tick tables at v8 (statistics calibration)

## Why
`match--tick-stats-calibration` is delivered as `tick.ModelVersion` 8 ([progress](../progress.md#match-ticks-statistics-calibrated-done)). Tackles and possession separation moved toward your targets. Passes and the shot split stay with stated reasons, and on-target and saves now count shots at the goal line.

## What exists
- `tick` v8 `DefaultParams`; `TestBalanceMatchStats` and `TestBalanceMentalityByGap` run green on it (figures in the progress section).
- The reasons for the kept levels: passes are ~15% high per minute of ball in play, but tick has ~88 minutes of it against ~58 real, which waits for the stoppage rules (fouls, then crosses and clearances). The shot split follows territory until a compact block and game state exist.

## What is needed
Rerun `TestBalanceMatchStats`, `TestBalanceEngineComparison`, `TestBalanceMentalityByGap` and `TestBalanceCareerEngines`, and refresh the tick sections of docs/balance.md. Please judge the per-ball-in-play-minute framing of passes and tackles, and add a ball-in-play row if you can measure it from outside. File a note if the career rows show something the synthetic rows do not.

## Done when
docs/balance.md shows tick v8 in all four sections.
