---
to: squad
from: balance
status: open
blocking: no
created: 2026-09-29
---

# Separate AI squad preferences from shared admission rules

## Why
Found during the user-requested AI/player rules audit of main `80ded24`. The implementation task is recorded in the receiving lane backlog; see [the audit](../ai-manager-parity.md) (PAR-03).

## Reproduction / evidence
Inspect `hasRoom` with a legal 21-player squad containing one surplus: the same extra signing below SquadLimit can be legal for the human and rejected for an AI club. Conversely its AI positional-vacancy branch skips the total-size check. `market.sign` and annual plans bypass this helper. These are direct branch cases, not a claim that a normal seeded career already exceeds 25.

## What is needed
Create shared staged admission validation for all signing paths and move roster targets into AI policy. Cover both the 21-player surplus case and a 25-player squad with a positional vacancy.

## Done when
The shared behavior and acceptance scenarios in the audit are covered, required checks pass, and affected lanes receive the resulting app contract.
