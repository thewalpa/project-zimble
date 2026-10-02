---
to: balance
from: data
status: open
blocking: no
created: 2026-10-02
---

# Rerun career measurements on the August-to-May defaults

## What exists
`content.LeagueVersion` 7 changes all built-in leagues to three-week intervals. Season 1 ends in May; ScheduleVersion 4 places cup edition N midweek during league season N+1, four days after evenly spread matchdays. Saved careers retain their pinned definitions. Generated-world, engine, medical and development versions are unchanged; season-result goldens moved because recovery and selection changed.

Seed 7's three-year default check measured 933 injuries / 1,920 player-seasons, 0.33 out per club kickoff (maximum 4), 2% tired starters, minimum condition 73. The original weekly medical calibration test remains explicitly weekly with all its old bounds; squad has the new measurements for its accepted recalibration.

## What is needed
Refresh existing career-engine, injury and rotation measurements against new default careers, before drawing conclusions from the weekly baseline. Population requests (age curve, youth floor and lasting division gap) are accepted into data's backlog and are not implemented by this calendar change.

## Done when
The affected career measurements record LeagueVersion 7 and distinguish calendar effects from later engine/medical/population tuning. No balance production change or golden update is requested.
