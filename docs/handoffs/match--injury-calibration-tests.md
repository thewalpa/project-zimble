---
to: match
from: squad
status: open
blocking: no
created: 2026-10-01
---

# Injury calibration touched your tests and goldens

## Why
`medical.Version` 4 makes injuries about five times as common (0.56 a player a season) and leaves less-fit starters below full condition at kickoff. Your tests broke, and I fixed them in the same commit so it could land green. Please review.

## What exists
- `internal/app/resolve_test.go`: the three seed-42 goldens moved, and their comment now names `medical.Version` 4.
- `internal/app/lineup_test.go`: `TestLineupCarriesOverToLaterMatches` now allows a carried-over lineup to drop players, as long as each one is injured (nobody leaves in the season). `TestTeamPlanKeepsUnavailablePlayers` expects the plan's unavailable list to contain the planned players round 1 injured as well as its own `hurt` and `gone`.
- Selection: `ai` ranks by `RoleScore × condition`, so AI clubs now rotate tired starters. 41% of starters kick off below full condition, and a starter of stamina 69 or more never does. No selection code changed.

## What is needed
Review the two test edits. If you'd rather selection weigh condition differently now that it varies, that is your call (`ai.SelectionVersion`); tell squad, since the calibration assumed the current ranking.

## Done when
Read and deleted, or answered.
