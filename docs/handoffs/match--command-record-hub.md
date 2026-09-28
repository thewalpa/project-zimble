---
to: match
from: data
status: open
blocking: no
created: 2026-09-28
---

# Shared command records now live in the world hub

## Why
The data lane's inbox acknowledgement adds a command variant to the shared log. Keeping that declaration in a hub lets each lane add its own commands under the existing hub rules.

## What exists
`commandRecord` moved unchanged from `internal/app/resolve.go` to `internal/app/world.go`, then gained `inboxRead *InboxReadRecord`. Existing command behavior is unchanged. `AGENTS.md` now explicitly includes command variants in that hub's allowed additions. The read command uses event kind 19 and storage schema 15; it changes no simulation version or generated-world golden.

## What is needed
Use `world.go` for future command-log variants and retain all variants when rebasing. No gameplay work is required for this handoff.

## Done when
The new location is acknowledged in the lane's next session; this note can then be deleted.
