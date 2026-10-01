---
to: ui
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Continue stops only at the manager's fixtures; other batches auto-resolve

## Why
The [competitions backlog item](../lanes/competitions.md) is delivered: `Continue` resolves rounds with no user fixture without stopping, so a career is not paused by other clubs' matchdays (an out-of-cup club's cup week, a play-off it is not in). Only a batch containing the managed club's fixture stops and waits for lineups and `ResolveRounds`.

## What exists
- `internal/app/continue.go`: `Continue` returns `ReachedTarget{Now, Resolved []BatchResolved}` or `FixtureRoundReady{At, Revision, Rounds, UserFixtures, Resolved []BatchResolved}`. `Resolved` carries every batch the call played on the way (before the stop, or all of them when it reached the target). `BatchResolved{At, Rounds []competitions.RoundRef, Matches []MatchReport}`; `MatchReport` is unchanged. The result is a fresh copy — keep or drop it freely.
- **Your `cmd/simulate`:** minimal blocking fixes, ready for you to reshape. `playBatch` now targets one batch's kickoff per call (so `-rounds` counts batches), prints each `Resolved` batch through the same round/match format as command results, and its final call still `Continue`s to the end target so trailing cohorts (the season end) run. `demoContinue` reports `resolved <time>: N rounds, M matches` lines and the calendar line now counts completed rounds too. `main_test.go` pins the new demo and reworks `TestLoadWithoutModePrintsStatusAndChangesNothing` to save with `-club 3` (an unmanaged demo no longer leaves a pending round).
- `cmd/play` and `cmd/web` run unchanged: their `len(ready.UserFixtures) == 0` branches are now unreachable (left standing), and their managed flows stop exactly where they did — only quieter.

## What is needed
Both clients should show auto-resolved results on the way to a stop (at least a summary line per batch in the match log; `playBatch` in `cmd/simulate` shows the full shape), and any copy that promised every batch pauses should change. One known limit to render honestly: full match detail (goals, events, lineups, stats) of an auto-resolved match lives only in that call's `BatchResolved.Matches` — a later `MatchReport()` falls back to score-only, like a restored save. Keeping those details queryable would need the command log to hold them (a `storage` schema change); file a note back if you want it.

## Done when
Both clients list auto-resolved batches when reporting a stop or target, and a managed career whose club is out of the cup runs the cup final week with no phantom stop (`go run ./cmd/web`, manage club 3, season to the end).
