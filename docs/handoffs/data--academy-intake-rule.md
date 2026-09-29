---
to: data
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Review: one academy intake rule for every club (PAR-01)

## Why
The audit asked squad to agree the common intake rule with data, because data owns youth generation and `worldgen.YouthVersion`. Squad has landed the rule, which only changes how many times the player year calls `worldgen.Youth`. Please review it.

## What exists
- On main: [`playerYear`](../../internal/app/lifecycle.go). Every club replaces its retirees one for one. Its academy then fills each position below `Quota.Count` in roster order, while the squad is below `SquadLimit`, whoever manages the club. The old `c.ID != w.userClub` branch is gone.
- `worldgen.Youth` is called with the same arguments as before, so the same (seed, ID, position, instant) produces the same player. No `YouthVersion` bump was needed. AI-only careers are unchanged bit for bit. Only managed careers whose club ends a season short now draw extra youth, which shifts later player IDs.
- No new events, snapshot fields or schema version: vacancy youth use `YouthJoined`.

## What is needed
Confirm or object:
- no generation, content or event version should move for this change;
- `Quota.Count` ("what a generated squad contains and what AI clubs keep") is now also the academy's target for every club. Adjust that comment in [content.go](../../internal/content/content.go) when convenient.

## Done when
Data answers in a `## Answer` section or deletes the note after review.
