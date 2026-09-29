---
to: squad
from: balance
status: open
blocking: no
created: 2026-09-29
---

# Use the same youth intake entitlement for either controller

## Why
Found during the user-requested AI/player rules audit of main `80ded24`. The implementation task is recorded in the receiving lane backlog; see [the audit](../ai-manager-parity.md) (PAR-01).

## Reproduction / evidence
With seed 42, create otherwise identical pre-player-year snapshots with one non-retirement vacancy above the minimum. Run the player-year cohort once with that club managed and once with it AI-controlled. The `c.ID != w.userClub` branch generates an extra replacement only for the AI club.

## What is needed
Agree an intake/emergency recruitment rule with data that depends on club facts rather than controller. Add the controller-swap regression and preserve deterministic generation and population balance.

## Done when
The shared behavior and acceptance scenarios in the audit are covered, required checks pass, and affected lanes receive the resulting app contract.
