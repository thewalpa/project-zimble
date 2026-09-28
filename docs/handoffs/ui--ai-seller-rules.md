---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Why an AI club rejects a bid at its price

## Why
AI clubs now weigh more than the fee (`ai.TransfersVersion` 4). A manager whose bid at the shown price is rejected needs to know why, and ideally before bidding.

## What exists
All in [internal/app/transfers.go](../../internal/app/transfers.go) and `ai.AcceptBid` in [internal/ai/transfers.go](../../internal/ai/transfers.go). A refused bid is rejected at the next run like a bid below the price (`OfferClosed`, rejected); commands and events are unchanged.

- **Prices.** `SquadPlayer.Value` of an AI club's unlisted player is now its selling price: its valuation plus 6% for each point he rates above the club's squad average (`ai.SellingPrice`). A club's best players cost two to three times their valuation. Listed players still cost their asking price; the manager's own players show their valuation.
- **Stars choose their club.** A player rated `ai.StarMargin` or more above his club's squad average joins only a club at least as strong: `ai.Joins(overall, sellerAverage, buyerAverage)`. The averages are `ClubSummary.AverageOverall` from `World.Summary()`. This holds for the manager's players too: a listed star draws bids only from clubs at least as strong as the manager's.
- **Settling in.** An AI club doesn't sell on an unlisted player it bought in the previous window.
- **Late in the window.** From `World.TransferWindow().NeededClose` (three days before the close) an AI club sells only players it can spare: listed ones, or at a position where it holds more than its roster count.

- **`World.BidRefusal(player)`** returns why the player's AI club would refuse a bid made now at its price: `RefusalStar`, `RefusalSettling`, `RefusalNeeded`, or `RefusalNone`. It is read-only and evaluates the rules above against the current squads.

## What is needed
In both clients' market views (`cmd/play/transfers.go`, `cmd/web/transfers.go`):
- mark the players `BidRefusal` refuses, with the reason ("won't join a weaker club", "just signed", "needed until the window closes"), and say it before the manager bids;
- from `NeededClose` on, say that AI clubs sell only listed players and players they can spare;
- the bid form can keep the price as its default fee: it already is the selling price.

## Done when
With `go run ./cmd/play -seed 42 -club 3`, the market marks the stars of stronger clubs as unwilling, and continuing to three days before the close shows the notice; the web view shows the same.
