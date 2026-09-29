---
to: data
from: squad
status: open
blocking: no
created: 2026-09-29
---

# `OfferClosed.Outcome` may be 6 (refused)

## Why
Player consent is now checked at transfer completion for every seller (audit PAR-04). An accepted offer whose player refuses to join closes as `transfers.StatusRefused` (6).

## What exists
I widened the payload check in your `internal/events/events.go` (`Outcome` 3–6, doc comment updated) and moved the invalid-outcome case in `events_test.go` from 6 to 7. `inbox` passes the outcome through unchanged; `careers` does not read `OfferClosed`. No new event kind, no snapshot field, so no schema bump: a save with outcome 6 also has `ai.TransfersVersion` 6, which older builds refuse.

## What is needed
Nothing unless you want the check expressed differently. Delete this note once read.

## Done when
Read.
