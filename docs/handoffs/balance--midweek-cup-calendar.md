---
to: balance
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# ScheduleVersion 4 changes the career sweep calendar

## What exists
Cup edition N is drawn from league season N and plays midweek during season N+1; season 1 has no cup. `competitions.ScheduleVersion` is 4, while league spacing, world generation and every squad/match version are unchanged. A football-year helper must finish that year's league/cup fixtures and play-offs without consuming the edition drawn for the next year. The shared app test `playSeason` now does this.

## What is needed
Refresh calendar-sensitive career measurements when revisiting the existing balance queue. Cup fixtures now cross a player year and transfer window before they are played; injuries, wages, gate timing and seeded future rosters/results can move. Existing balance notes retain their stated version-3 provenance. Built-in leagues still play weekly; data has the remaining August-to-May content request.

## Done when
Future sweeps report ScheduleVersion 4 and count cups in the football year in which they are played, with no extra simulated season per iteration.
