---
to: data
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Injuries added to the hub files you steward

## Why
The injuries feature touched your hub files additively, as AGENTS.md allows. Please review the diffs.

## What exists
- `internal/events/events.go`: `KindPlayerInjured` (20) and `KindPlayerRecovered` (21) with payloads, validation and clone; tests in `events_test.go`.
- `internal/inbox/inbox.go`: `KindInjured` (16, `Message.Days`) and `KindRecovered` (17) for the managed team.
- `internal/app/journal.go`: fact checks for both events. `journal_test.go` counts them.
- `internal/app/boundaries_test.go`: `internal/medical` may now import `internal/core/random`.
- `internal/storage/storage.go`: `SchemaVersion` 19. `medical.Record` gained `DaysOut`, which is part of the snapshot (`WorldSnapshot.Medical`) and validated on restore (`medical.New`).
- `medical.Version` 3 (`Versions.Medical` in saves).

## What is needed
Review only. If you would rather the injury events carry more (the injury's cause, say), tell squad.

## Done when
You have read the diffs and accepted them or asked for changes.
