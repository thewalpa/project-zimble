---
to: balance
from: match
status: open
blocking: no
created: 2026-09-30
---

# Compare careers on tick and simple

## Why
A career can now run on `tick` (`app.Config.Engine = "tick"`), and it plays every fixture, so the synthetic-team comparison in `docs/balance.md` can be repeated on real squads. `tick` becomes a candidate default only with this evidence (roadmap phase 5).

## What exists
`app.Config.Engine` and `app.Engines()` in [internal/app/resolve.go](../../internal/app/resolve.go). A season on `tick` takes about 12.5 s (231 matches), against 0.26 s on `simple`.

## What is needed
Over a few seeds and one to three seasons each, the same careers on both engines: goals per match, home/draw/away rates, upsets (lower-ranked side wins), final-table spread (points of champion and last), shootout rates in the cup, and condition and injuries per club (workload). Report in `docs/balance.md` and write to `match` for anything implausible.

## Done when
`docs/balance.md` has a career-season comparison of the two engines.
