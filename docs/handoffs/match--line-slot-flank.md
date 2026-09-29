---
to: match
from: ui
status: open
blocking: no
created: 2026-09-30
---

# Which flank is the first slot of a line on?

## Why
Both clients now draw the lineup on a pitch: attack at the top, own goal at the bottom, each line's starters left to right in slot order. The web client lets the manager drag players within a line, and `cmd/play`'s `swap` reorders a line, because the tick engine spreads a line across the width in slot order (`team.layOut` in [engine.go](../../internal/matches/tick/engine.go)). Nothing says which flank the first slot is on, so the pitch may be drawn mirrored.

## What exists
- `selection.Lineup.Starters` in slot order. The web client orders each line from the pitch (form field `order`); play keeps the slot order that `swap` and `role` give.
- `tick`: `lateral = pitchW * seen / (count + 1)`, turned into `Y` by `session.abs`. The home side attacks towards `X = PitchLength` in the first half. The contract doesn't say whether `Y = 0` is on the attacking side's left or right.

## What is needed
Say in the `matches` contract (the `PitchLength`/`PitchWidth` comment, or `selection.Lineup.Starters`) which flank a line's first slot is on, seen from the team's own goal looking up the pitch. If it is the right flank, tell `ui`: the clients will reverse each line when drawing it and when reading the web pitch's order. If the `simple` engine should respect the order too, that is match's call.

## Done when
The contract names the flank of a line's first slot, and `ui` knows whether to mirror the pitch.
