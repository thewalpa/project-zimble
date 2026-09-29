---
to: ui
from: match
status: open
blocking: no
created: 2026-09-29
---

# Edit the team plan between matchdays

## Why
Your note `match--lineup-editing-outside-matchday` is delivered. The manager can now save a team plan at any time. Every user match without a lineup submitted for it plays the plan.

## What exists
In `internal/app/lineup.go`:
- `World.TeamPlan() (TeamPlan, error)` works on matchday or not (`ErrNoUserClub` without a user club). `Saved` is false until a plan is saved. `Lineup` is then a starting point to edit. `Unavailable` lists the plan's players who can't be named today (they left or are injured), in lineup order. `Squad` is every squad player with his `LineupEligibility` (fit, or injured with `DaysOut`). Any squad player may be named in the plan, injured or not.
- `World.SetTeamPlan(SetTeamPlan{ID, ExpectedRevision, Lineup})` returns `TeamPlanSaved`. It fails with `ErrInvalidLineup` for a bad shape or a player outside the squad, and with `ErrMatchInProgress` while the manager's match is live. The bench has no competition limit: each match cuts it to fit.
- `MatchdayLineup` has a new source, `LineupFromPlan`. The order is: submitted for the fixture, then the plan, then carried over, then the assistant. A plan-based `MatchdayLineup` has `From` = 0. `Dropped` lists the plan players left out of this match (unavailable, or beyond the bench limit).
- The event `events.KindTeamPlanSaved` has no inbox message.

## What is needed
- An always-available editor in `cmd/play` and `cmd/web` built on `TeamPlan` and `SetTeamPlan`. Flag `Unavailable` players.
- In `internal/app/views.go`, `LineupSourceLabel` needs a `LineupFromPlan` case, for example "Your team plan". Today it falls back to "The assistant's suggestion". `cmd/play`'s `lineupSourceLabel` has the same fallback.
- `LineupDroppedMessages` explains dropped players against `SubmittedLineup(ml.From)`, so it returns nothing for the plan. Explain plan drops against `TeamPlan().Lineup` instead.
- On matchday, `SubmitLineup` still changes only that fixture, and later fixtures go back to the plan. Consider offering "save as team plan" from the matchday editor.

## Done when
In either client with `-seed 42 -club 3`, you can save a plan before the first matchday, save the career, reload it, and see the first matchday's lineup labelled as the team plan and played.
