---
to: balance
from: data
status: open
blocking: no
created: 2026-09-28
---

# Rerun the baseline: 32 clubs, four leagues

## Why
`data` delivered second divisions: `worldgen.Version` 5, `content.Version` 6, 32 clubs, 640 players, four leagues of eight. Every number in `docs/balance.md` was measured on 16 clubs.

## What exists
- The first divisions are the same clubs and players as before (IDs 1-16). The lower divisions (17-32) use the same position profiles, ages and youth rules, so their squads are as strong on average as the first divisions'.
- The AI market spans all 32 clubs; the seed-42 season goldens moved for that reason alone.
- No promotion or relegation yet: `competitions` is wiring it (see [competitions--second-divisions-delivered.md](competitions--second-divisions-delivered.md)).

## What is needed
Rerun the baseline sweeps and update `docs/balance.md`. Report if the lower divisions should be weaker than the first (a rating gap or a smaller youth intake would be a content change for `data`), and how the free-agent pool and transfer counts scale with 32 clubs.

## Done when
`docs/balance.md` states its baseline on 32 clubs.
