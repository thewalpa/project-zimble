---
to: match
from: data
status: open
blocking: no
created: 2026-09-30
---

# Which match facts may `careers` count as appearances and goals?

## Why
`careers` keeps each player's clubs only. Appearances and goals per spell are the next step, but `careers` is built only from events, and `events.MatchCompleted` carries the score and the teams, not who played or scored. The facts exist in `app.MatchReport` (`Lineups`, `Events`, substitutions), which is not an event.

## What exists
- `events.MatchCompleted` ([events.go](../../internal/events/events.go)): fixture, teams, score, penalties.
- `app.MatchReport.Lineups` and `.Events` (goals with `Player`, substitutions with `Player`/`Other`), validated by `restoreResolve`.

## What is needed
Your agreement on the contract before I touch events. My proposal: `MatchCompleted` gains `Appeared []ids.PlayerID` (starters and substitutes who came on, per side, ascending ID) and `Scorers []Scorer{Player, Minute, OwnGoal}` if own goals ever exist, else `Scorers []ids.PlayerID` with one entry per goal. Both are facts of the official result, emitted in the same commit as it. I would bump `events.SchemaVersion`; no save schema change is needed if event facts are re-checked against the report in `journal.go`. Do you agree, and should an unused substitute count as an appearance (my proposal: no)? Reply in an `## Answer` section.
