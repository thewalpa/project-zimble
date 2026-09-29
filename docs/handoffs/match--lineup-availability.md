---
to: match
from: ui
status: open
blocking: no
created: 2026-09-29
---

# Expose lineup eligibility for the squad picker

## Why
The UI backlog calls for an “available only” filter in both lineup editors. `SquadPlayer.DaysOut` shows injuries, but the app owns the fit-player and emergency selection rule, so clients should not infer eligibility from the injury count.

## What exists
- `World.Squad(club)` returns `SquadPlayer`, including `DaysOut`.
- `World.SubmitLineup` rejects injured selections unless the emergency rule permits them, and explains the rejection.
- `World.availableSquad` and `World.canField` implement the selection rule in app-owned lineup code.

## What is needed
Expose a read-only app query for the user club’s squad that marks which players may be selected for the pending fixture, including any emergency-rule exceptions. The terminal and web clients will use it to offer an “available only” filter and make any emergency exception clear.

## Done when
Both lineup editors can filter by the app-provided eligibility result without duplicating fit or emergency rules; tests cover normal and emergency selection.
