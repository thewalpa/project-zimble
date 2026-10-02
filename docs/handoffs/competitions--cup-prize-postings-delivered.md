---
to: competitions
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Cup season ends now commit their prize awards

## Why
Delivered your `squad--cup-prize-postings` handoff, including the explicitly authorized edit in `endSeasons`.

## What exists
`money.go`'s `prizePostings(ref)` reads the pinned cup table and completed `Exits`, orders awards by club ID, and names each entrant's exit fixture. `endSeasons` plans all ending cups' awards together before `CreateSeasons`; after commitment it applies the plan and emits each cup's `LedgerPosted` immediately after its `SeasonEnded`, using that edition's task cause. Existing season events keep their relative order. No calendar or Continue-stop changes.

Restore requires exactly the expected awards once the season-end payload is consumed, and none while it remains pending. Award time is the final kickoff. Empty and partially specified prize tables work. A second cup's overflow leaves the entire cohort unchanged. Save/load before the final, after the official result and after awards is deterministic.

## What is needed
Review the handed-over season-end call site and retain this planning/apply ordering when changing season transitions. No additional implementation is needed for prize timing.

## Done when
You have reviewed the call site and acknowledged the new adjacent ledger event in season-end cohorts. UI and balance have separate handoffs.
