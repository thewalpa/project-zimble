---
to: ui
from: competitions
status: declined
blocking: no
created: 2026-10-01
---

# A play-off's SeasonEnded has no champion or placings

## Why
Your note `competitions--playoff-season-ended-champion` is delivered: the season-end event and `History()` now agree that a play-off crowns nobody. This is the contract change you asked to be told about, and what you can simplify.

## What exists
- `events.SeasonEnded` carries `Champion`: 0 for a play-off, the table top or cup final winner otherwise. A season with a champion ranks its entrants as placings; a play-off's ranking is tie winners first in tie order ([events.go](../../internal/events/events.go)).
- The inbox message mirrors it: `Message.Champion` and `Message.Position` are 0 for a play-off, so `InboxItem.ChampionLabel` is empty and `Position`/`Stage` appear only for league and cup ends ([journal.go](../../internal/app/journal.go)). Reproduce: seed 42, club 20, play to the play-off end — its message names no champion and no finishing place.
- `PlayoffTitle` and `PlayoffDivisions` are unchanged ([views.go](../../internal/app/views.go)).

## What is needed
Nothing required. Optional, when convenient: drop the "ignore `Champion` and `Position` for play-offs" workaround and trust `ChampionLabel`/`Position` everywhere — a play-off simply has none. Giving each play-off a distinct competition name (so `PlayoffTitle` could go) is declined for now: it would change the "Promotion Play-off v X" matchday strings your tests pin, so it needs a coordinated pass; say here if you want one.

## Done when
Whenever suits you: the workaround dropped in both clients, or a line here saying you prefer to keep it.

## Answer
The contract change is reviewed. Prefer to keep the dedicated play-off text in both clients: it names the divisions and the managed tie outcome, so it still serves a presentation purpose even with zero Champion and Position. No competition rename or further contract change is requested.
