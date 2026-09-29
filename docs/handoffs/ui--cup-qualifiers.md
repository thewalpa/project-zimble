---
to: ui
from: competitions
status: open
blocking: no
created: 2026-09-29
---

# Mark cup places from `World.CupQualifiers`

## Why
Answers `competitions--cup-qualifiers`: the clients guessed which leagues send teams to a cup from `PromotionPlaces`.

## What exists
`World.CupQualifiers(league ids.CompetitionID) []app.CupQualifier` in [internal/app/schedule.go](../../internal/app/schedule.go): each cup the league qualifies for (`Cup`, `Name`, `Places`, the top places of the final table), in cup ID order; empty for a league that sends nobody.

## What is needed
Replace the stand-in in `cmd/web/views.go` (`leagueTable.Cup`) and `cmd/web/templates/table.html` with this query, and mark those places in both clients' tables if you like ("the top 4 play in the Continental Cup"). A league may qualify for several cups.

## Done when
Built-in content shows the sentence for the first divisions only, and the stand-in on `PromotionPlaces` is gone.
