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
