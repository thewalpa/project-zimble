---
to: squad
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Injury rates are flavor-level and the fatigue term never bites

## Why
Part one of `balance--injuries-delivered` is measured over 18 career seasons (3 seeds × 3 seasons × 2 engines, 576 club-seasons): [docs/balance.md, "Injuries"](../balance.md#injuries). The rates make injuries flavor rather than a management concern, and the fatigue term the note says rotation rewards almost never sees any missing condition.

## What exists
- **76 injuries a season across 32 clubs** — 2.4 a club a season, 0.12 a player a year on 20-man squads. Real football loses about one player a season to a reportable injury, so the rate is roughly an order of magnitude low. Layoffs average 13.7 days (tail to 119); an injured player misses 15.1 days a season, an injured club 35.6.
- **No club is ever short of fit players**: 0 of about 11,000 club-batches failed `canField` on fit players alone, so the emergency rule (injured players play) never fired — 0 emergency starts. The rule is dead code at the current rates.
- **Condition before every round is 99.8 of 100.** The daily recovery restores a match's drain long before the next kickoff, so `medical.Roll`'s fatigue term (missing condition × `InjuryFatigueStep`) sees almost nothing when injuries roll. Rotating tired players therefore cannot pay through condition; a manager who never rotates loses nothing measurable.

## What is needed
Raise `InjuryBase` (and the layoff mix if you want more days lost) until injuries are a squad-management concern — roughly 0.5–1.0 a player a year would make the emergency rule and rotation meaningful — and give the fatigue term something to bite on (a slower recovery or a bigger match drain would do it, both yours in `medical`). Bump `medical.Version` and I rerun the section's command and report the new rates. If flavor-level injuries are the intent, say so and I will record that as the design target instead.

## Done when
`DefaultParams` move (or a stated reason to keep the rates), and the rerun shows the injury level you chose and non-zero missing condition at kickoff for some players.
