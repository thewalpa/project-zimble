---
to: ui
from: match
status: open
blocking: no
created: 2026-10-01
---

# Seeded career stories in the client tests moved: simple's home edge

## Why
`simple.ModelVersion` 5 gives the home side a real edge: the home side's chance rate is multiplied by `HomeAdvantagePermille` (1150) and the away side's by its reciprocal, where before the away side played level. The engine version seeds every match's random stream, so every seeded career plays out differently, and your tests that pin a seed-42 story broke.

## What exists
I re-pinned them in the same commit, keeping each test's scenario:
- `cmd/play` and `cmd/web`: Quillford FC (seed 42, club 3) now finishes 4th and goes out in the cup quarter-final at Drakeford City (3-2); Saltmere Athletic wins the league and the Continental Cup (2-0 in the final v Ironbridge Wanderers); the managed cup winner is Saltmere Athletic (club 5) instead of Hollowick Athletic (seed 1, club 6 — that career's cup now goes to Marrowdale City); Brackenmoor Town (club 4) still draws Ironbridge Wanderers in the quarter-final but goes out 2-2 (3-4 on penalties) instead of winning a shoot-out in the semi-final, so `TestCupInTheBrowser` covers the managed winner first and a shoot-out tie second; the play-off and promotion story moved from Lowenby City (club 20, now 4th) to Ivybridge Athletic (club 24, 2nd, wins its tie at Hollowick Town and is promoted); the AI bid for Callum Doyle is offer 84 from Eldhaven United (was offer 86 from Glenrock Town); club 3's season-2 opener is away to Greyfen United (was Brackenmoor Town at home).
- `cmd/simulate`: an attacking season for club 3 now reaches the cup final, so it submits 17 lineups, not 15.

No client code or `app` API changed.

## What is needed
Nothing, unless you'd rather pick other scenarios. Tests that find their scenario (a club that is relegated, a club that wins the cup) instead of naming a club would survive the next engine change.

## Done when
Read and deleted.
