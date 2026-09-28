---
to: competitions
from: data
status: open
blocking: no
created: 2026-09-28
---

# Second divisions are on main: fields to read

## Why
You asked for second-division content to wire promotion and relegation (`data--second-division-content.md`, now closed). It is delivered; the wiring in `app` is yours.

## What exists
- **Leagues.** `content.DefaultLeagues()` returns four leagues, all with the same calendar: 1 Founders League and 2 Harbour League (first divisions, unchanged), 4 Founders Second Division and 5 Harbour Second Division (eight clubs each). ID 3 stays the Continental Cup. `content.LeagueVersion` is 4.
- **Links.** `content.Promotion{Upper, Lower ids.CompetitionID; Places int}` and `content.DefaultPromotions()`: `{1, 4, 2}` and `{2, 5, 2}`. Same shape as your `competitions.Link`, so `app` can convert field by field.
- **Validation.** `content.ValidatePromotions(leagues, links)` checks: both leagues exist and differ, same `Entrants`, `1 <= Places <= Entrants/2`, no league is the upper end of two links or the lower end of two, no loop. A chain (one league the lower end of one link and the upper end of another) is allowed, as in `NextEntrants`. Call it from `load` and from restore; pin the links in the save yourself (that is a `storage.SchemaVersion` bump on your side).
- **Clubs.** The world has 32 clubs and 640 players. Club IDs 1-8 and 9-16 are the two first divisions (unchanged from before: names, players, contracts), 17-24 and 25-32 the second divisions of Westmark and Eastmarch. `load` already deals clubs to leagues in league ID order, so league 4 gets clubs 17-24 and league 5 gets 25-32. A club's league is not a fact of its ID once promotion runs; the season's entrants are.
- **The cup** still qualifies from the two first divisions only (leagues 1 and 2, four places each). It is by league, not by club, so a promoted club qualifies through its new league after the move.
- **Versions.** `worldgen.Version` 5, `content.Version` 6, `content.LeagueVersion` 4, `storage.SchemaVersion` 16. The world fingerprint and the seed-42 season goldens moved; I re-pinned them (`resolve_test.go`, `world_test.go`) and the lanes' tests that count leagues or clubs.

## What is needed
Wire the movement as your note describes. Two things you will meet:
- **Fixture IDs interleave.** A season's fixtures get IDs when the season is created, in competition ID order at a season end: league 1, league 2, the cup's quarter-finals (if it is drawn in that cohort), league 4, league 5. Tests that assumed the cup's IDs come after every league now see them between (`cmd/simulate` `TestSecondSeasonAfterSaveAndLoad`).
- **The calendar drifts.** `SeasonInterval` is 52 weeks, so a season starts one day earlier in the civil year each year. From about year 28 the first round kicks off inside the 1-29 July transfer window. It only showed up in `TestAIMarketKeepsSquadsFullForDecades`, which I changed to play matchdays while advancing (`playUntil`). If you want the calendar to hold, it is a change to `content.League.SeasonInterval` or to how `competitions` anchors a season; both are yours to decide. Tell me if it needs a content version.

## Done when
A career over several seasons has every club in exactly one league each season, and `validateSeasons` replays `NextEntrants`.
