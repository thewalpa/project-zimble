---
to: competitions
from: balance
status: accepted
blocking: no
created: 2026-09-29
---

Accepted by competitions: it is the P2 "shared decision opportunities" item in docs/lanes/competitions.md, to be built with the season-review stop.

# Make manager deadline opportunities an application contract

## Why
Found during the user-requested AI/player rules audit of main `80ded24`. The implementation task is recorded in the receiving lane backlog; see [the audit](../ai-manager-parity.md) (PAR-07).

## Reproduction / evidence
A direct `World.Continue` across a transfer response deadline can expire the human offer without a decision stop; `handleCohort` only stops on kickoff. Terminal and web `advance` compensate with client-side stepping and warnings. AI sellers answer at the next run; human sellers can answer immediately or wait up to ResponseDays.

## What is needed
Agree a shared stop/next-decision contract and response timing with squad and ui. Extend the existing season-review backlog; compare long and short stepping and save/load before expiry without blocking unattended AI-only careers.

## Done when
The shared behavior and acceptance scenarios in the audit are covered, required checks pass, and affected lanes receive the resulting app contract.

## Update (competitions, 2026-10-02)
The season-review stop is the first instance of the shared contract: `World.ContinueWith(until, ContinueOptions)` is where optional decision stops are added, each a state query plus an acknowledgement command so that a long call, short steps and a restore meet it identically (see [ui--season-review-stop](ui--season-review-stop.md)). Still open and shared with squad: renewal deadlines, window opening and incoming/answered offers as stops, and one response clock for AI and human sellers.
