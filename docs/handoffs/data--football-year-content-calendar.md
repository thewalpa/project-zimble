---
to: data
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Finish the content side of the existing football-year item

## What exists
Competitions' ScheduleVersion 4 supports explicit round kickoffs and midweek cups during the qualifying leagues' next season. Cup rounds are spread evenly after matchdays `ceil(r * leagueRounds / cupRounds)`, four days after each selected matchday, with the shortest qualifying league defining `leagueRounds`. Cup qualifiers must share their first kickoff and round interval. Load and restore reject calendars that overlap the summer transfer window or reach the player-year task before the next contract-year end, including play-offs and cup finals. A four-day margin covers anniversary weekday/leap drift.

`TestFootballYearFromAugustToMay` runs two seasons with three-week league intervals: leagues end in May, play-offs follow a week later, and cup edition 1 plays in November 2026, February 2027 and May 2027. Save/load and long versus short stepping agree. Competition work leaves the world fingerprint unchanged.

## What is needed
The built-in leagues still use `RoundInterval: sim.Week`. Complete the existing content portion of the August-to-May backlog by selecting the effective round spacing (three weeks is supported and tested). Bump the appropriate content version for changed definitions; pinned definitions are never replaced on restore. Review cup timing metadata: `FirstRoundDelay` and `RoundInterval` are retained legacy fields used by ScheduleVersion 3, while version 4 derives kickoffs from next-season league matchdays. Their comments now say so; removing/replacing pinned fields would need a schema bump and a new fixture. A configurable midweek offset/calendar definition can be agreed separately rather than silently assigning new semantics to the legacy fields.

## Done when
New careers' league seasons run from August to May, existing content-version pinning remains explicit, and calendar validation accepts the chosen defaults. Notify squad, balance and ui when league spacing changes. This session implements no further data backlog item.
