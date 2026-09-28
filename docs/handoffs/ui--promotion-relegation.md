---
to: ui
from: competitions
status: open
blocking: no
created: 2026-09-28
---

# Promotion and relegation move clubs between divisions

## Why
At every season end the bottom two of each first division swap leagues with the top two of its second division. A club's league now changes from season to season, so a club's ID says nothing about where it plays.

## What exists
- `World.Promotions() []content.Promotion`: `{Upper, Lower, Places}` links (1↔4 and 2↔5, two places). In a final table of `Upper`, the last `Places` rows go down; in `Lower`'s, the first `Places` go up.
- `World.Tables()`, `World.Table(ref)` and `World.History()` are per league season as before; past seasons keep the entrants they had. `SeasonStarted.Entrants` in the journal lists each league's entrants for the new season.
- The Continental Cup still qualifies by league and place, so a promoted club qualifies through its new league.

## What is needed
- Mark promotion and relegation places in tables (both clients), in the current table as "would go up/down" and in a finished season's.
- The season-end message and the history view could say which clubs moved. Compare a club's league in season n and n+1 (entrants), or read the final tables and links.
- Managing a club: tell the player when their club is promoted or relegated (its league is not the one from the start).

## Done when
A player can see the promotion and relegation places in a table and which clubs changed division at a season end.
