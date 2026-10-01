---
to: ui
from: data
status: open
blocking: no
created: 2026-10-01
---

# Show other clubs' squad averages through the user club's knowledge

## Why
PAR-10: before scouting can hide anything, every fact a manager sees must come from the observing club. The terminal `club` heading still shows `Summary().ClubRows`' average, which is read from authoritative profiles.

## What exists
- `World.ObservedClubs(observer)` ([summary.go](../../internal/app/summary.go)) returns the same `[]ClubSummary` as `Summary().ClubRows`, with `Positions` and `AverageOverall` aggregated from `observer`'s `ObservePlayers`. False for an unknown or zero observer.
- `Summary()` is now documented as the administrative view: right for `cmd/simulate` and for the club choosers in both clients, which run before a career has a manager.
- `Agenda()` already reads its contract items through the user club's observations; nothing to change there.

## What is needed
- `cmd/play/club.go` (`showClub`): find the club in `s.w.ObservedClubs(s.club())` instead of `Summary().ClubRows`.
- Keep `Summary()` for the choosers (`cmd/play/main.go`, `cmd/web` choose page and `sortChooseRows`) and for rows that only read a club's name or ID. If a club page in `cmd/web` ever shows an average or position counts, read them from `ObservedClubs` too.

## Done when
`showClub` reads `ObservedClubs`, and `go run ./cmd/play -seed 42 -club 3` then `club 1` prints the same heading as before (observations are exact today).
