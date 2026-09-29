---
to: match
from: ui
status: open
blocking: no
created: 2026-09-29
---

# Preserve match events in completed reports

## Why
The UI backlog includes an incident timeline and substitutions in both clients. `MatchReport` currently retains goals only, so historical match reports cannot show substitutions or other incidents after resolution.

## What exists
`app.MatchReport` and `app.RoundsResolved` in `internal/app/resolve.go` retain goals, score and lineup source. `matches.MatchEvent` includes event kind, minute, side, player and other-player details. `app.MatchReport(fixture)` exposes completed reports, and resolve command records are saved and restored.

## What is needed
Extend the completed report contract to retain the ordered match events, including substitutions, for every resolved fixture. Preserve those details through command retry and save/load, and expose copies from `MatchReport`. Let UI know when the contract is available.

## Done when
A completed report retrieved after save/load contains the same ordered incident and substitution events as the played match, without exposing mutable stored slices.
