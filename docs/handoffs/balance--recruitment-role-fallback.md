---
to: balance
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Remeasure all three markets after daily role fallback

## Why

The [investigation](../squad-market-investigation.md) proved a missed daily recruitment opportunity: seed 42 / club 18 / year 2 repeatedly chose an unfillable MF vacancy while listed FW player 600 was affordable. `ai.TransfersVersion` 7 now tries vacancies in descending need and canonical role order until it can stage one bid or signing. Need ordering between clubs, reserve policy, target margin, clocks, consent and bought/listed rules are unchanged.

## What exists

`ai.PrioritizedNeeds` and `market.recruit` implement fallback. Focused squad tests cover paid/free availability, ties, largest fillable need, one action, grace/deadlines, reserve-blocked fees, listings, consent, capacity and failed-plan retries. Existing save/retry, money conservation, deterministic season goldens and 15-year legality checks pass. No production population or economy tuning accompanies this change.

## What is needed

Rerun `TestBalanceAIMarketSweep`, `TestBalanceAIMarketWithPassiveManager` and `TestBalanceAIMarketWithRecruitingManager` with `ZIMBLE_BALANCE=1`, current generation/calendar and TransfersVersion 7. Compare against the investigation's version-6 baseline (AI-only 14/300 short windows, passive 3/120, recruiting 27/120 with 40 missing players). Report short windows/roles, remaining positional supply, unsold share, population bounds and accounting. If shortages persist, distinguish no supply from reserve-blocked listings rather than assuming role fallback solves either.

## Done when

`docs/balance.md` records all three current-policy sweeps and a squad note identifies remaining supply/reserve defects with seeds and years. Previous version-6 figures stay explicitly historical until refreshed.
