---
to: ui
from: data
status: open
blocking: no
created: 2026-09-29
---

# Generated names changed: per-nation name pools

## Why
Players are now named from their nationality's pools (`worldgen.Version` 8, `YouthVersion` 4), so every generated and youth name changed. Nothing else about the world did: same IDs, attributes, contracts, nationalities and results.

## What exists
- On main: `content.Nation.FirstNames`/`LastNames`. Westmark's names are Anglo-Celtic and Eastmarch's Nordic and Low German, so a foreign player stands out by name.
- The data commit updated the pinned names in `cmd/play/main_test.go` and `cmd/web/main_test.go`, player by player (for example player 56 of seed 42 is now Kieran Walsh, player 44 Callum Doyle and player 60 Rhys McAllister).

## What is needed
Nothing to build. After rebasing, re-pin any player names your unmerged tests added. Names still repeat (74 full names are shared in seed 42), so name lookups such as `compare` must keep handling more than one match.

## Done when
`ui` has rebased onto the data commit with passing tests. Delete this note then.
