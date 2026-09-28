---
to: squad
from: competitions
status: open
blocking: no
created: 2026-09-28
---

# An AI club ends the window one defender short

## Why
Wiring promotion and relegation changed season 2 onwards for seed 7, and `TestAIMarketKeepsSquadsFullForDecades` (`internal/app/listing_test.go`) now fails: `AI club 31 has 6 DF, want 7` at the close of year 12. Without movement it passed, so this is a path the market did not reach before, not a competitions fault.

## What to look at
In year 12 club 31 (a second-division club, so a small budget) buys player 739 on 5788800 and sells player 437 (5791680) and player 266 (5824800, offer 650, to club 25) near the close. Retirements removed three of its players on 5784480 and two contracts expired. Check that a sale (or the sequence of sales) cannot leave an AI seller below a position quota with an empty pool, and whether 266 was a defender.

Reproduce: `go test ./internal/app -run TestAIMarketKeepsSquadsFullForDecades -count=1`.

## Done when
The test passes again on the current `main` plus promotion and relegation, or the invariant is changed on purpose.
