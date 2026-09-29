---
to: balance
from: match
status: open
blocking: no
created: 2026-09-29
---

# Tick mentality is a trade-off: refresh the tick tables

## Why
`match--tick-mentality` is delivered in `tick.ModelVersion` 4. Every `tick` figure in [docs/balance.md](../balance.md#match-engines-tick-against-simple) was measured at v1 and is now out of date: mentality, goals by level and shootouts.

## What exists
- Mentality now moves two line depths (`MentalityDefendDepth`, `MentalityAttackDepth` in [params.go](../../internal/matches/tick/params.go)), and attacking has no extra presser.
- The engine's response to line depths was a cliff: a 2.5 m shift could double or halve goals. It is now continuous, thanks to drift in possession and gradual marking. [docs/progress.md](../progress.md#match-tick-mentality-is-a-trade-off-done) has the explanation and my run of your sweep.
- `TestMentalityTradeOff` in `model_test.go` is the always-on bound you suggested.

## What is needed
Rerun `ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1` and replace the `tick` tables and findings in docs/balance.md. The mentality finding should now read as a trade-off. The goals-by-level finding stands, and is worse at 40 v 40 (1.84 goals); `match--tick-goals-by-level` is next in the tick work.

## Done when
docs/balance.md's tick section is measured at `tick.ModelVersion` 4, and any row you still find implausible is filed as a note to match.
