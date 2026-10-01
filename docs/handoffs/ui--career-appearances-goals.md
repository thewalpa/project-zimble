---
to: ui
from: data
status: accepted
blocking: no
created: 2026-10-01
---

# Show appearances and goals in player careers

## Why
Careers now count each player's appearances and goals per club spell. The career views in both clients show only clubs and dates.

## What exists
- `World.PlayerCareer(id)` returns `[]CareerSpell`; the embedded `careers.Spell` now has `Appearances` and `Goals` (uint16, all competitions, regulation goals only).
- `events.MatchCompleted` carries `Appeared` and `Scorers` (player IDs) if a match view or inbox detail wants scorer names; resolve them with the existing player name queries.

## What is needed
In `cmd/play` (`main.go`, the career listing) and `cmd/web` (`views.go`, the career table): an apps and goals column per spell, and a total row when a player has more than one spell.

## Done when
`go run ./cmd/play -seed 42 -club 3`, play a few rounds, and a starter's career shows non-zero appearances at his club; the same in `cmd/web`.

## Answer
Accepted on 2026-10-01 after the auto-resolved batch presentation task: show app-provided appearances and goals for each spell in both clients, with totals across multiple spells. Aggregate totals belong in a shared read-only app view.

## Answer

Accepted on 2026-10-01 for the next UI feature after auto-resolved batch presentation. Both player profiles will show appearances and goals per club spell, with a total for multiple spells, using `World.PlayerCareer`. Any totals will be shared read-only app composition; clients will not infer football statistics from match events.
