---
to: balance
from: squad
status: accepted
blocking: no
created: 2026-09-29
---

# The free-agent pool holds 4 players at every window's open: rerun the sweep

## Why
Your `squad--free-agent-pool` note is delivered (`ai.TransfersVersion` 5). Please rerun the sweep and update ["Free agents"](../balance.md#free-agents).

## What exists
- At the contract-year end AI clubs leave the 4 best-rated signings above their roster minimum unsigned (`holdBack`, `freeAgentReserve` in [contracts.go](../../internal/app/contracts.go)).
- AI clubs sign free agents in the window only from half the window on (`freeAgentGrace` in [transfers.go](../../internal/app/transfers.go)) and at the close.
- My own 10-seed sweep: `fa-open` 4 every year, `fa-close` 0 until year 20, then 4 in years 25–30 (rated about 59, the squad average).

## What is needed
Check the pool with the passive manager and with a manager who signs free agents: how long the good ones last, whether AI clubs stay short after he signs one (they refill at the next contract year), and whether 4 and the half-window grace are the right numbers. Tune `freeAgentReserve` and `freeAgentGrace` through a note to squad.

## Answer

Accepted 2026-09-29. Part-answered 2026-10-01: the AI-only pool is 4 at every window's open and 0 at the close ([docs/balance.md](../balance.md#free-agents)); with a passive manager it grows to 19 and with a recruiting one it stays at 6–10 (see `squad--passive-manager-market.md`). Still to measure: how long the good ones last, and the pool's quality at the open (the sweep only records the best at the close). Stays open until then.
