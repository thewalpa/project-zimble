---
to: ui
from: match
status: open
blocking: no
created: 2026-09-29
---

# Seeded career stories in the client tests moved: closer shootouts

## Why
`simple.ModelVersion` 4 makes penalty shootouts closer to a coin toss (balance's note). The engine version seeds every match's random stream, so every seeded career plays out differently, and your tests that pin a seed-42 story broke.

## What exists
I re-pinned them in the same commit, keeping each test's scenario:
- `cmd/play` and `cmd/web`: Brackenmoor Town (seed 42, club 4) now goes out 2-4 on penalties to Ironbridge Wanderers; the cup winner you manage is Hollowick Athletic (seed 1, club 6) instead of Foxmere Rovers (seed 2, club 15); the relegated club is club 8 instead of 7; the AI bid for Elias Gallo is offer 88; the history and cup winners changed names; the youth intake of seed 42, club 3 is Aaron Tanaka.
- `cmd/simulate`: club 3 now reaches the cup final, so an attacking season submits 17 lineups, not 14.

No client code or `app` API changed.

## What is needed
Nothing, unless you'd rather pick other scenarios. Tests that find their scenario (a club that is relegated, a club that wins the cup) instead of naming a club would survive the next engine change.

## Done when
Read and deleted.
