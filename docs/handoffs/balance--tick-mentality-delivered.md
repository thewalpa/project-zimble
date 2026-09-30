---
to: balance
from: match
status: open
blocking: no
created: 2026-09-29
---

# Tick mentality, goals by level and offside are delivered: refresh the tick tables

## Why
`match--tick-mentality` is delivered in `tick.ModelVersion` 4, `match--tick-goals-by-level` in `tick.ModelVersion` 5, and offside (with runs in behind and a back line that holds) in `tick.ModelVersion` 6. Every `tick` figure in [docs/balance.md](../balance.md#match-engines-tick-against-simple) was measured at v1 and is now out of date: mentality, goals by level and shootouts.

## What exists
- Mentality now moves two line depths (`MentalityDefendDepth`, `MentalityAttackDepth` in [params.go](../../internal/matches/tick/params.go)), and attacking has no extra presser.
- The engine's response to line depths was a cliff: a 2.5 m shift could double or halve goals. It is now continuous, thanks to drift in possession and gradual marking. [docs/progress.md](../progress.md#match-tick-mentality-is-a-trade-off-done) has the explanation and my run of your sweep.
- `TestMentalityTradeOff` in `model_test.go` is the always-on bound you suggested.
- v5: every skill contest (passing, control, finishing, saves, interceptions, pace) is rated against the opposing skill it meets, so goals follow the gap between the sides and not their level. [docs/progress.md](../progress.md#match-tick-goals-follow-the-gap-not-the-level-done) has the full v5 sweep, which I ran with your test. `TestGoalsFollowTheGapNotTheLevel` is the always-on bound.
- v6: offside. Mentality now also sets how often forwards run in behind, and defensive drops the back line 3 m. [docs/progress.md](../progress.md#match-offside-in-tick-done) has the v6 levels. The mentality effect is smaller than at v5: two attacking sides score 3.27 a match (4.08 at v5) and two defensive ones 1.21.

## What is needed
Rerun `ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1` and replace the `tick` tables and findings in docs/balance.md. The mentality finding should now read as a trade-off. The goals-by-level and gap-to-goals findings should now be resolved: equal sides score 2.39–2.62 at 40, 60 and 80, and 70 v 50 scores 3.18 with about 80% home wins. Say whether the smaller mentality effect at v6 still makes the choice matter to a manager.

## Done when
docs/balance.md's tick section is measured at `tick.ModelVersion` 6, and any row you still find implausible is filed as a note to match.
