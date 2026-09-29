---
to: balance
from: squad
status: accepted
blocking: no
created: 2026-09-28
---

# Sellers now keep their stars: rerun the churn sweep

## Why
Your `squad--star-churn` note is delivered (`ai.TransfersVersion` 4). Please rerun the sweep and record the new market in `docs/balance.md`, together with the 32-club baseline you owe `data`.

## What exists
Four seller rules, in [internal/ai/transfers.go](../../internal/ai/transfers.go) and `market` in [internal/app/transfers.go](../../internal/app/transfers.go):
- **Selling price:** an AI club asks its valuation plus 6% for each point a player rates above its squad average (`ai.SellingPrice`, `KeyPermillePerPoint`).
- **Settling in:** it doesn't sell on an unlisted player it bought in the previous window.
- **Stars choose:** a player `ai.StarMargin` (10) or more above his club's average joins only a club at least as strong (`ai.Joins`).
- **Late in the window:** it sells a player it needs only while it can still replace him (`TransferWindow.NeededClose`).

My runs of `TestBalanceAIMarketSweep` (10 seeds, 30 years, means) against the same sweep before the change:

| | before | after |
| --- | --- | --- |
| stars16 per window | 15 | 4 (2-3 in years 1-10, 5-6 by year 30) |
| moves to a weaker club | 42% | 31% |
| longest streak per seed | 16-19 | 4-7, never a player who was ever among the 16 best (their longest is 1) |
| squad-average gap, top to bottom | 8-9 | 11 (64 to 53) |
| champions per league in 30 seasons | 11-15 | 10-15 |

Three of your targets are not met, on purpose:
- **Weaker moves at 31%, not under 25%.** The rest are mostly fringe players: a strong club's reserves are a weak club's upgrade, and surplus flows down. Stars no longer move down. A star margin of 5 gave 28% but a gap of 13; of 0, 27% and 14.
- **Streaks of 4-7** belong to listed journeymen bought as surplus cover and listed again when their buyer upgrades. The rule protects players a club wants to keep, not players it lists.
- **Stars move more as balances grow.** Rich clubs meet any price late in a career. Money has no sink yet; the `AI money` backlog item is where that belongs.

## What is needed
Rerun the sweep and update `docs/balance.md` ("Churn", "Strength and titles"). Tell me if the gap of 11 or the late rise in star moves is a problem for play; the knobs are `StarMargin` and `KeyPermillePerPoint`.

## Done when
`docs/balance.md` describes the market at `ai.TransfersVersion` 4.

## Answer

Accepted 2026-09-29 into the balance backlog alongside the AI/player parity audit. The requested measurements remain pending; no new sweep results are claimed in this audit.
