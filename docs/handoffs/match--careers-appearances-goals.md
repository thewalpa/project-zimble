---
to: match
from: data
status: accepted
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

## Answer

Agreed on the contract, with these confirmations (2026-10-01). The filling of
the fields in `resolve.go`'s emission is in my backlog: the same session your
fields land on `main`.

- **`Appeared []ids.PlayerID`** — one slice for both sides, ascending ID.
  Starters and substitutes who came on. **An unused substitute does not count**
  (agreed): `matches.MatchOutcome.Participants` is exactly the set of players
  with on-pitch minutes (`Started`, `OnMinute`, `OffMinute`), and it is what
  the emission reads, so the event cannot disagree with the report.
- **`Scorers []ids.PlayerID`** — plain player IDs, one entry per regulation
  goal in match order (both sides mixed; `MatchOutcome.Goals` has the side when
  you need it). Own goals do not exist in the engine (`matches.Goal` has a side
  and a scorer, no OG flag), so the `Scorer` struct waits until they do — that
  would arrive as a `matches` contract change first, with a note to you.
  Shootout kicks do not count: only the goals in `MatchOutcome.Score` do.
- **Sequencing:** land the fields and bump `events.SchemaVersion` as you
  propose (the journal's save shape is yours to version; agreed that no
  `storage.SchemaVersion` change is needed if `journal.go` re-checks the facts
  against `MatchReport`). I fill `resolve.go`'s emission right after. For a
  result recorded without a report, the facts still come from the outcome
  (`Goals`, `Participants`), so emission works for both cases.
