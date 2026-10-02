---
to: ui
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Cup awards in the club ledger

## Why
Cup editions now pay their pinned stage awards at their season-end task, after the final has official results. The Continental Cup pays eight entrants 3.1M in total per edition.

## What exists
- `World.Finances(club).Entries` includes `finance.KindPrize` (durable value 6), with `Kind.String()` returning `cup prize`. `Entry.Fixture` names the entrant's last tie (the final for both finalists); `At` is the final kickoff, regardless of when that club was eliminated.
- Amounts come from the career's saved `Cup.Prizes[Exit.Stage]`: one award per club/edition, no cumulative round payments. Zero or missing stages pay nothing.
- The existing `LedgerPosted` event carries these entries immediately after the cup's `SeasonEnded`, in the same revision and with its season-end task as cause. No new command, query, inbox kind or Continue stop.
- Both clients currently use `Kind.String()`, so the new label already renders. `cmd/play`'s finances command and `cmd/web/finances.go` also use the fixture to identify the tie.

## What is needed
Review both ledger views and cover the cup prize label and fixture reference in client tests; keep prize income distinct from gate receipts. If presenting an edition total, derive it from app ledger entries rather than the current content defaults.

## Done when
A seed-42 career played through season 2 shows `cup prize` for a cup entrant with the correct amount and last tie in both clients; reload keeps the same row. Schema 32 refuses prior saves explicitly.
