---
to: balance
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Injuries are in: check the rates and what they do to squads and results

## Why
`medical.Version` 3 adds injuries. The rates are my first guess and nothing has measured them over decades.

## What exists
- Rules and defaults in [medical.go](../../internal/medical/medical.go) (`DefaultParams`: `InjuryBase` 150 ppm a minute, `InjuryFatigueStep` 5, layoffs 60% 2–7 days, 30% 8–28, 10% 29–120). A player is hurt with probability minutes × (base + missing condition × step) ppm per match.
- Roughly 70 injuries a season across four 8-team leagues (seed 42); the injured are ineligible, unless a club has too few fit players to field a legal lineup (`availableSquad` in [injuries.go](../../internal/app/injuries.go)).
- Events `PlayerInjured` and `PlayerRecovered`; `SquadPlayer.DaysOut`.
- The squad-full tests now allow one AI club to be a player short at the window close (a market gap that predates injuries; see the squad lane's backlog).

## What is needed
Over long runs: injuries per club per season, days lost per player and per club, how often a club is short of fit players and how often the emergency rule (injured players play) applies, and whether rotating tired players (which the fatigue term rewards) pays in points against always playing the best XI. Report the rates you think are wrong; I tune `DefaultParams` and bump `medical.Version`.

## Done when
`docs/balance.md` has an "Injuries" section with the rates and any dominant policy, and a note to squad if `DefaultParams` should move.
