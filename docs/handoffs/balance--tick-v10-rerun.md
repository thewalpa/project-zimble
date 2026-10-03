---
to: balance
from: match
status: open
blocking: no
created: 2026-10-03
---

# Rerun the tick sweeps at v10 (formations priced)

## Why
`tick.ModelVersion` 10 prices formations: compact lines, kept marking pairs, forwards that stay up and a midfield screen, with the runs, shots, saves and tackles recalibrated around them. Every `tick` number moves. It replaces the v9 rerun note (`balance--tick-v9-rerun`): v9's goal level is kept, so rerun once, at v10.

## What exists
[progress](../progress.md#match-formations-priced-in-tick-done). My own runs: career seasons (`TestBalanceCareerEngines`, 3 seeds x 3 seasons) 2.52 goals a league match (1.41-1.11, 44.8 / 25.6 / 29.5 %, upsets 29.2 %), against 2.56 at v9; league statistics 13.2 / 11.0 shots a side, 802 / 745 passes, 24.0 / 23.0 tackles, 0.7 / 0.6 offsides. Career profile 60 v 60: 2.67 goals. Flat profile 60 v 60: 3.18 (was 2.79), so the synthetic rows score more than careers by about half a goal. Mentality on 1,500-match rows: within 0.1 points of balanced at equal teams on both profiles, defensive -0.01 for the 55 v 65 underdog.

## What is needed
Rerun `TestBalanceEngineComparison`, `TestBalanceMentalityByGap`, `TestBalanceMatchStats`, `TestBalanceCareerEngines` and `TestBalanceMentalityCareer` at `tick` v10 and update docs/balance.md. Check in particular:
- offsides fell from about 2 to 0.6 a side (fewer runs in behind), against a real ~2;
- shots are 13 / 11 a side (v9 12.4 / 10.5) and passes fell by a fifth, nearer the real total;
- the synthetic rows' goal level against the real column, now that only careers are calibrated.

## Done when
docs/balance.md has the v10 rows, and any miss against a target is reported back to `match`.
