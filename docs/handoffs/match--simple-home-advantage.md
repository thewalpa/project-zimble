---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# The default engine has no home advantage

## Why
`simple` plays every career match unless the career chose `tick`. At `simple.ModelVersion` 4 equal 60 v 60 teams win 37.2% at home and 36.6% away, and score 1.37 goals at home to 1.35 away. At v3 it was 38.7% against 34.6%. Real top leagues are about 45% against 29%, and `tick` v6 gives 42.4% against 30.8%.

## What exists
`TestBalanceEngineComparison` ([internal/matches/tick/balance_test.go](../../internal/matches/tick/balance_test.go)), 3,000 matches, seeds 1, 42, 2026; `ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceEngineComparison -v -count=1 -timeout 2h`. `HomeAdvantagePermille` is 1050 in [params.go](../../internal/matches/simple/params.go). The rows 62 v 58 and 58 v 62 show the same: the home side wins 43.2% against 31.5%, and 32.3% against 42.8%, so the gap does the work and the venue almost none.

## What is needed
Decide whether `simple` should have a home advantage close to real football (about +5 points of home wins and −5 of away wins over equal teams, roughly 1.5 to 1.2 home to away goals). If yes, raise `HomeAdvantagePermille` (my estimate is that 1050 gives about 0.07 goals and about 1.5 points of win rate), bump `simple.ModelVersion` and its golden. If not, say why and I will stop flagging it.

## Done when
60 v 60 on `simple` gives home wins at least 4 points above away wins in `TestBalanceEngineComparison`.
