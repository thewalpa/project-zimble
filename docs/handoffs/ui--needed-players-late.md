---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Late in the window AI clubs keep the players they need

## Why
An AI club now rejects a bid for a player it needs when the answer comes too late for it to replace him (`ai.TransfersVersion` 3). A manager bidding in the window's last days sees the rejection without knowing why.

## What exists
- `World.TransferWindow().NeededClose` ([internal/app/transfers.go](../../internal/app/transfers.go)): bids for a player his AI club needs (it holds no more than its roster count at his position) must be made before it, three days before the close with the default content. Later bids for such players are rejected at the next run like a bid below the price (`OfferClosed`, rejected). Listed players and players a club holds beyond its roster count are sold until `BidsClose` as before.
- Nothing else changed in the commands or events.

## What is needed
In both clients' transfer views (`cmd/play/transfers.go` and `cmd/web/transfers.go`, next to the `BidsClose` sentence): from `NeededClose` on, say that AI clubs sell only listed players and players they can spare. Optional: mark in the market list which players their club can spare (the squad counts are already public through `Squad`).

## Done when
With `go run ./cmd/play -seed 42 -club 3`, continuing to three days before the close shows the notice in the transfer view; the web view shows the same.
