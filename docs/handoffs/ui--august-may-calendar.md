---
to: ui
from: data
status: open
blocking: no
created: 2026-10-02
---

# Built-in leagues now run from August to May

## What exists
`content.LeagueVersion` 7 spaces the fourteen built-in league rounds three weeks apart. Season 1 runs from 9 August 2025 to 9 May 2026; play-offs follow on 16 May. Cup edition 1 follows season 2's league matchdays 5, 10 and 14 on 4 November 2026, 17 February 2027 and 12 May 2027. `World.Schedules`, `Agenda`, `Cup`, `History`, `Continue` and their types are unchanged. Saved careers keep their pinned calendars, including weekly leagues.

## Review needed
Calendar expectations in all three clients blocked the data checks, so data made small test-fixture updates only. Terminal/browser penalty stories now manage Ironbridge Wanderers (seed 42, club 12): a 1–1 quarter-final against Quillford won 4–3 on penalties, then a semi-final exit. Saltmere Athletic (club 5) still wins its managed cup, now against Ironbridge in the final. Automatic-batch stories remain club 4; promotion uses seed 7, club 21, finishing second and winning its tie. History for managed Quillford names Greyfen United. Simulator calendar/demo assertions reflect the May finish and two batches by 6 September, with save/load still matching uninterrupted play.

Review these fixtures and client wording about when a cup starts: it is drawn after qualifying leagues finish and plays during the following league season. No production client behavior changed.

## Done when
Both clients describe the new default schedule consistently, retain penalty/champion/exit/promotion and automatic-result coverage, and display a restored weekly career from its pinned definitions.
