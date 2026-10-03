---
to: competitions
from: squad
status: open
blocking: no
created: 2026-10-03
---

# Which league season a club was in at a given instant

## Why
The agreed division economy ([design](../squad-economy-design.md#agreed-contract-2026-10-03),
`squad--division-content-agreement.md`) prices gates, weekly operating costs
and youth intake by a club's current domestic division. Squad needs that at
the posting instant, and restore validation needs it for every historical
entry, across promotion, relegation and play-offs.

## What exists
`competitions.Store` keeps past seasons, entrants and rounds, but records
no instant at which a league season became current. `endSeasons` in
`internal/app/season.go` creates the next league seasons at the league's
last kickoff, or after the play-off edition is decided, in
`sim.PhaseConsequences`. Wages run in `PhasePreparation` and the player year
in `PhaseExpiries` (1 Jul epoch, so 30 June).

## What is needed
1. An app query, for example `w.leagueSeasonAt(comp, at) (competitions.SeasonRef, error)`
   (or one returning the club's tier directly): the season of league `comp`
   whose entrants were current at `at`, as seen by Preparation and Expiries
   tasks at that instant. Derive it from kept seasons if you can prove it
   unambiguously; otherwise save the instant each season became current
   (a snapshot field: bump the schema with squad's joint commit, not alone).
2. Confirm the ordering squad will rely on: a payday or player year at the
   same instant as a transition sees the pre-transition membership, because
   Preparation and Expiries run before Consequences. Squad documents it in
   tests; flag it if you plan to move season creation to an earlier phase.
3. Confirm play-offs always complete before the 30 June player year on the
   default calendar. If not, intake simply uses the old membership; say so
   and squad will test that case.

## Done when
The query exists with tests covering a promotion via play-off, a straight
swap and the first season, and points 2 and 3 are answered here. It lands
with or before squad's step 2 on `joint/division-economy`.
