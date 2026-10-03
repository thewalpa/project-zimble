---
to: balance
from: match
status: open
blocking: no
created: 2026-10-02
---

# In `tick` a formation is free: confirm and measure

## Why
Formations can now change in a live match (`CommandSetRoles`), and measuring the command showed that in `tick` moving a midfielder to the attack costs nothing. The lineup editor could always set these shapes before kickoff, so the lever is old; the match lane will price it and needs your numbers before and after.

## What exists
Home side's roles set at kickoff by `SetRoles`, `enginetest.CareerInput(f, 60, 60)`, 600 matches a row, `tick.ModelVersion` 9 (goals for / against a match): 4-4-2 1.55 / 1.18; 4-3-3 3.01 / 1.16; 3-4-3 2.91 / 2.06; 5-4-1 0.97 / 1.38; all defenders 0.01 / 2.28; all forwards 8.16 / 9.65. `simple` on 2,000 matches: 4-4-2 1.51 / 1.12, 4-3-3 1.51 / 1.12, 5-4-1 1.45 / 1.14, 3-4-3 1.46 / 1.13. The measuring test was temporary; `internal/matches/tick/formation_test.go` (`TestRolesChangePlay`) keeps the extremes.

## What is needed
A sweep of the common shapes (4-4-2, 4-3-3, 3-5-2, 5-3-2, 4-5-1, 5-4-1, 3-4-3) for equal and unequal teams on `tick` career squads, reporting goals for and against, points and shots. Add the finding to `docs/balance.md` once the match lane has priced the midfield (it will file a note when `tick.ModelVersion` moves), and compare.

## Done when
`docs/balance.md` shows the shape table before and after, and no shape beats 4-4-2 on points at equal teams by more than a few hundredths.

## Update (match, 2026-10-03)
Priced in `tick.ModelVersion` 10 ([progress](../progress.md#match-formations-priced-in-tick-done)); the v10 refresh is [balance--tick-v10-rerun](balance--tick-v10-rerun.md). The cause was wider than the midfield: any shape that differed from the opponent's paid, through lines spread over the whole width and marking pairs that swapped every tick. `TestNoFormationIsFree` (`internal/matches/tick/formation_test.go`) now plays the six shapes against 4-4-2, 500 matches home and 500 away on career squads at 60 v 60: half the points gap a match, v9 to v10, 4-3-3 +0.75 to +0.19, 3-5-2 +0.49 to -0.11, 5-3-2 +0.40 to +0.09, 3-4-3 +0.29 to +0.10, 4-5-1 -0.22 to -0.30, 5-4-1 -0.37 to -0.20. With the shape on the stronger side (65 v 55, 500 matches home and 500 away, points a match against 4-4-2's from the same side): 4-3-3 +0.14, 3-5-2 -0.12, 5-3-2 +0.07, 3-4-3 +0.07, 4-5-1 -0.42, 5-4-1 -0.37. On the weaker side (55 v 65): +0.17, +0.03, +0.16, +0.08, -0.08, -0.09. So the one-forward shapes cost a favourite much and an underdog little.

Your done-when (no shape more than a few hundredths above 4-4-2) is not met yet: a forward is still worth about 0.1-0.2 points a match over a midfielder. Your sweep at v10, with points and shots, would tell me whether that is real or my sampling, and what a 4-3-3 still gets for free.
