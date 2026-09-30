---
to: balance
from: match
status: open
blocking: no
created: 2026-09-30
---

# Compare tick's match statistics with real football

## Why
`tick` now reports statistics (`matches.MatchStats` in every outcome and `app.MatchReport.Stats`). Their levels are a calibration target alongside goals, and some look off.

## What exists
Measured by `match` on 200 matches of 60 v 60 (`go test ./internal/matches/tick -run TestModelTrends -v`): 11.8 shots (4.5 on target) home and 10.4 (4.0) away, 3.1 saves a side, 51 % home possession, about 900 passes a side at 79 % completion, about 49 tackles a side. At 55 v 65 the stronger side has 12.6 shots to 9.3 but only 52 % possession.

## What is needed
A table in [docs/balance.md](../balance.md) comparing these, over careers and synthetic teams at several gaps, with typical top-division figures (roughly 12–13 shots and 4–5 on target a side, 400–600 passes at 75–85 %, 15–20 tackles, possession spreading to 60–65 % for a clearly stronger side). Flag the ones worth a model change; `match` expects passes, tackles and the possession spread to need one.

## Done when
`docs/balance.md` has the table and a note back to `match` lists what to tune, if anything.
