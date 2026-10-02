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

Accepted on 2026-10-02 into the ui backlog, waiting for an app-owned cue. "Below about 90" is a condition rule, which the ui lane may not compute in `cmd/`. Filed `match--tired-starters` for a per-starter flag on `MatchdayLineup` (or a query beside it). Both lineup screens will mark the flagged starters and offer `SuggestLineup` once it lands.

## Update (balance, 2026-10-02)
On the default football year (`LeagueVersion` 7) this matters less than when it was filed: starters arrive at 99.8 condition on average, and a carried lineup costs 0.2 ± 0.1 points a season in a year with no cup and 0.7 (`simple`) to 3.3 (`tick`) points in a year with a cup ([docs/balance.md, "Rotation policy on the football year"](../balance.md#rotation-policy-on-the-football-year-with-and-without-a-cup)). The warning is needed only before a short-rest match (a cup match four days after a matchday), where 53–61% of carried starters are below 90. Keep the note; show the cue when the rest before the match is under a week, and skip it otherwise.

## Update (match, 2026-10-02): the cue is on main
`app.MatchdayLineup.Tired []ids.PlayerID` lists the starters below `app.TiredCondition` (90) in slot order, set only when `Source` is `LineupCarriedOver` or `LineupFromPlan`; it is empty for a submitted lineup and the AI's suggestion. The player stays in `Lineup.Starters`; `World.SuggestLineup(fixture)` gives the alternative. Take the starter's current condition from `SquadPlayer.Condition` to show the number. For the short-rest refinement above, `app` does not know the rest: compare the fixture's kickoff with the club's previous kickoff in the client, or show the flag whenever `Tired` is not empty, since after a week's rest it is nearly always empty.
