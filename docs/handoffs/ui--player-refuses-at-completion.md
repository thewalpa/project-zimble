---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Say when a player refuses to join, and when AI clubs may sign free agents

## Why
A player's consent (`ai.Joins`: a star joins only a club at least as strong) is now checked when a transfer completes, for every seller (audit PAR-04). An accepted offer whose player refuses closes with a new status, and both clients currently word it as a collapse. Separately, the manager asked for the date from which AI clubs may sign free agents (was `squad--free-agent-reservation-instant.md`).

## What exists
- `transfers.StatusRefused` (6): "accepted, but the player refused to join the buyer at completion". It is the `Status` of `OfferView`, `OfferAnswered.Status` from `RespondToOffer(Accept: true)`, and the `Outcome` of an `inbox.KindOfferClosed` message. `StatusCollapsed` (5) now means only the buyer or seller could no longer complete.
- The manager's bid for an AI club's star now ends `refused`, not `rejected`, when the star won't join (`BidRefusal` still predicts it as `RefusalStar`). An AI bid the manager accepts ends `refused` if the squads have changed since and the player no longer agrees; nothing moves.
- `TransferWindow.FreeAgentsOpen` (`sim.GameInstant`): until then only the manager signs free agents for an AI club's vacancies above its roster minimum; AI clubs sign them from the first transfer run at or after it (the middle of the window).

## What is needed
In both `cmd/play` and `cmd/web`:
- After `accept`, a `StatusRefused` result: for example "Accepted, but Elias Gallo refused to join Eldhaven United." (the default branch currently says the buying club can no longer complete it).
- `outcomeName` / the inbox wording for `StatusRefused`: "the player refused to join" instead of "fell through".
- Replace "from the middle of the window" with the date: "until <FreeAgentsOpen> only you may sign free agents".

I updated your pinned test strings, since `ai.TransfersVersion` 6 moved the seed-42 careers: offer IDs 58→55 and 88→84, the year-2 youth player, the seed-42 cup winner (now Quillford FC when club 3 is managed), the seed-1 cup final opponent, and the web history's cup winner. `TestHistory` now manages club 3, because club 2 no longer wins anything. No client code changed.

## Done when
A refused acceptance and a refused bid read as refusals in both clients, and the free-agent date is printed; delete this note.
