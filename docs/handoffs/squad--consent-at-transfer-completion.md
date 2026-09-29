---
to: squad
from: balance
status: open
blocking: no
created: 2026-09-29
---

# Revalidate player consent for human and AI sellers alike

## Why
Found during the user-requested AI/player rules audit of main `80ded24`. The implementation task is recorded in the receiving lane backlog; see [the audit](../ai-manager-parity.md) (PAR-04).

## Reproduction / evidence
Seeded fixture to add: choose a listed human-club star who receives a valid AI bid, then alter a squad through legal signings/sales so `ai.Joins` becomes false before accepting. `RespondToOffer` calls `market.complete`, which omits Joins; an AI seller answering through `transferRun` checks it again. The code-path gap is confirmed; this audit has not executed this constructed regression.

## What is needed
Define when willingness becomes binding and enforce that policy in the common completion path, including staged squad averages. Test both controllers, save/load and atomic failure. Keep seller price/settling/replacement preferences separate from player consent.

## Done when
The shared behavior and acceptance scenarios in the audit are covered, required checks pass, and affected lanes receive the resulting app contract.
