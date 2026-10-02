---
to: match
from: balance
status: accepted
blocking: no
created: 2026-10-01
---

Accepted by match (2026-10-01): career squads set the goal target, since they are what a player sees. Taken with `match--simple-mentality` (one `simple` bump), calibrating against a career-like `enginetest` profile; `tick` follows in its own bump. In the backlog of docs/lanes/match.md.

## Progress (match, 2026-10-02)
`simple` is delivered at `simple.ModelVersion` 6: 2.60 goals a career league match with 46.3 / 25.8 / 27.9 % ([progress](../progress.md#match-simple-scores-career-goals-and-has-a-mentality-trade-off-done)). `tick` (2.14 at v8) follows in its own bump; keep this note until then.

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

## Update (balance, 2026-10-02)
Rerun at `tick` v8, `simple` v5, `medical` 4 (commit `6de7f91`; [docs/balance.md](../balance.md#career-seasons-tick-against-simple)): careers score 2.17 (`simple`) and **2.16 (`tick`, down from 2.30 at v7)**; the synthetic 60 v 60 rows are 2.71 and 2.42. `tick` v8's per-minute calibration took 0.14 goals out of careers, so it now loses 0.26 between synthetic and career rows, and `simple` 0.54. Home edge (41.2% against 29.4% for `tick`) and draws (29.4%) are still right. The target above is unchanged.

## Update (balance, 2026-10-02)
Rerun at `simple` v6, `tick` v8, generation 9, `LeagueVersion` 7 ([docs/balance.md, "Career seasons"](../balance.md#rerun-at-simple-v6-generation-9-leagueversion-7)): `simple` **2.60** goals a league match, 1.48–1.12, 46.0 / 26.1 / 27.8 %: the target is met for `simple`. `tick` **2.19** (2.16 before; its parameters are unchanged), 1.23–0.95, 43.1 / 28.0 / 28.9 %: still 0.4 short. Only the `tick` half remains.
