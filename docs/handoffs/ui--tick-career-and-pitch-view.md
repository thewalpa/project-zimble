---
to: ui
from: match
status: open
blocking: no
created: 2026-09-30
---

# Start a career on the tick engine and watch it on a pitch

## Why
A career can now be played on the `tick` engine, which moves the ball and all 22 players and produces positional frames. The player needs a way to choose it and to see the frames.

## What exists
On main, in [internal/app/resolve.go](../../internal/app/resolve.go) and [internal/app/live.go](../../internal/app/live.go):
- `app.Config.Engine` (string) picks the engine for a new career; `app.Engines()` lists the choices, the default (`simple`) first. An unknown name returns `app.ErrUnknownEngine`. The choice is fixed for the career and a save keeps it: a loaded career needs no flag.
- `World.MatchEngine()` returns `ID`, `Version` and `Capabilities`. Offer the pitch view only when `Capabilities.PositionalFrames` is set.
- `World.LiveFrames(everyMillis uint32) ([]matches.Frame, error)`: the frames of the live match from the previous stop to the current one. `everyMillis` thins them (1000 gives one a second; 0 keeps all five a second, 13,500 for a half). `ErrNoLiveMatch` without a live match; an error wrapping `matches.ErrUnsupported` on `simple`. It is a read: call it after each `PlayMatch`.
- `matches.Frame`: `Millis` (regulation clock), `Ball`, `Carrier` (zero when loose), `Players[side][slot]` in centimetres, indexed like `LiveMatch.View.OnPitch` of the same step. The pitch geometry and the flank convention are documented beside `matches.PitchLength` in [internal/matches/matches.go](../../internal/matches/matches.go).

## What is needed
- An `-engine` flag (values from `app.Engines()`) for new careers in `cmd/simulate`, `cmd/play` and `cmd/web`, ignored or rejected with `-load`. Show the career's engine where the career's versions or summary are shown.
- `cmd/web`: a 2D pitch view of the live match (a canvas replaying the segment's frames, a few per second), with players labelled from `View.OnPitch` and the carrier marked.
- `cmd/play`: nothing required; text commentary from frames can wait for match statistics (roadmap phase 3).

## Done when
`go run ./cmd/web -engine tick -seed 42 -club 3` shows the manager's match moving on a pitch between stops, and `go run ./cmd/simulate -engine tick -seed 42 -season` plays a season on `tick`.
