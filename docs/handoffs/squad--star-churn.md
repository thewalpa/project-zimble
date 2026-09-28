---
to: squad
from: balance
status: open
blocking: no
created: 2026-09-28
---

# The best players change clubs every summer

## Why
Over 30 AI-only years (10 seeds), 12 of the 16 best players in the game move in every window, and the most-moved player moves 13–18 times. Seed 42's player 13 (rated 78–90) moved in 16 consecutive windows, never listed. Each buyer took him as its largest upgrade, and 9 of his 17 moves went to a weaker club than his seller. A club can't keep its star for even one season, and for a manager the league's best players will look random from year to year. The numbers are in [docs/balance.md, "Churn"](../balance.md#churn).

## What exists
- The rules at `ai.TransfersVersion` 2: `aiActions` and `market.candidates` in [transfers.go](../../internal/app/transfers.go), and `ai.AcceptBid` (`fee >= price`) in [ai/transfers.go](../../internal/ai/transfers.go).
- The sweep: `ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceAIMarketSweep -v -count=1`. The `stars16` column counts the 16 best players who moved, `weaker` counts moves to a weaker club, and `max-streak` is the longest run of consecutive windows one player moved in.

## What is needed
A rule, your choice, that keeps a club's key players unless it is really outbid. For example:
- sellers refuse, or ask a premium, for a player well above their squad average or among their best at his position;
- a player who moved in the last window isn't a candidate to move again;
- a player's price rises with his importance to his club, so a club's best player costs more than a typical upgrade.

Every bid currently completes: of 8,358 bids in the sweep, none was rejected or expired, and 106 collapsed. Sellers have no preference of their own today, so any of these rules adds one.

## Done when
Rerunning the sweep gives about these ranges (my suggestion; tell me if the design wants otherwise):
- 4 or fewer of the 16 best players move per window;
- a player's longest streak of consecutive moves is 3 or less;
- fewer than 25% of transfers go to a weaker club;
- squads stay full and strength stays level: a top-to-bottom squad-average gap of 10 points or less, and 6 or more different champions per league in 30 seasons.

I'll rerun it and update `docs/balance.md` after your change.
