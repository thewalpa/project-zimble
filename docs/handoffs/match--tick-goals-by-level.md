---
to: match
from: balance
status: accepted
blocking: no
created: 2026-09-28
---

# Tick: goals grow with team quality and with the rating gap

## Why
In real football, goals per match hardly depend on how good both teams are, and a mismatch raises goals only modestly. In `tick`, both drive goals up steeply. Career squads sit at 54–63 today, so this barely shows yet. It will show in any league or cup whose teams are far from 60, in cup ties between leagues of different strength, and as ratings drift.

## What exists
Measured at `tick.ModelVersion` 1: 3,000 matches per row, seeds 1, 42 and 2026 × fixtures 1–1000, balanced mentality. Full tables are in [docs/balance.md](../balance.md#match-engines-tick-against-simple).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1
```

| Match | tick goals | 0–0 % | 4+ goals % | simple goals |
| --- | --- | --- | --- | --- |
| 40 v 40 | 2.08 | 11.8 | 16.1 | 2.81 |
| 60 v 60 | 2.92 | 5.5 | 33.5 | 2.75 |
| 80 v 80 | 4.64 | 0.8 | 69.3 | 2.71 |
| 65 v 55 | 3.34 | 3.6 | 42.5 | 2.79 |
| 70 v 50 | 4.27 | 1.4 | 61.1 | 2.90 |

Win rates by gap look reasonable in the career's range: 72% for a home side 10 points stronger. The problem is the goal count. The stronger side scores much more, and the weaker side scores only a little less.

Accepted 2026-09-29: next in the match lane after the attribute, shootout and report-event notes, before tick phase 2.

Update from match, 2026-09-29, at `tick.ModelVersion` 4 (after the mentality fix): 40 v 40 scores 1.84 goals, 60 v 60 2.80, 80 v 80 4.46, 65 v 55 3.30 (75.9% home wins) and 70 v 50 4.52 (94.9% home wins). Level still drives goals, and the low end is now under the target too.

## What is needed
Make goals per match depend mainly on the difference between the sides, not on their absolute level. For example, attack and defence (and finishing and goalkeeping) could scale together with rating. Also make a mismatch show more in who scores than in how many goals there are. Bump `tick.ModelVersion`.

## Done when
In the sweep above, equal teams at 40, 60 and 80 all score 2.3–3.2 goals a match, and 70 v 50 stays under about 3.5 goals while the stronger side still wins clearly.
