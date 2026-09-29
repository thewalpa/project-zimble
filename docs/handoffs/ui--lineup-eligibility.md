---
to: ui
from: match
status: open
blocking: no
created: 2026-09-29
---

# Filter the lineup editors by the app's eligibility

## Why
Your note `match--lineup-availability.md` asked for the app's selection rule so the lineup editors can offer an "available only" filter without re-deriving it. It is delivered.

## What exists
In [internal/app/lineup.go](../../internal/app/lineup.go):
- `World.SquadEligibility(fixture) ([]LineupEligibility, error)`: every player of the user club's squad for a pending user fixture, in ascending player ID order (the same order as `World.Squad`). Same errors as `SuggestLineup` (`ErrNoUserClub`, `ErrNotUserFixture`, `ErrFixtureNotPending`).
- `LineupEligibility{Player, Eligibility, DaysOut}`, with `Eligibility`:
  - `EligibleFit` (1): fit, may be named;
  - `EligibleInjured` (2): injured, but may be named because the fit players cannot field a legal eleven (a goalkeeper and ten outfield players). When any player is `EligibleInjured`, every injured player is, and the club is fielding its injured;
  - `IneligibleInjured` (3): injured, may not be named.
  - `Eligibility.Selectable()` says whether a player may be named.
- `SubmitLineup` checks the same rule. A player who becomes injured between the preview and the submission is rejected with `ErrInvalidLineup` and a message such as "player 17 is injured for 5 more days".

## What is needed
In both lineup editors (`cmd/play` and `cmd/web`), an "available only" filter that hides players whose `Eligibility.Selectable()` is false. When a player is `EligibleInjured`, show that the club is short and is fielding injured players (for example "injured, playing: not enough fit players"). Take this from the query instead of from `SquadPlayer.DaysOut`.

## Done when
Both editors filter on `SquadEligibility` and label the emergency case; delete this note.
