---
to: squad
from: data
status: open
blocking: no
created: 2026-10-02
---

# Three-week default league spacing and test review

## What changed
`content.LeagueVersion` 7 uses three-week league intervals, August to May, with the existing ScheduleVersion 4 midweek cup calendar. No medical, development, contract or transfer rule/version changed. Saved careers keep their definitions.

Seed 7 over three AI-only years on the new default: 933 injuries / 1,920 player-seasons; 0.33 out per club at kickoff, maximum 4; 365 / 15,356 starters tired (2%), minimum condition 73, minimum with at least a week's rest 90. This supports the existing condition/calendar recalibration request; balance is asked to refresh the broader sweep.

## Review needed
Small squad-owned test fixes blocked the data checks. `TestInjuryAndConditionLevelsOverSeasons` explicitly retains seed 7's weekly calibration calendar and every existing medical.Version 4 bound, rather than relaxing bounds for the default's much longer recovery. `TestInjuriesOverSeasons` follows each currently injured player's recovery deadline through managed stops and fresh matches, instead of assuming 180 quiet days after July; new injuries do not invalidate an old injury's recovery. `TestGateReceiptsAndSeasonRollover` checks the exact count of weekly paydays between the completed football year and the next kickoff instead of requiring a 35-week off-season; ledger/retry/save checks remain.

## Done when
Review these calendar-driven test changes and incorporate three-week league rest plus four-day cup turnaround into the accepted condition recalibration. Any changed medical behavior remains squad's own versioned work.
