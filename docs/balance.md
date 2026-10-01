# Balance

Measurements of the seeded game over long careers: the current numbers, the seeds and versions behind them, and what changed. The `balance` lane keeps this file ([lane doc](lanes/balance.md)). Every number here comes from a test that skips unless `ZIMBLE_BALANCE=1` is set, and the command that reproduces it is given with it.

## AI transfer market

Measured 2026-10-01 on 32 clubs (four leagues of 8) at `ai.TransfersVersion` 6, `ai.ContractsVersion` 2, `ai.SelectionVersion` 3, `players.DevelopmentVersion` 1, `worldgen.Version` 8, `worldgen.YouthVersion` 4, `content.Version` 9, `content.LeagueVersion` 5, `competitions.ScheduleVersion` 3, `medical.Version` 3, at commit `15ff2fa`. Knobs: `ai.UpgradeMargin` 8, `ai.ListingPermille` 800, `ai.TransferMargin` 5, `ai.ReserveWeeks` 26, `ai.StarMargin` 10, `ai.KeyPermillePerPoint` 60 (6%), `freeAgentReserve` 4. Answers `balance--rerun-baseline-32-clubs`, `balance--star-churn-delivered`, `balance--free-agent-pool-delivered` and `balance--consent-at-completion-delivered`. The previous baseline (16 clubs, `ai.TransfersVersion` 2) is in the [history](#history).

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceAIMarket -v -count=1   # about 100 s on 32 cores
```

- `TestBalanceAIMarketSweep`: AI-only, 30 years, seeds 1, 2, 3, 5, 7, 11, 13, 42, 99 and 2026.
- `TestBalanceAIMarketWithPassiveManager`: the same with a manager at club 3 who does nothing (submits no lineup, answers no bid, lists no one, renews no one, signs no one). Seeds 7, 42, 99 and 2026.
- `TestBalanceAIMarketWithRecruitingManager`: the same seeds with a scripted manager (`managerPolicy` in `internal/app/balance_test.go`). Every year he renews the expiring players who rate within 2 of his squad average (and any whose position would otherwise fall below its minimum), signs the best free agent who beats his position's weakest by 3 (or fills a position under its minimum), bids at the asking price for up to two listed players a day who beat his position's weakest by 4 and cost under a quarter of his balance, lists up to two surplus players when the squad is within 2 of the limit, and accepts every bid for his players. He does not choose lineups (the fitted one plays). It is one reasonable policy, not an optimum.

Each year runs to the close of the transfer window, read day by day (the journal keeps only the latest 1,000 events, and a 32-club window overflows it). It then plays the season, the promotion play-offs and the cups, and runs on to the contract-year end, where money is measured. The 31 AI clubs (32 when AI-only) play in two leagues of 8 and their two second divisions, with promotion and relegation, plus the Continental Cup. Counts below are AI-to-AI transfers; the manager's own purchases and sales are counted separately.

### Transfers per window (AI-only, mean over 10 seeds)

| | year 1 | year 5 | year 10 | year 20 | year 30 | all 30 years |
| --- | --- | --- | --- | --- | --- | --- |
| Bids made | 57 | 56 | 55 | 59 | 57 | 56 |
| Completed | 53 | 50 | 47 | 48 | 46 | 48 |
| Rejected / expired / collapsed | 0 / 0 / 1 | 1 / 0 / 1 | 2 / 0 / 1 | 3 / 0 / 2 | 2 / 0 / 1 | 2 / 0 / 1 |
| Refused by the player | 3 | 4 | 4 | 6 | 7 | 5 |
| AI listings | 24 | 20 | 19 | 20 | 18 | 20 |
| Listed players still unsold at the close | 0 | 1 | 2 | 2 | 1 | 1 |
| Most moves in and out of one club | 7 | 7 | 7 | 7 | 7 | 7 |
| Moves to a club with a lower squad average | 18 | 17 | 17 | 16 | 16 | 17 |
| Of the 16 best players, moved in this window | 2 | 1 | 3 | 5 | 5 | 3 |

Per seed over 30 years: 46–48 transfers per window (range 40–58), about 1.5 per club, against 27 per window on 16 clubs. Each AI club buys 33–59 players in 30 years. 4–14% of listed players go unsold.

**Refusals grow with the years.** Consent at completion (`ai.Joins`) closes 3 offers per window as refused at the start and 7 at year 30, out of about 57 bids, while completions fall from 53 to 46. Over 30 years about 9% of bids end as refusals. They grow together with the squad-strength gap (below), which fits the rule that a star joins only a club at least as strong, but I haven't isolated the cause. A refused offer moves nothing. No problem for play yet; worth watching if completions keep falling.

**AI squads rarely end a window short.** 13 of 300 AI-only windows (4%) close with at least one AI club below its roster, 16 players missing in all, 1–2 players at a time. With a passive manager: 1 of 120. With the recruiting manager: 24 of 120 (20%) windows, 31 players missing in all, more often than before the recruiting manager existed (I didn't run it earlier). It is a short-lived gap: squads are measured at the window close, and `squad` says the next player year refills them. It is the "thin market" item in `docs/lanes/squad.md`. The sweep doesn't show how many of those clubs have a zero transfer budget.

### Churn

Across the 10 seeds, 741–796 different players move in 30 years (275–308 on 16 clubs), of whom 168–188 move three times or more, and 54–72 move in two consecutive windows or more. The most-moved player moves 6–8 times (13–18 before the star rules) and the longest streak is 4–6 consecutive windows (16–19 before).

**Stars no longer churn.** 1–3 of the 16 best players move per window in the first ten years, 5 by year 30 (12 of 16 before `ai.TransfersVersion` 4). 17 of 48 completed moves (35%) go to a club with a lower squad average than the seller's: down from 44%, but above the 31% that `squad` measured on 16 clubs, and the same late in a career (16 of 46 at year 30). The remainder is fringe surplus flowing down, as `squad` expected. No note is filed.

### Strength and titles (AI-only)

| | year 1 | year 5 | year 10 | year 20 | year 30 |
| --- | --- | --- | --- | --- | --- |
| Strongest AI squad average | 63 | 62 | 64 | 65 | 64 |
| Weakest AI squad average | 56 | 52 | 52 | 53 | 52 |
| Mean AI squad average | 60 | 58 | 59 | 59 | 59 |

**The gap widens from 7 to 11–12 points and then stays there.** The strongest squad stays at 62–65, the weakest drops from 56 to 52. The first and second divisions are generated equally strong (`data` note), and the measurement doesn't separate the divisions. Of the four strongest squads after the first window, 0–1 are still in the top four after the thirtieth.

**Strength does not concentrate in titles.** With promotion and relegation, each of the two first divisions has 10–15 different champions in 30 seasons and the most titles for one club is 4–8; the second divisions have 12–16 champions (most 3–7). The Continental Cup has 14–20 winners (most 3–5). The counts are not comparable with the 16-club baseline (7–8 champions per league): clubs now move between divisions, which gives more champions. No club wins dynastically, so the wider squad gap does not yet turn into titles.

### Money (AI clubs, at each contract-year end, mean over 10 seeds)

| | year 1 | year 5 | year 10 | year 20 | year 30 |
| --- | --- | --- | --- | --- | --- |
| Lowest balance | 1.09M | 0.92M | 0.83M | 0.98M | 1.02M |
| Median balance | 2.08M | 2.43M | 2.96M | 4.55M | 6.50M |
| Highest balance | 3.88M | 9.23M | 14.37M | 17.54M | 24.38M |
| Clubs below zero | 0 | 0 | 0 | 0 | 0 |

**No club trends towards insolvency**, but the lowest balance sits near 1M for 30 years while the median grows and the highest reaches 24M: the poorest club gains nothing in the long run. The median gains 0.15M a year (0.2M on 16 clubs), because gate receipts exceed wages. Net transfer spend over 30 years, per club, ranges across seeds from −8.9M…−19.9M at the low end (a net seller) to +5.4M…+8.8M at the high end: a few clubs sell much more than any club buys, which the sweep doesn't connect to who ends up rich. No tuning is requested yet: money still has no sink. The `AI money` item in `docs/lanes/squad.md` is where this belongs.

### Free agents

| | AI-only | passive manager | recruiting manager |
| --- | --- | --- | --- |
| Pool when the window opens, year 2 / 10 / 30 | 4 / 4 / 4 | 8 / 15 / 19 | 6 / 7 / 10 |
| Pool when the window closes, year 2 / 10 / 30 | 0 / 0 / 0 | 0 / 5 / 9 | 0 / 0 / 2 |
| Best overall in the pool at the close, when not empty | 28 (rare) | 36–44 | 26–36 |

**The pool is the designed 4 at every window's open when AI-only, and empty at the close** (`freeAgentReserve` 4, `freeAgentGrace` half a window). With a passive manager it is not: it rises to 19 at the open and 9 at the close by year 30. His expiring players are never signed back, and the AI clubs pass over what is left: the best in the pool at the close rates 36–44 against squad averages of 59. The active population peaks at 659–665 (640–643 AI-only). It is harmless for play, but unwanted players stay a long time in the pool; I haven't measured how long, or whether they retire at the usual age.

**A manager who signs does find players.** The recruiting manager signs 2–4 free agents a year. What is left at the close rates 26–36 (36–44 when passive): the pool holds what AI clubs didn't want. `squad` measured the reserve's 4 players at about the squad average (59) at the window's open in its own runs; this sweep only records the best player at the close, so I don't know how good the manager's signings are, nor how long the good ones last (asked in the note). Not measured yet: the quality of the pool at the open. The half-window grace and a reserve of 4 keep AI squads full (see the vacancy count above); whether they are the right numbers for a manager depends on that.

### Passive manager (mean over 4 seeds)

| year | squad average | balance | bids for his players |
| --- | --- | --- | --- |
| 1 | 60 | 2.4M | 0 |
| 3 | 55 | 3.8M | 0 |
| 5 | 51 | 5.5M | 0 |
| 10 | 51 | 10.1M | 0 |
| 30 | 51 | 28.5M | 0 |

The market runs slower (36 completed transfers a window against 48), and **55–59% of the listed players go unsold, against 4–14% AI-only and 8–15% at the previous baseline**. A sample of the unsold ones (seed 7, years 5 and 10) are fringe players: rated 32–44, mostly aged 31–34 or 16–18, at clubs averaging 57–63. I haven't isolated the cause. The free-agent pool is large in the same runs and the numbers fit a world where the pool replaces the listed fringe, but that is a guess; a passive manager's world simply loses most of its fringe trade. Seed 7, year 10: 23 listed, 15 unsold, pool 5; the same seed AI-only: 22 listed, 1 unsold, pool 0. Filed to `squad` as `squad--fringe-listings-unsold`.

AI clubs bid for none of the passive manager's players (0 bids in 120 windows), as before: AI clubs only bid for the players he lists. A passive manager's squad average falls from 60 to 51 by year 5 and then stays there, because only the minimum quotas are refilled. He is not relegated within the measured years, but this sweep doesn't record his division (a gap). His balance climbs to 28M, so a manager who does nothing is richer than any AI club, which reinforces the money item above. Titles in 30 seasons, over all competitions: 1–4 (the sweep doesn't record his division).

### Recruiting manager (mean over 4 seeds)

| year | squad average | balance | signed | bought | sold | renewed | bids for his players |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 60 | 2.4M | 0 | 0 | 0 | 3 | 0 |
| 2 | 59 | 3.6M | 2 | 1 | 2 | 3 | 2 |
| 5 | 59 | 5.5M | 4 | 1 | 2 | 4 | 2 |
| 10 | 61 | 6.7M | 4 | 0 | 3 | 5 | 3 |
| 30 | 62 | 13.2M | 3 | 0 | 1 | 3 | 1 |

A simple recruiting policy keeps the managed club at 59–62, one point above the AI mean (59) and 8–11 above the passive one. Over 30 seasons that club's squad average is 60–62 per seed (range 53–68) and it wins 3–7 titles (all competitions), against 1–4 for the passive manager. It is not dominant: the policy buys 0–1 players a year (I haven't measured why: asking prices against his budget limit, or few listed upgrades). His balance still climbs to 13M. None of his commands was refused. AI clubs bid for 1–3 of the players he lists each window and he accepts every bid. This policy does not test lineups, rotation, injuries or tactics: see the backlog.

### History

- **2026-09-28**, 16 clubs, `ai.TransfersVersion` 2: 27 transfers a window, every bid completed, the 16 best players 12 a window, strength gap 6–8, median balance 8.5M at year 30, free-agent pool empty (AI-only) or 2–4 (passive). Filed `squad--star-churn` and `squad--free-agent-pool`.
- **2026-10-01**, 32 clubs, `ai.TransfersVersion` 6: this section.

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
