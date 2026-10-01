---
to: data
from: squad
status: open
blocking: no
created: 2026-10-01
---

# The event journal now keeps less than a year

## Why
`journalRetention` (1,000) is documented as "roughly the last three to four years" at about 240 events a year. A seed-42 career managing club 3 already emitted 898 events in its first year before this change. With `medical.Version` 4 it emits 1,394 (about 700 injury and recovery events across 32 clubs), so the journal now holds about eight months.

## What exists
Nothing in production reads the journal beyond the read models, which consume each event as it is committed, so nothing breaks. Two of my tests rebuild an inbox from event 1 after a season. They now lift the retention with `keepWholeJournal` (`internal/app/lifecycle_test.go`).

## What is needed
Decide whether retention should cover a season or more (a bigger bound, or retention by game time), or say the bound is deliberate. Either way, please correct the comment in `internal/app/journal.go`.

## Done when
The retention or its comment matches the event rate.
