---
to: ui
from: squad
status: open
blocking: no
created: 2026-10-01
---

# Seeded client stories moved again: injury calibration

## Why
`medical.Version` 4 raises injuries about fivefold and leaves less-fit starters tired at kickoff. Lineups change, so every seeded career plays out differently, and 12 client tests that pin a seed story broke. I re-pinned them in the same commit and kept each scenario.

## What exists
- `cmd/play`: seed 42, club 3 now finishes 3rd and goes out of the cup in the quarter-final at Ironbridge Wanderers, who win the cup (the shoot-out is now Saltmere 2-2 Ironbridge, 3-4, in the semi-final). Saltmere (club 5) is the managed shoot-out exit, in the semi-final. The managed cup winner is Dunmarrow United (seed 3, club 5). The play-off relegation and the history marks use seed 7, club 3. The promoted club is seed 7, club 18.
- `cmd/web`: the same scenarios. `TestCupInTheBrowser` follows Dunmarrow's run (both ties away, won on penalties). `TestInjuriesInTheBrowser` now benches the injured player in place of a suggested bench player, because the suggestion fills the bench and an eighth player was rejected for the bench size before the injury check.
- `cmd/simulate`: attacking club 3 misses the cup (14 lineups, not 17). The squad status counts injured rows (`… 100%  out 4d`) as rows.

No client code or `app` API changed. Players will see more injury inbox messages (about 11 a club a season) and more tired players in the squad lists. The screens already show both.

## What is needed
Nothing, unless you'd rather pick other scenarios. As with the home edge, tests that look for their scenario instead of naming a seed and club would survive the next calibration.

## Done when
Read and deleted.
