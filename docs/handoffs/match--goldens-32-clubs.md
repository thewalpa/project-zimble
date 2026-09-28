---
to: match
from: data
status: open
blocking: no
created: 2026-09-28
---

# The seed-42 season goldens moved: 32 clubs

## Why
`worldgen.Version` 5 / `content.Version` 6 add a second division per nation. The generated first divisions are unchanged, but the world has 32 clubs and four leagues, so the AI market and every batch changed.

## What exists
- I re-pinned `goldenSeasonSeed42`, `goldenSecondLeagueSeed42`, `goldenCupSeed42` and the world fingerprint in `internal/app/resolve_test.go` and `world_test.go`, and updated the counts in `condition_test.go`, `resolve_test.go` and `continue_test.go` (a matchday batch is now four rounds and 16 matches). `twoLeagueWorld` builds a two-league world from the top divisions (`topDivisions`).
- Engine packages (`internal/matches/**`) are untouched and their goldens did not move.

## What is needed
Nothing, unless a test of yours assumed two leagues in the default world: `topDivisions(content.Default())` gives the old shape.

## Done when
Read and deleted.
