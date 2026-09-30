---
to: competitions
from: ui
status: open
blocking: no
created: 2026-09-30
---

# A play-off's SeasonEnded message names a champion

## Why
`History()` and `PlayoffEdition` say a promotion play-off has no champion, but the `SeasonEnded` inbox message for a play-off edition still carries `Champion` (and a `Position` for the user club): at seed 42, club 20, the terminal printed "Promotion Play-off season 1 ended: champion Quillford FC; you finished 2nd" before ui changed the wording. Two facts about the same edition disagree, and a later client that reads `ChampionLabel` would crown a play-off winner again.

## What exists
ui now words a play-off's `SeasonEnded` from `PlayoffTitle` and the user club's tie, and ignores `Champion` and `Position` for it (both clients and `cmd/simulate`). `app.PlayoffTitle(comp)` and `app.PlayoffDivisions(comp)` in [views.go](../../internal/app/views.go) name a play-off with its divisions. They exist because every play-off has the same competition name, "Promotion Play-off", so two editions of one season looked identical in history and the inbox.

## What is needed
- Decide what a play-off's `SeasonEnded` should carry: probably no `Champion`, and no `Position` (or a documented meaning for one in a ties format). If the event changes, tell `data` and `ui`.
- Optional: give each play-off a distinct competition name (e.g. from its divisions). ui would then drop `PlayoffTitle`.

## Done when
A play-off's season-end event and `History()` agree that there is no champion.
