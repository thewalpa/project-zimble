---
to: squad
from: balance
status: accepted
blocking: no
created: 2026-10-01
---

# With a passive manager most listed fringe players go unsold and the free-agent pool grows

## Why
The market sweep on 32 clubs at `ai.TransfersVersion` 6 (see ["AI transfer market" in docs/balance.md](../balance.md#ai-transfer-market)) shows two things that only happen when a manager does nothing. A manager who never touches contracts is a plausible player (a first-time one, a delegating one), so the market should still work around him.

## What exists
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceAIMarketWithPassiveManager -v -count=1`, seeds 7, 42, 99 and 2026, 30 years, a manager at club 3 who does nothing. The same seeds AI-only are in `TestBalanceAIMarketSweep`.

- **Listings go unsold.** 55–59% of the AI's listed players are still at their club at the window close, against 4–14% AI-only (8–15% at `ai.TransfersVersion` 2). Seed 7, year 10: 23 listed, 15 unsold; AI-only: 22 listed, 1 unsold. Sampled unsold players are fringe: rated 32–44, aged 31–34 or 16–18, at clubs averaging 57–63. Transfers per window: 36 against 48.
- **The pool grows.** At the window's open the pool holds 8 free agents in year 2, 15 in year 10 and 19 in year 30 (AI-only: always 4, the reserve); at the close 0, 5 and 9. The best one at the close rates 36–44 against squad averages of 59. The active population peaks at 659–665 (640–643 AI-only).
- With the recruiting manager (renews, signs, bids, lists; `managerPolicy`) both effects shrink: 24% unsold, pool 6–10 at the open and 0–2 at the close. But 24 of 120 windows then close with an AI club short of its roster (31 players in all), against 13 of 300 AI-only and 1 of 120 passive.

## What is needed
Say whether this is intended. Candidates, only if you think they are wrong: why fringe listings stop selling when the pool is large (does an AI club with a vacancy prefer the pool to the list, or does it not bid at all?); whether players with no club who nobody wants should retire or leave the pool sooner; and whether the 24 short windows with an active manager matter (the "thin market" backlog item). I haven't isolated any cause; the numbers are measurements only.

## Done when
You answer: a change with a version bump (I rerun the three sweeps and update the section), or a reason to leave it as is.

## Answer

Accepted 2026-10-01 into the thin-market backlog for a focused reproduction. A vacancy club may reasonably prefer a free agent over a fee for a fringe listed player; an unsold listing or larger pool alone does not require every player to find a buyer. The 24 short active-manager windows need investigation alongside the existing need-ordering/zero-budget case before changing policy. Keep shared legality and costs, without an AI insolvency exemption. No version change until the cause and an intended behavioral change are established.
