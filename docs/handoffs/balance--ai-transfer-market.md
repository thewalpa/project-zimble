---
to: balance
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Sweep the AI transfer market

## Why
AI clubs now trade every summer without the manager (`ai.TransfersVersion` 2). They buy upgrades, list the players these replace, and fill vacancies from the transfer list first. The rules were tuned on two seeds only. Please measure them over more seeds and longer careers.

## What exists
- The rules: [progress.md, "squad: the transfer list and AI transfers"](../progress.md) and `aiActions` / `listSurplus` in [transfers.go](../../internal/app/transfers.go). The knobs are `ai.UpgradeMargin` (8), `ai.ListingPermille` (800), `ai.TransferMargin` (5) and `ai.ReserveWeeks` (26).
- My measurement (30 AI-only years, seeds 7 and 42, after each window): 22–31 transfers per window, bids about equal to completions, every AI squad back at 20, and the population at 320.
- `TestAIMarketKeepsSquadsFullForDecades` in [listing_test.go](../../internal/app/listing_test.go) is the model for a long run.

## What is needed
A sweep over many seeds (AI-only and with a passive manager) reporting:
- transfers per window and per club, and how often a listed player goes unsold;
- whether strength concentrates: squad averages of the top and bottom clubs over time, and title winners;
- balances over decades (they already diverged before this change: AI clubs spend at most one upgrade a year);
- the free-agent pool after each window (AI clubs now sign most free agents during it; a manager may find few);
- anything that looks like churn for its own sake (a player moving every year).

## Done when
`docs/balance.md` has the numbers, and any tuning request comes back to `squad` as a note.
