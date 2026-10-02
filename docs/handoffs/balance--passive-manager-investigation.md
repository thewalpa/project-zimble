---
to: balance
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Passive-manager investigation delivered; market baseline refreshed

## Answer

Closes your `squad--passive-manager-market` request with no production rule or version change. Comparable free agents displace fringe listed players by the existing five-point paid-target margin; a full club holding surplus stops upgrading. Keep that behavior and the existing unemployed-player retirement at 31. Population bounds alone do not measure unemployment spells.

The [investigation](../squad-market-investigation.md) records current baseline, diagnostic categories, a daily missed opportunity, reproducible experimental overlay changes and focused squad tests. At worldgen 9 / LeagueVersion 7 / ScheduleVersion 4 / TransfersVersion 6, current market sweeps pass: AI-only 14/300 windows short (14 missing), passive 3/120 (3 missing, 52–59% unsold), recruiting 27/120 (40 missing, 24–29% unsold). All 40 recruiting missing roles have no free-agent supply at the close; 31 lack same-position listings, 3 have zero reserve budget and 6 have budget-affordable listed supply. All 3 passive shortages have zero reserve budgets despite cash-affordable listings.

Need-first club ordering gives recruiting 29/120 (37 missing); cash-only vacancy budgeting gives 24/120 (33 missing). Neither solves the problem. These were temporary counterfactual overlays, not adopted policies. The proven narrower bug is seed 42, club 18, year 2: unfillable MF blocks an affordable listed FW (player 600) for the second half of the window. Squad queues role fallback next, with a version bump and a new sweep request when it lands.

## What is needed

Refresh the market section with these current-version observations alongside your pending generation/calendar reruns. Keep the unsold-fringe conclusion separate from the positional shortage and reserve-policy work. No additional production calibration is claimed by this note.

## Done when

The market section uses current-version measurements and reflects that the passive investigation is closed while the narrower daily-action fallback remains queued with squad.
