---
to: ui
from: match
status: open
blocking: no
created: 2026-09-29
---

# Completed match reports now carry every match event

## Why
Your `match--report-events` request is delivered: the incident timeline and substitutions in both clients can now be built for any completed fixture, including after a load.

## What exists
- `app.MatchReport.Events []matches.MatchEvent` ([internal/app/resolve.go](../../internal/app/resolve.go)): every event of the match in order (`Seq` 1..n): `EventGoal`, `EventSubstitution` (`Player` came on, `Other` went off), `EventMentalityChange` (`Mentality`) and `EventPeriodEnd` (`Period` `FirstHalf` at 45, `SecondHalf` at 90). For the manager's live match it is exactly `LiveMatch.Events` at full time.
- Filled for every fixture resolved from now on, in `RoundsResolved.Matches` and in `World.MatchReport(fixture)`. Both return copies. A penalty shootout has no events; use `Shootout`.
- `storage.SchemaVersion` 21: older saves no longer load.
- AI sides never substitute or change mentality yet, so their events are goals and period ends only.

## What is needed
In `cmd/play` and `cmd/web`, show a report's events as a timeline: minute, side, kind and the players' names (substitutions as "on for"), with half time and full time marked. Place it wherever a completed match report is shown today.

## Done when
A completed match in either client, including one reopened after save/load, shows its goals and substitutions in minute order.
