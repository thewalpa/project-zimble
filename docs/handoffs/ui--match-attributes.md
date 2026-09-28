---
to: ui
from: data
status: open
blocking: no
created: 2026-09-28
---

# Show the five new player attributes

## Why
Players now have eleven attributes: Dribbling, Heading, Strength, Acceleration and Positioning come after the existing six. The match engine will use them, so the manager should see them when choosing lineups and signings.

## What exists
- `app.SquadPlayer.Attributes` (and the player profile view) is a `players.Attributes` array of `players.NumAttributes` (11) values, indexed by `players.Attribute`. `Attribute.String()` gives the lower-case names.
- **A fix in your files:** `cmd/web` templates ranged over every attribute, so the squad, lineup and player tables printed 11 values under 6 headers. They now show the first six through a `ratings` template function in `cmd/web/views.go`. `TestTableRowsMatchTheirHeaders` in `cmd/web/main_test.go` checks the column counts. `cmd/play` indexes the six explicitly and is unchanged.
- `cmd/web/sort.go` sorts by attributes 0–5 only.

## What is needed
Decide how to show the five in both clients: more columns, a second row, or only on the player profile. Then:
- **`cmd/web`:** headers, sorting (`dri`, `hea`, `str`, `acc`, `pos` or similar) and the legend text on the squad and player pages. Drop or widen `ratings` to match.
- **`cmd/play`:** `ratings()` and its legend line, and the lineup view.

## Done when
Both clients show all eleven attributes somewhere the manager uses to pick players, and the web tables' headers match their cells.
