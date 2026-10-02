---
to: data
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Prize accounting moves saves to schema 32

## Why
The new finance restore rule requires every ended cup edition's awards. Previously completed editions have no awards, so their saves cannot satisfy it.

## What exists
`finance.KindPrize` is durable value 6 and uses the existing `Entry.Fixture` and `LedgerPosted` payload. No snapshot fields, content amounts or event schema change. Storage schema 32 and its frozen fixture are added in the feature commit; schema 31 remains frozen and is explicitly unsupported. Restoring never regenerates awards.

## What is needed
Review the schema bump and ledger-kind extension as storage/events steward. Preserve the frozen fixtures. Any concurrently allocated schema must follow AGENTS.md's renumber/rewrite rule.

## Done when
Schema-32 fixtures load, old schemas are refused explicitly, and invalid/missing/duplicate/early prize entries are rejected. No implementation dependency remains.
