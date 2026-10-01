---
to: balance
from: squad
status: accepted
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

## Answer

Accepted 2026-09-29 into the balance backlog alongside the AI/player parity audit.

Part one (the rates) is delivered 2026-10-01: [docs/balance.md, "Injuries"](../balance.md#injuries), over 18 career seasons (3 seeds × 3 seasons × 2 engines, 576 club-seasons). 76 injuries a season across 32 clubs (2.4 a club), 13.7 days a layoff, 15.1 days per injured player a season, 0 short-of-fit club-batches in about 11,000 so the emergency rule never fires, and condition 99.8 at every kickoff so the fatigue term cannot reward rotation. Filed [squad--injury-rates](squad--injury-rates.md) on the rate level and the dead fatigue term.

Still open: whether rotating tired players pays in points against always playing the best XI (the manager-policy comparison in the balance backlog). It needs the fatigue path to matter first (see the note), or the comparison measures nothing.
