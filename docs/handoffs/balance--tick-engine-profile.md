---
to: balance
from: match
status: open
blocking: no
created: 2026-09-28
---

# Measure the tick engine against the simple engine

## Why
`internal/matches/tick` is a second match engine behind the same contract. It simulates the ball and all 22 players five times a second, and goals come out of passes, dribbles, tackles and shots. Before any career match uses it (match-lane roadmap phase 2), we want an independent statistical profile. Its trend tests only guard signs and gross calibration.

## What exists
- `tick.New(tick.DefaultParams())`, driven like `simple` through `matches.Engine`. `enginetest.Input(fixture, homeStrength, awayStrength)` builds test matches, and `enginetest.Outcome(t, engine, input)` plays one to full time.
- Our own numbers (seed 42, 200 matches each, `go test ./internal/matches/tick -run TestModelTrends -v`): equal teams 60 v 60 score 2.94 goals a match, with home 44%, away 35% and draws 21%. At 55 v 65 the stronger (away) side wins 56%. With both sides attacking, 3.4 goals; with both defensive, 1.45. A home side at condition 30 wins 15%. Draws look low to us.
- About 20 ms per match, so a full league season takes several seconds.

## What is needed
A `balance_test.go` in `internal/matches/tick` (gated sweeps), and a section in `docs/balance.md` comparing the two engines on the same inputs: goals per match and their distribution (0–0s, 4+ goals), home/draw/away rates, win rate by rating gap, the mentality effect, and shootout frequency in knockouts. Please file notes for anything implausible. `tick.ModelVersion` is ours to bump.

## Done when
`docs/balance.md` has the comparison with the command that reproduces it, and any concerns are filed as notes to `match`.
