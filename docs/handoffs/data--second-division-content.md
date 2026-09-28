---
to: data
from: competitions
status: open
blocking: no
created: 2026-09-28
---

# Second divisions: content, clubs and generation

## Why
Promotion and relegation needs a lower division per nation. The rule is decided (see `docs/progress.md`, "Promotion and relegation"): a direct swap of the bottom N of the upper division with the top N of the lower, on final rankings. Competitions has the movement function; it needs content to feed it.

## What exists
- `competitions.NextEntrants(rankings, links)` and `competitions.Link{Upper, Lower, Places}` in [movement.go](../../internal/competitions/movement.go): a pure function from every league's final ranking to next season's entrants.
- `content.League` has no notion of divisions; `load` in `internal/app/world.go` deals clubs to leagues in ID order.

## What is needed
1. New league definitions: a second division per nation (eight clubs each, same calendar as the first), so `DefaultLeagues` has four leagues. Suggested IDs keep the existing ones (1 Founders, 2 Harbour) and the cup at 3; pick free IDs for the new leagues.
2. A content type for the links, pinned in the save like the cups (`content.Promotion{Upper, Lower ids.CompetitionID; Places int}`, two places per link is my suggestion; it must not exceed half of either league). Validate it in content: leagues distinct, each league the upper end of at most one link and the lower end of at most one, both leagues the same size. Tell me the field names and I will wire the pinned rule into `app` and `validateSeasons`.
3. Clubs and generation for the new divisions. `worldgen` changes the world fingerprint, so bump `worldgen.Version`, `content.Version` and `content.LeagueVersion` and update the goldens. Keep the first nation's first-division clubs unchanged if you can (the earlier milestones did).
4. Decide the cup: the Continental Cup qualifies from the first divisions only; say so if that changes.

## Done when
`DefaultLeagues` returns the new divisions and links, `go test ./...` passes with the new goldens, and a note comes back to `competitions` saying which fields to read.
