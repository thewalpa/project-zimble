---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Releasing players and the squad limit in the web client

## Why
The manager can now release players, and a squad is no longer capped per position: it may hold up to `SquadLimit` (25) players in all, at any positions, while keeping each position's minimum. The web client still enforces the old per-position cap and has no release action.

## What exists
- `World.ReleasePlayer(ReleasePlayer{ID, ExpectedRevision, Player}) (PlayerReleased, error)` in [contracts.go](../../internal/app/contracts.go). Errors: `ErrNotUserPlayer`, `ErrSquadMinimum`, `ErrCannotAfford`, `ErrSquadsLocked` (a matchday is pending).
- `SquadPlayer.Payoff`: what releasing an employed player costs now (the rest of his contract), for a preview.
- `World.Content().SquadLimit` and `Content().Roster[i].Min`. `ErrSquadFull` now means the whole squad is at the limit.
- Inbox `inbox.KindReleased` (15), with `Player`, `PlayerName` and `Compensation`. Ledger kind `finance.KindPayoff` (5), `Entry.Player` names the player.
- `cmd/play` is done in the same change (squad touched your files; please review the wording): `release ID` previews the payoff, `release ID yes` releases, `squad` states the limit and minimums, the inbox line is `release: NAME left the club as a free agent; you paid X`, and ledger lines show `contract payoff P60`.

## What is needed
In `cmd/web`:
- A Release action per player on the Squad page (a form posting the player; show `Payoff` and ask for confirmation, since it cannot be undone).
- `Room` on the Free agents and Transfers pages must be `len(squad) < Content().SquadLimit`, not `counts[pos] < Quota(pos).Count` ([views.go](../../cmd/web/views.go), [transfers.go](../../cmd/web/transfers.go)). The message "Your squad is full at POS: you can buy only where you have room" becomes one about the whole squad.
- The `KindReleased` inbox text, and the payoff's player on the Finances page.

## Done when
On seed 42, club 3: the Squad page releases a player after confirmation and the Finances page shows the payoff; with 20 players you can bid for a forward (the squad already has 4); at 25 players the Bid buttons are disabled.
