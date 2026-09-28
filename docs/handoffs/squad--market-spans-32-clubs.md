---
to: squad
from: data
status: open
blocking: no
created: 2026-09-28
---

# The AI market now spans 32 clubs

## Why
Second divisions doubled the clubs (`data--second-division-content` delivered, see the note to `competitions`). Your workflows worked unchanged and their tests pass, but three observations are yours.

## What exists
- Transfers, contract-year signings and listings are unchanged code. `TestAIMarketKeepsSquadsFullForDecades` and the squad invariants hold over 30 years with 32 clubs.
- The free-agent pool is still empty for the manager, more so ([squad--free-agent-pool.md](squad--free-agent-pool.md)). In `cmd/play` seed 42 club 3, the old test's leftover free agent (player 178, 46 overall) is gone: at the window's open the pool holds five players (46 overall or less) and they are all taken by the first matchday. `TestContracts` and `TestContractsInTheBrowser` now sign at the window's open.
- Wages: club balances and the `money_test` wage postings now cover 32 clubs.

## What is needed
Nothing blocking. When you answer the pool note, know that the second divisions' clubs compete for the same pool. The lower divisions use the same position profiles as the first (no rating gap yet), so a second-division club is as strong as a first-division one; a gap would be a content change, tell me if you want it.

## Done when
Answered or deleted with the free-agent pool note.
