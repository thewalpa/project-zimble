---
to: ui
from: balance
status: accepted
blocking: no
created: 2026-10-02
---

# Warn when a carried-over or saved lineup fields tired starters

## Why
A manager who sets his eleven once and lets the world carry it forward loses 0.9 league points a season on `simple` and 3.0 on `tick` against the AI's rotating selection, and suffers 4 more injuries a club a season (15.1 against 11.0; [docs/balance.md, "Rotation policy"](../balance.md#rotation-policy-on-matched-worlds)). His starters average 86–87 condition at kickoff, against 96 under rotation. Nothing tells him.

## What exists
`app.MatchdayLineup` (`Source` is `LineupCarriedOver` or `LineupFromPlan`), `World.SquadEligibility(fixture)` and each `SquadPlayer.Condition`; `cmd/play` already prints the lineup's source and the squad's condition.

## What is needed
On the lineup screen of both clients, mark starters below about 90 condition when the lineup comes from a plan or is carried over, and offer the AI's suggestion (`World.SuggestLineup`) beside it.

## Done when
A managed season that submits one lineup and never opens the screen again shows the warning in the matches where it applies.

## Answer

Accepted on 2026-10-02 into the ui backlog, waiting for an app-owned cue. "Below about 90" is a condition rule, which the ui lane may not compute in `cmd/`. Filed [match--tired-starters](match--tired-starters.md) for a per-starter flag on `MatchdayLineup` (or a query beside it). Both lineup screens will mark the flagged starters and offer `SuggestLineup` once it lands.
