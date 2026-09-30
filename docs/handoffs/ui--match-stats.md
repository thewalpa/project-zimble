---
to: ui
from: match
status: open
blocking: no
created: 2026-09-30
---

# Show match statistics in reports and the live match

## Why
A career on the `tick` engine now has statistics for every match: shots, shots on target, passes and completion, tackles, saves, offsides and possession. The player should see them after a match and while watching one.

## What exists
On main, in [internal/matches/matches.go](../../internal/matches/matches.go) and [internal/app/resolve.go](../../internal/app/resolve.go):
- `matches.MatchStats{Available bool; Teams [2]TeamStats}`, indexed home then away. `TeamStats` has `Shots`, `ShotsOnTarget`, `Passes`, `PassesCompleted`, `Tackles`, `Saves`, `Offsides` (times the side was caught offside, since `tick` v6) and `PossessionPermille` (the two sides add up to 1000).
- `MatchReport.Stats` (from `ResolveRounds` results and `World.MatchReport`) holds the final statistics.
- `LiveMatch.View.Stats` holds the statistics so far after each `PlayMatch` or `MatchDecision`.
- `Available` is false on a `simple` career (and on a result recorded without a report): show nothing, never zeros. `World.MatchEngine().Capabilities.DetailedStats` says the same up front.

## What is needed
- `cmd/web`: a statistics table on `/report` (home and away columns: possession %, shots, on target, passes with completion %, tackles, saves, offsides), and the same table beside the live match, refreshed at each stop.
- `cmd/play`: the same rows after `FULL TIME`, and a `stats` command (or a line at each stop) during the live match.
- `cmd/simulate`: optional; a line per match when `-engine tick` is used, if it fits the output.

## Done when
`go run ./cmd/web -engine tick -seed 42 -club 3` shows statistics on the live match and its report, and a `simple` career shows no statistics section.
