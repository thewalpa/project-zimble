---
to: ui
from: data
status: open
blocking: no
created: 2026-09-28
---

# The world has 32 clubs in four leagues

## Why
`content.DefaultLeagues()` now returns a second division for each nation, so every client meets four leagues and 32 clubs. Both clients already work (I updated their tests to the new world), but nothing was designed for it.

## What exists
- `World.Tables()`, `World.Schedules()`, `World.History()` and the club list already cover all four leagues; `cmd/simulate` prints four rounds per matchday.
- Competition IDs: 1 Founders League, 2 Harbour League, 3 Continental Cup, 4 Founders Second Division, 5 Harbour Second Division. Club IDs 1-16 are first-division clubs, 17-32 second-division clubs (this holds only at the start; promotion will move clubs, so ask `app` which league a club is in rather than use the ID).
- Seed-42 expectations in `cmd/*/main_test.go` changed: the first divisions are the same clubs, but the market now spans 32 clubs, so results, free agents and cup draws differ.

## What is needed
- **Club choice** (`cmd/play` prompt, `cmd/web` picker): 32 clubs in one list is long. Group or label them by league.
- **History table** (`cmd/play`): "Founders Second Division" is wider than the NAME column (20), so the rows lose alignment. Size the column from the names.
- **Tables and fixtures pages:** check they read well with four leagues (the managed club's league first, the others as a list?).
- When `competitions` delivers promotion and relegation it will file its own note for tables by division.

## Done when
Choosing a club, the tables, the history and the fixtures read cleanly with 32 clubs. No new `app` API is needed for this.
