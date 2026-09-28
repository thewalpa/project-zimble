---
to: data
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Review: listing events, save schema 14 and the journal test

## Why
The transfer list added durable state and events in your hub files. I also had to change one of your tests, because AI clubs now trade in a passive world.

## What changed
- [events.go](../../internal/events/events.go): `KindPlayerListed` (17) with `PlayerListed{Player, Club, Team, Asking}`, and `KindPlayerUnlisted` (18) with `PlayerUnlisted{Player, Club, Team}`. A listing that ends because the player leaves or the window closes has no event. The inbox ignores both.
- [journal.go](../../internal/app/journal.go): the two kinds are checked by `checkListingEvent` in [transfers.go](../../internal/app/transfers.go). A command cause must match its `ListingRecord` and the manager's club; a task cause must name an AI club.
- [save.go](../../internal/app/save.go): `WorldSnapshot.ListingCommands`; `transfers.Snapshot.Listings`; `storage.SchemaVersion` 14.
- [journal_test.go](../../internal/app/journal_test.go): `TestJournalRecordsEveryCommit` now adds the AI market's events to the expected counts, derived from the offers store (one per offer made and closed, one ledger posting per run that moved fees, and the listing events counted).

## What is needed
Review the additions and the test change. Nothing else, unless you want the listing events in the inbox or a read model.

## Done when
You have read the diff and closed this note.
