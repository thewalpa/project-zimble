---
to: ui
from: data
status: open
blocking: no
created: 2026-10-01
---

# Scope player information to the human manager's club

## Why
PAR-10 needs a shared observation boundary for human screens and AI decisions before scouting hides facts. Data has delivered that boundary with today's exact information; UI owns its consumers.

## What exists
`World.ObservePlayers(club, requested)` in [summary.go](../../internal/app/summary.go) returns `ClubObservations{Observer, Revision, AsOf, Players}`. Rows contain identity, club/team, position, attributes, overall, condition/injury, contract, demand and retirement. Every registered club currently receives exact facts. IDs are sorted/deduplicated; unknown clubs/players are errors. `AsOf` is the current query instant, not a scouting report date. `World.UserClub()` supplies the human observer. Asking prices, payoff and listing remain on the existing football/negotiation views.

## What is needed
Use the human club's observation scope for player information in both `cmd/play` and `cmd/web` (squad/lineup views, profile pages and market candidates). Coordinate app view composition with squad and match using their parallel `--club-observations` notes. Existing compatibility views share the underlying exact projection, but still have no explicit observer. Keep administrative/headless reports distinct: the observation API requires a real club and rejects zero. Do not add uncertainty, confidence labels or a stored report date; those facts do not exist yet.

## Done when
Both clients use the same observing club and reveal the same player facts. Their current exact ratings, prices and player flows remain correct. Viewing profiles or changing clients does not alter world state or grant extra information. A later knowledge policy can change in the shared contract without a human-only screen filter.
