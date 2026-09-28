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

## Match engines: tick against simple

Measured 2026-09-28 at `tick.ModelVersion` 1 and `simple.ModelVersion` 3, both with their `DefaultParams`. Requested by `match` (note `balance--tick-engine-profile`).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1   # about 60 s on 32 cores
```

`TestBalanceEngineComparison` in `internal/matches/tick/balance_test.go` plays both engines on the same `enginetest.Input` teams: 11 starters and 7 substitutes, balanced unless stated, full condition, no commands. A team's "strength" is the rating its profile is built around, and it is close to its overall. Each row is 3,000 matches: seeds 1, 42 and 2026 × fixtures 1–1000, with each engine's own `matches.FixtureRandom` stream. Every match is played as a knockout. The contract guarantees that the knockout rule leaves the 90 minutes unchanged, so one run gives both the regulation result and the shootout. With 3,000 matches, a rate near 25% is good to about ±1.6 points (95%), and one near 50% to about ±1.8.

These are synthetic teams, not career squads. No career match uses `tick` yet. A league-season comparison has to wait until `app` can choose the engine per match (`match` roadmap phase 2).

### Equal teams, 60 v 60

| | simple | tick | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match | 2.75 | 2.92 | 2.6–2.9 |
| Home–away goals | 1.41–1.34 | 1.59–1.33 | 1.5–1.2 |
| Home / draw / away % | 38.7 / 26.6 / 34.6 | 44.0 / 24.9 / 31.1 | 45 / 26 / 29 |
| Draw % if the two scores were independent | 25.6 | 24.4 | |
| 0–0 % | 6.5 | 5.5 | 7–8 |
| 4 or more goals % | 30.0 | 33.5 | 25–30 |

| Total goals (% of matches) | 0 | 1 | 2 | 3 | 4 | 5 | 6+ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| simple | 6.5 | 17.8 | 24.6 | 21.0 | 14.6 | 8.9 | 6.6 |
| tick | 5.5 | 15.8 | 22.7 | 22.6 | 16.7 | 8.9 | 7.9 |

**Draws are not low.** Over 3,000 matches `tick` draws 24.9%. That is what independent scores with its goal rate would give (24.4%), and close to real football. The 21% that `match` saw came from 200 matches, where the 95% margin is about ±6 points. **`tick` has the more realistic home advantage:** 44% home wins against 31% away. `simple`'s home edge is only 4 points (39% against 35%), which is weak but not implausible, so no note was filed for it. `tick` scores a little more than real football, with 5.5% 0–0s and a third of matches reaching 4 goals.

### Rating gap (home strength v away strength)

| Match | simple H / D / A % | simple goals | tick H / D / A % | tick goals |
| --- | --- | --- | --- | --- |
| 70 v 50 | 62.9 / 22.2 / 14.9 | 2.90 | 92.7 / 5.2 / 2.1 | 4.27 |
| 65 v 55 | 51.6 / 24.1 / 24.3 | 2.79 | 72.0 / 16.3 / 11.7 | 3.34 |
| 62 v 58 | 44.0 / 26.3 / 29.7 | 2.75 | 54.8 / 23.6 / 21.6 | 3.01 |
| 60 v 60 | 38.7 / 26.6 / 34.6 | 2.75 | 44.0 / 24.9 / 31.1 | 2.92 |
| 58 v 62 | 33.3 / 26.4 / 40.3 | 2.73 | 28.7 / 24.9 / 46.5 | 2.94 |
| 55 v 65 | 26.8 / 26.0 / 47.3 | 2.78 | 16.8 / 21.9 / 61.3 | 3.07 |
| 50 v 70 | 16.4 / 22.7 / 60.9 | 2.88 | 4.2 / 8.8 / 86.9 | 3.87 |

In a career, squad averages range from 54 to 63 (see the market section), so the gaps that matter are the 58–62 and 55–65 rows. There, `tick` makes the stronger side a clear favourite: 72% for a home side 10 points stronger. That is steep, but a top-against-bottom match in a real league looks much like it. `simple` is flat: the same side wins 52%, and a 20-point gap still leaves 15% away wins. **`tick`'s gap turns into goals.** The stronger side scores more without the weaker one scoring much less, so a 20-point mismatch averages 4.3 goals and 61% of those matches have 4 or more. `simple` stays near 2.8 goals at every gap.

### Quality level

| Match | simple goals | simple draw % | tick goals | tick draw % | tick 0–0 % | tick 4+ goals % |
| --- | --- | --- | --- | --- | --- | --- |
| 40 v 40 | 2.81 | 25.8 | 2.08 | 29.8 | 11.8 | 16.1 |
| 60 v 60 | 2.75 | 26.6 | 2.92 | 24.9 | 5.5 | 33.5 |
| 80 v 80 | 2.71 | 27.1 | 4.64 | 20.2 | 0.8 | 69.3 |

**In `tick`, goals grow with absolute quality.** Two equal teams rated 80 score 4.6 a match, and two rated 40 score 2.1. Attack improves faster than defence. Real football shows no such trend. Career squads all sit between 54 and 63 today, so this barely shows yet. It will show in any league or cup whose teams are far from 60, and as ratings drift. Filed as `match--tick-goals-by-level`, together with the gap-to-goals effect.

### Mentality (60 v 60)

| Home v away | simple goals | simple H / D / A % | tick goals | tick H / D / A % |
| --- | --- | --- | --- | --- |
| balanced v balanced | 2.75 | 38.7 / 26.6 / 34.6 | 2.92 | 44.0 / 24.9 / 31.1 |
| attacking v attacking | 3.78 | 40.4 / 22.4 / 37.3 | 3.65 | 43.9 / 21.8 / 34.3 |
| defensive v defensive | 1.88 | 34.4 / 33.1 / 32.5 | 1.53 | 34.5 / 36.9 / 28.6 |
| attacking v balanced | 3.20 | 40.6 / 25.0 / 34.4 | 3.61 | 64.4 / 18.1 / 17.5 |
| balanced v attacking | 3.21 | 37.3 / 25.3 / 37.4 | 3.44 | 25.3 / 21.2 / 53.5 |
| defensive v balanced | 2.28 | 34.5 / 29.5 / 36.1 | 4.65 | 40.6 / 18.8 / 40.6 |
| balanced v defensive | 2.28 | 38.5 / 29.3 / 32.2 | 4.74 | 50.4 / 18.3 / 31.3 |
| attacking v defensive | 2.67 | 41.8 / 26.7 / 31.5 | 3.82 | 71.2 / 15.3 / 13.5 |
| defensive v attacking | 2.66 | 35.0 / 27.1 / 37.9 | 3.69 | 20.7 / 18.7 / 60.7 |

In `simple`, mentality works as a manager would expect. Attacking raises goals at both ends for a win rate 2–3 points higher, and defensive lowers them for 2–4 points fewer wins and more draws.

**In `tick`, attacking always pays and defensive backfires.** Against a balanced side, attacking lifts the win rate by 20 points (44% to 64% at home, 31% to 54% away), with no cost at the back. It is the dominant choice. A defensive side against a balanced or attacking one does not shut the game down: the match averages 4.7 goals, 0–0s almost vanish (0.7%), and the defensive side itself scores more than when it is balanced (2.34 at home against 1.59). Defensive lowers goals only when both sides play it. The AI always plays balanced (`ai.SelectionVersion` 3), so a manager on `tick` would win far more by always attacking. Filed as `match--tick-mentality`. It should be fixed before phase 2 puts the manager's live match on `tick`.

### Shootouts

A knockout tie goes to penalties exactly when it is level after 90 minutes: 25–27% of ties between equal teams in either engine (24.9% in `tick`). The sweep checks that the shootout count equals the draw count in every row. There is no extra time, which roughly doubles or triples the real-world shootout rate. That is a rules choice for `competitions` and `match`, not a calibration issue.

| Stronger side (home) wins the shootout, % | 60 v 60 | 62 v 58 | 65 v 55 | 70 v 50 |
| --- | --- | --- | --- | --- |
| simple | 49 | 59 | 74 | 88 |
| tick | 53 | 65 | 80 | 90 |

The away rows mirror these: the 50-rated side wins 8% (`simple`) or 12% (`tick`) of shootouts against a 70-rated side. Both sides together score 8.4–10.1 penalties per shootout in either engine.

**Shootouts follow the rating gap as strongly as open play does.** Real shootouts are close to a coin toss even between very unequal teams. In both engines, a side 10 points weaker in a career cup (65 v 55) reaches penalties 16–24% of the time and then wins only 20–26% of them, so it gets almost no second chance. This is live in the career now, through `simple` in the Continental Cup. Filed as `match--shootout-favourite`.
