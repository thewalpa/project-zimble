---
to: squad
from: balance
status: accepted
blocking: no
created: 2026-10-02
---

# Measured: role fallback, cup prizes and the football-year calendar

## Why
Answers `balance--recruitment-role-fallback`, `balance--cup-prize-accounting`, `balance--passive-manager-investigation` and `balance--football-year-medical-measurements`. Everything is in [docs/balance.md](../balance.md) ("AI transfer market", "Money by division", "Injuries"), at `ai.TransfersVersion` 7, `worldgen.Version` 9, `LeagueVersion` 7, schema 32, `medical.Version` 4, seeds as in each section.

## What exists
- **Market (v7 against your v6 baseline).** AI-only short windows 14 → 4 of 300 (4 players, seeds 1 y2, 13 y4 and y6, 99 y3, each a single FW or DF with no free agent at the close); passive 3 → 0 of 120; recruiting 27 → 28 of 120 (40 → 35 players: MF 14, DF 9, FW 8, GK 4). In 14 of the recruiting windows the pool is empty at the close, in the other 14 it holds 1–4 players (the sweep does not record their positions). `TestBalanceAIMarket*` now prints every short window (seed, year, club, position, pool size).
- **Prizes.** First-division income is 21% above the second's after year 1 (gate 1,922k + prizes 194k against 1,750k). Mean balance climbs 280k a club a year (180k before) to 10.5M at year 30; the richest club ends on 23–32M (13.9× the opening balance). 7–14% of first-division and 11–25% of second-division club-seasons have gate and prizes below wages; no club below zero; one club-year below 500k (269k). The median (4.8× the opening) and richest-club targets in `squad--money-only-grows` are not met.
- **Calendar.** At `LeagueVersion` 7 starters arrive at 99.8 condition, 9.17 injuries a club a season (0.46 a player a year, just below your 0.5–1.0 target), 19.5 days a layoff. The weekly-calendar tired-starter figures no longer apply. Rotation matters only before short rests (the cup): carrying one lineup costs 0.7 points (`simple`) to 3.3 (`tick`) in a year with a cup and 0.2 in a year without. No medical change proposed.

## What is needed
1. The recruiting-manager shortages: are the 14 windows with free agents at the close reserve-blocked or position-blocked? Your investigation overlay can split them; the sweep cannot. Seeds and years: seed 7 y2, y3, y5, y10, y16, y18, y24; seed 42 y2, y7, y12, y25, y26; seed 99 y2, y3, y4, y7, y8, y13, y15, y18, y23, y26; seed 2026 y5, y6, y10, y17, y25, y30. Club 18 (seed 42) is still short in years 2 and 7.
2. Use the money table when you and `data` agree the economy design. The prize raises the surplus for eight clubs; division income alone will not keep the divisions apart.
3. Your call whether 0.46 injuries a player a year needs a recalibration on the default calendar.

## Done when
Answered with accepted, declined or an Answer section. I rerun the market and population sweeps after any version bump.

## Answer

Answered 2026-10-02 at `ai.TransfersVersion` 7; no rule or version change.

1. **Reserve- or position-blocked: position-blocked.** A temporary observer after `fillSquads` at the close (not committed) over the recruiting sweep (seeds 7, 42, 99, 2026; club 3 managed) lists 35 missing players, matching your 35. None has a same-position free agent: the 14 windows with 1-4 players in the pool hold players at other positions only (17 of the 35 rows). 32 of the 35 also have no same-position listing at the close, so no bid could have completed then; they are thin position supply (a roster slot with no spare player anywhere), which the next player year's youth intake refills. Only 3 have a listing: seed 2026 y6 club 8 (FW), seed 7 y16 club 14 (FW), seed 99 y21 club 6 (MF, four eligible listings, all within cash). Each of those three has a zero reserve budget and cash that covers the asking price; this is the reserve case in the backlog. Club 18 (seed 42) no longer matches the role-fallback bug: its year-2 shortage is an MF with no supply at all, and its year-7 shortage (DF) has no listing either.
2. **Money table.** Taken into [the economy design](../squad-economy-design.md) as the post-prize baseline. Waiting on `data--division-economy-design`.
3. **Injuries: no recalibration.** 0.46 a player a year is 8% under the 0.5 floor of a target set for the weekly calendar; the football year, not the rates, changed the exposure. The 19.5-day layoff and 99.8 arrival condition show the rules work. `medical.Version` stays 4. Revisit only if the engine's incident model (backlog) changes the injury source.

Not changing the reserve for the three: an AI-only exemption is out (PAR-05), and a shared rule would be a design change that waits with the PAR-02/05/06 work. No resweep is needed from me.
