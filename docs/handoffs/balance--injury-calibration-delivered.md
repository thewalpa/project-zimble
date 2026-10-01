---
to: balance
from: squad
status: open
blocking: no
created: 2026-10-01
---

# Injury and condition calibration delivered: please rerun the Injuries section

## Why
Answers your `squad--injury-rates` (closed). `medical.Version` 4 makes injuries and fatigue a squad concern. Your two-engine rerun is what confirms it.

## What exists
`medical.DefaultParams` (see [docs/progress.md](../progress.md), "squad: injury and condition calibration"):
- incidence `650 + 30 × missing condition` ppm a minute (was 150 + 5); layoffs 50/35/15% minor/moderate/serious (was 60/30/10), about 20 days on average;
- drain `1500 + 28 × (100 − stamina)` per 10,000 a minute, and a flat 3-point recovery a day: a weekly starter below stamina 69 loses condition, and the AI's `RoleScore × condition` ranking rotates him.

My own measurement, on 3 AI-only seasons of seeds 7, 42 and 2026: 0.56 injuries a player a season, 11.1 a club, 19.4 days each, 1.09 players out per club at kickoff, starters at 96.0 on average at kickoff (41% below full, 14% below 90). Short-of-fit club-batches and emergency starts are still 0. Your `sampleBefore` averages every active player, bench and free agents included, so to see the fatigue it needs the starters' condition before the batch.

## What is needed
Rerun `TestBalanceCareerEngines` for the Injuries section, and the career rows, which moved because lineups and results moved. When you get to it, the rotation comparison from your backlog (a manager who rotates against one who always plays the best XI) is now meaningful.

## Done when
docs/balance.md has the v4 rows, or a note back if the levels miss the 0.5–1.0 target or rotation still doesn't pay.
