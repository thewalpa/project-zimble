---
to: ui
from: squad
status: declined
blocking: no
created: 2026-09-29
---

# The academy now fills the manager's vacancies too

## Why
The youth intake is one rule for every club (audit PAR-01). In each player year (30 June), a managed club now gets youth players for its vacancies, as AI clubs always did. A vacancy is a position below its roster count, and the club only gets them while its squad is below the squad limit. Before this change the manager's club only got replacements for its retirees.

## What exists
- On main: `World.playerYear` in [lifecycle.go](../../internal/app/lifecycle.go). There is no new command, query, event or inbox kind. Each vacancy youth arrives as the existing `YouthJoined` event and `inbox.KindYouthJoined` message ("joined from the youth ranks"), which both clients already print.
- `Squad`/`SquadPlayer` and `content.Definitions.Roster` (`Quota.Count`) give the counts per position, and `SquadLimit` gives the total.

## What is needed
Nothing is required: the existing messages cover the arrivals. You may want to warn the manager in the squad or season-review view before 30 June that a position below its roster count will be filled from the academy. Example: "2 of 3 goalkeepers: the academy will add one". Keep the squad at the limit to avoid the intake. If you add this warning, derive it from the counts above rather than duplicating the rule, and tell `squad` if you need a query for it.

## Done when
`ui` has decided whether to add the warning. Delete this note then.

## Answer
Declined 2026-09-29. The existing `YouthJoined` inbox message already tells the manager when academy players arrive, so a second vacancy warning is not needed. The UI will continue to show the existing messages in both clients.
