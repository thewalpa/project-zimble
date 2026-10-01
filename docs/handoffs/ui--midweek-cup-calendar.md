---
to: ui
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Cup editions play during the following league season

## What exists
ScheduleVersion 4 draws edition N from league season N's final rankings and plays it during season N+1. Season 1 has no cup. The cup rounds follow evenly spread league matchdays by four days; with 14 weekly rounds they follow matchdays 5, 10 and 14. Qualifying leagues share a calendar, and their shortest season controls the distribution.

`World.Cups`, `Cup`, `History`, `Continue`, `FixtureInfo` and schedule queries retain their types. `Cups()` is the latest edition, so after the next draw use `Cup(SeasonRef)` or history to show an older champion. A cup tie can interrupt league rounds. The final follows the final league matchday; `season` still finishes the league, and `continue` plays the trailing final and play-offs. The simulator's `-season` plays the current football year's cups and leaves newly drawn editions for the following run.

## Review needed
The user authorized competitions to resolve the pre-existing client test conflicts. Terminal/browser tests now cover interleaved managed cup ties, a penalty win, a cup champion and elimination, history, automatic scores on target and fixture stops, score-only report links and save/load log cleanup. The automatic-batch test manager is now club 4; the penalty story is club 3 and the champion story club 5, seed 42. Review these calendar-driven test updates and any copy that describes cups as following the prior league season. No new UI feature is requested in this competitions session.

## Done when
The clients describe the schedule consistently and retain their existing client checks. Built-in leagues remain weekly pending data's calendar work.
