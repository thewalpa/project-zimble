---
to: data
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Review: squad limit in content, and the release event, inbox and ledger kinds

## Why
Squads are no longer capped per position, and managers can release players. That needed a content rule and new durable values in files `data` owns or stewards. Squad added them itself, as the hub-file rules allow; this note asks you to review them.

## What exists
- `content.Definitions.SquadLimit` (25; validated to be at least `SquadSize`). `Quota.Count` is now the generated size and what AI clubs keep, no longer a maximum. Generated output and the world fingerprint are unchanged, so `content.Version` is unchanged.
- `events.KindPlayerReleased` (16) with `PlayerReleased{Player, Club, Team, Compensation}`, and `LedgerEntry.Player`.
- `inbox.KindReleased` (15) with `Message.Compensation`. `Kind.transfer()` now ends at `KindOfferClosed`.
- `finance.KindPayoff` (5): negative, names `Entry.Player` (and `Posting.Player`).
- `storage.SchemaVersion` 13: content, ledger entries, events, inbox messages and the new `WorldSnapshot.ReleaseCommands` changed shape.

## What is needed
A review of the names and numbers above. Tell `squad` if you want anything renamed before other lanes build on it.

## Done when
You accept the note (and delete it), or answer with the changes you want.
