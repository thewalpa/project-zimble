# Balance

Measurements of the seeded game over long careers: the current numbers, the seeds and versions behind them, and what changed. The `balance` lane keeps this file ([lane doc](lanes/balance.md)). Every number here comes from a test that skips unless `ZIMBLE_BALANCE=1` is set, and the command that reproduces it is given with it.

## AI transfer market

Measured 2026-09-28 at `ai.TransfersVersion` 2, `ai.ContractsVersion` 2, `ai.SelectionVersion` 3, `players.DevelopmentVersion` 1, `worldgen.YouthVersion` 1, `content.LeagueVersion` 3. Knobs: `ai.UpgradeMargin` 8, `ai.ListingPermille` 800, `ai.TransferMargin` 5, `ai.ReserveWeeks` 26. Requested by `squad` (note `balance--ai-transfer-market`).

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalance -v -count=1   # about 20 s
```

- `TestBalanceAIMarketSweep`: AI-only, 30 years, seeds 1, 2, 3, 5, 7, 11, 13, 42, 99 and 2026.
- `TestBalanceAIMarketWithPassiveManager`: the same with a manager at club 3 who does nothing (submits no lineup, answers no bid, lists no one, renews no one). Seeds 7, 42, 99 and 2026.

Each year runs to the close of the transfer window, which is where the market is measured. It then plays the season and cups and runs on to the contract-year end, where money is measured. The 15 or 16 AI clubs are split into two leagues of 8 (Founders League and Harbour League), plus the Continental Cup.

### Transfers per window (AI-only, mean over 10 seeds)

| | year 1 | year 5 | year 10 | year 20 | year 30 | all 30 years |
| --- | --- | --- | --- | --- | --- | --- |
| Bids made | 28 | 27 | 27 | 28 | 28 | 28 |
| Completed | 28 | 27 | 27 | 27 | 26 | 27 |
| Rejected / expired / collapsed | 0 / 0 / 1 | 0 / 0 / 1 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 |
| AI listings | 12 | 11 | 12 | 12 | 12 | 12 |
| Listed players still unsold at the close | 0 | 0 | 0 | 0 | 1 | 0 |
| Most moves in and out of one club | 6 | 6 | 5 | 5 | 5 | 6 |
| Moves to a club with a lower squad average | 11 | 11 | 12 | 12 | 11 | 12 |
| Of the 16 best players, moved in this window | 10 | 10 | 12 | 12 | 12 | 12 |

Per seed over 30 years: 26–28 transfers per window (range 22–32). Each AI club buys 35–62 players in 30 years. 0–3% of listed players go unsold.

**Every bid completes.** Of 8,358 AI bids in 300 windows, none was rejected or expired, and 106 collapsed. Sellers accept any bid at or above the price, even for their best player.

### Churn

Across the 10 seeds, 275–308 different players move in 30 years. Of them, 90–109 move three times or more, and 114–141 move in two consecutive windows or more.

**The best players move every year.** 12 of the 16 best players in the game change clubs in each window. The most-moved player moves 13–18 times. Seed 42's player 13 moved in 16 consecutive windows, from age 19 to 34, while rated 78–90, and once more at 36. He was never listed; each buyer took him as its largest upgrade. 9 of his 17 moves went to a club with a weaker squad than his seller's. Across all transfers, 44% go to a weaker club. This is churn for its own sake: a club's best player is the one every other club wants, and no rule protects him. Filed as `squad--star-churn`.

### Strength and titles (AI-only)

| | year 1 | year 5 | year 10 | year 20 | year 30 |
| --- | --- | --- | --- | --- | --- |
| Strongest AI squad average | 63 | 61 | 63 | 63 | 62 |
| Weakest AI squad average | 57 | 54 | 55 | 56 | 55 |
| Mean AI squad average | 60 | 58 | 59 | 59 | 59 |

**Strength doesn't concentrate.** The gap between the strongest and weakest squad stays at 6–8 points. Of the four strongest squads after the first window, 0–2 are still in the top four after the thirtieth. Over 30 seasons each 8-club league has 7 or 8 different champions, and the most titles for one club is 6–10. The Continental Cup has 12–15 different winners. The rotation of stars shown above is what keeps it level.

### Money (AI clubs, at each contract-year end, mean over 10 seeds)

| | year 1 | year 5 | year 10 | year 20 | year 30 |
| --- | --- | --- | --- | --- | --- |
| Lowest balance | 1.15M | 1.10M | 1.18M | 1.43M | 2.25M |
| Median balance | 2.16M | 3.10M | 4.48M | 5.90M | 8.47M |
| Highest balance | 3.91M | 6.29M | 8.63M | 12.49M | 15.42M |
| Clubs below zero | 0 | 0 | 0 | 0 | 0 |

**No club trends towards insolvency.** Every club starts on 2M, and the median club gains about 0.2M a year, because gate receipts exceed wages. Balances diverge: the spread between the richest and poorest club goes from 2.8M to 13.2M. Most of that spread comes from transfers: net spend per club over 30 years ranges from −8.7M to +7.9M. Fees (0.1–2M) are small against balances, so money rarely stops an AI bid. No tuning is requested yet. Money has no sink, which is worth revisiting when promotion and relegation or other costs land.

### Free agents

| | AI-only | passive manager |
| --- | --- | --- |
| Pool when the window opens | 0 every year | 2–4 in years 2–10, 0–2 later |
| Pool when the window closes | 0 every year | 0–1 |
| Best overall in the pool at the close, when not empty | – | 36–43 in years 2–10 (squad averages are about 59) |

**A manager finds no free agents.** AI clubs sign every useful free agent at the contract-year end. Whatever the manager's expiring contracts add is signed during the window. What's left is one player rated about 40. Filed as `squad--free-agent-pool`.

### With a passive manager (mean over 4 seeds)

The market runs a little slower: 24 transfers per window against 27, one fewer AI buyer. 8–15% of listings go unsold, against 0–3% AI-only. Churn, strength and money look like the AI-only numbers.

AI clubs never bid for the manager's players: 0 bids in 120 windows. They only bid for players he lists (the seller check in `market.candidates` in [transfers.go](../internal/app/transfers.go)), so incoming offers only reach a manager who lists someone. That is the rule as designed, not a bug. It does mean the incoming-offer flow never happens on its own.

A passive manager's squad average falls from 60 to 46–48 from year 5 on, because only the minimum quotas are refilled from the pool. His balance climbs to 29M by year 30.

The active population stays at 315–321 in every run.
