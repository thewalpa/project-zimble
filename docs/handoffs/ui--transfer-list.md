---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-28
---

# The transfer list in the web client

## Why
Clubs can now put players on a transfer list at an asking price, and AI clubs bid for listed players when they need one at that position. AI clubs no longer bid for the manager's players unless the manager lists them, so listing is now the only way the manager sells. The web client has no way to list a player yet.

## What exists
- `World.ListPlayer(ListPlayer{ID, ExpectedRevision, Player, Asking}) (PlayerListed, error)` in [transfers.go](../../internal/app/transfers.go). `Asking` zero takes the player off the list; a new price relists him. Errors: `ErrNotUserPlayer`, `ErrWindowClosed` (listing needs a run left before the close, like a bid), `ErrNotTransferable` (he moved in this window), `ErrSquadMinimum` (his position is at its minimum, so no club could buy him), `ErrInvalidPrice` (negative), `ErrNotListed` (unlisting an unlisted player). A listing ends by itself when the player is sold or released, or the window closes.
- `World.TransferList() []ListedPlayer`: every listed player (both AI and the manager's), with `SquadPlayer`, `Club`, `ClubName` and `ListedAt`. `Value` is the asking price.
- `SquadPlayer.Listed`; `SquadPlayer.Value` is now the asking price for a listed player, and the fee his AI club accepts.
- Events `PlayerListed` (17) and `PlayerUnlisted` (18). No inbox kind; bids for listed players arrive as the existing `KindBidReceived`.
- `cmd/play` is done in the same change (squad touched your files again; please review the wording): `list` shows the transfer list, `list ID [PRICE]` lists one of yours (default: his value), `unlist ID`, your listed players under `transfers`, and a `listed` mark in `market`.
- I also updated seeded expectations in your tests (`cmd/play`, `cmd/web`, `cmd/simulate`): AI clubs now trade in the first window, so names, cup winners and offer IDs moved. `TestTransfersInTheBrowser` lists Elias Gallo through `c.s.w.ListPlayer` as a stand-in for the missing form; please replace it with a POST once the form exists. In `TestReleaseAndSquadLimitInTheBrowser` (which landed while I worked), bids now skip players the AI market has already moved or bid for, and the free agent kept for the "squad full" check is goalkeeper 43, because AI clubs sign free agents for their vacancies during the window.

## What is needed
In `cmd/web`:
- On the Squad page, a List action per player with an asking-price field (default `Value`), and Unlist for listed players. Show which players are listed.
- On the Transfers page, a Transfer list section built from `TransferList()`, with Bid forms for other clubs' listed players (the default fee is the asking price), and the manager's own listed players.
- Mark listed players in the market by position.
- Explain that the manager receives bids only for listed players.

## Done when
On seed 42, club 3, at the career start: listing Callum Ibsen (56) at 700,000 on the Squad page brings "Hollowick Town bid 700,000.00 for Callum Ibsen" at the next Continue. A day or two later the Transfers page shows AI clubs' listed players with Bid buttons. Listing a player at his position's minimum shows the refusal.
