---
to: ui
from: data
status: open
blocking: no
created: 2026-09-28
---

# Show unread messages and mark them read

## Why
Inbox read state now survives saves and journal retention. Both clients can distinguish new messages from acknowledged ones.

## What exists
- `World.Inbox()` returns `InboxItem.Read` (promoted from `inbox.Message`); identity stays `InboxItem.Event`.
- `World.UnreadInboxCount()` counts unread messages in the retained inbox (up to 200). Both queries are read-only.
- `World.MarkInboxRead(app.MarkInboxRead{ID: w.NextCommandID(), ExpectedRevision: w.Revision(), Message: item.Event})` returns `app.InboxRead` (`Command`, `Revision`, `At`, `Message`). One command acknowledges one message. An already-read retained message succeeds, while an exact retry returns its original result even after eviction.
- Unknown, evicted or non-message event IDs return `ErrNoSuchMessage`; normal command ID/revision rules apply. No managed club is required for league-wide messages.
- `events.KindInboxRead = 19` updates a read flag and creates no additional inbox item. Saves use schema 15; older schemas are explicitly refused with the existing `storage.ErrUnsupportedSave` path.

## What is needed
Show unread counts and read/unread styling in `cmd/play` and `cmd/web`; provide a mark-read action (or acknowledge explicitly opened messages). Use the command rather than changing flags locally. Persist successful acknowledgements using the clients' normal save flow. Listing/querying the inbox alone need not acknowledge it. A mark-all action, if useful, can issue one command per unread retained item using the revision after each command.

## Done when
New messages appear unread; acknowledging one changes its flag/count; save/reload retains it; later arrivals stay unread. Failed actions leave the view and save unchanged.
