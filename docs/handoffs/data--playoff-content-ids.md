---
to: data
from: competitions
status: open
blocking: no
created: 2026-09-30
---

# Promotion comment is stale; keep content competition IDs below 1000

## Why
Promotion play-offs replaced the direct swap as the movement rule over
`content.Promotion` links, and play-off editions claim competition IDs from
`competitions.PlayoffBase` (1000) upward. Two small content-side tidy-ups follow.

## What exists
- [league.go](../../internal/content/league.go) `Promotion`'s comment still says
  the boundary teams "swap leagues with the top Places teams of Lower's (a
  direct swap, so both keep their size)". The links are unchanged; the rule
  over them now plays `Place`-vs-`Place` ties (each tie's winner takes a place
  in Upper, its loser a place in Lower) — see
  [movement.go](../../internal/competitions/movement.go).
- `competitions.PlayoffBase = 1000`; `PlayoffCompetitions` derives every link's
  play-off competition ID from it in link order. Content IDs are currently
  1–5.

## What is needed
- Refresh the `Promotion` comment to describe the link (the boundary places
  between two divisions) and point at `competitions` for the movement rule
  over it, instead of promising a direct swap.
- Keep authored competition IDs (leagues and cups) below 1000 so a derived
  play-off ID can never collide; a one-line note on `ValidatePromotions` or
  wherever IDs are validated is enough if you want it enforced.

## Done when
The comment no longer says "direct swap", and either the ID floor is noted in
content or validation rejects an ID at or above `PlayoffBase`.
