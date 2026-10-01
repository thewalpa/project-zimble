---
to: squad
from: data
status: open
blocking: no
created: 2026-10-01
---

# Use club observations for recruitment and renewal decisions

## Why
PAR-10 calls for human and AI managers to use the same club knowledge before scouting introduces uncertainty. Data has delivered the observation contract; squad owns its recruitment/renewal callers and their staged market state.

## What exists
`World.ObservePlayers(club, requested)` in [summary.go](../../internal/app/summary.go) returns detached `ClubObservations{Observer, Revision, AsOf, Players}` in player ID order, with exact identity, employment, ratings, condition/injury, contracts and wage demands for every registered club. Unknown clubs/players are errors, repeated IDs collapse to one row. Existing player rows share its underlying projection. Asking prices and payoffs are not knowledge fields: negotiation policy and common football rules still own them. No saved fields, schema bump or seeded output change.

## What is needed
Use the deciding club's observations for recruitment comparisons and renewal policy inputs (`freeAgentPool`, annual planning and `market.candidates`/members). Preserve authoritative capacity, retirement, employment, financial and player-consent validation. A market stages transfers/signings within a cohort: preserve those projected assignments and squad averages rather than accidentally rereading pre-cohort employment; describe the overlay or ask data for the needed observation API extension before introducing uncertainty. Coordinate club scope for `FreeAgents`/`PlayerProfile` with UI; compatibility rows currently carry no explicit observer.

## Done when
Human and AI policy inputs for equivalent club state see identical player information. Staged same-cohort changes remain deterministic; current exact-information adoption preserves existing outcomes and saves. No hidden authoritative ratings bypass the observation projection for manager policy. There is no request to change football rules or add scouting now.
