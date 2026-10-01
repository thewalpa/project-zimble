---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Career league matches score 2.2–2.3 goals in both engines, below real football

## Why
Both engines are calibrated on `enginetest` profiles, where `simple` scores 2.71 a match and `tick` 2.40 at 60 v 60. A player sees career squads, and there both engines score less: every league table in the game is low-scoring.

## What exists
[docs/balance.md, "Career seasons"](../balance.md#career-seasons-tick-against-simple), `TestBalanceCareerEngines`: 2,016 league matches per engine (seeds 7, 42, 2026 × three seasons, AI-only, `tick` v7, `simple` v5):

| | simple | tick | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match, careers | 2.16 | 2.30 | 2.6–2.9 |
| Goals per match, synthetic 60 v 60 | 2.71 | 2.40 | |
| Home–away goals, careers | 1.25–0.92 | 1.29–1.00 | 1.5–1.2 |
| Home / draw / away %, careers | 43.7 / 28.6 / 27.7 | 43.0 / 27.2 / 29.8 | 45 / 26 / 29 |

- Results are right: the home edge and the draw rate are on the real column. Only the goal level is low.
- `simple` loses 0.55 goals between its synthetic and career rows, `tick` 0.10 (it is already low on synthetic teams, which `match--tick-stats-calibration` covers through its shot count).
- The cause is in the inputs, not one engine. Career squads spread attributes across roles and both clubs field their best XI. Measured at v6 too (2.18 and 2.23), so it is not new with v7 or v5.

## What is needed
Decide whether career squads, not synthetic profiles, set the goal target. If they do, lift the career goal rate towards 2.6, for example with a goal-rate parameter, or by calibrating against a career-like `enginetest` profile. Keep the career home/draw/away split. Each engine is a separate version bump. `TestBalanceCareerEngines` (about 25 s on 32 cores) is the check.

## Done when
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1` reports 2.5–2.9 goals a league match for the engine(s) you tune, with home % still above away %. Or the note is declined with the reason (for example, that the synthetic level is the contract and the career gap is accepted).
