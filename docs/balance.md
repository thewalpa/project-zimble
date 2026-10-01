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

Measured 2026-10-01 at `tick.ModelVersion` 6 and `simple.ModelVersion` 4, both with their `DefaultParams`, at commit `78fc6b2`. Answers `balance--tick-mentality-delivered`. The earlier measurement (`tick` v1, `simple` v3, 2026-09-28) is in the [history](#history-1).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceEngineComparison' -v -count=1 -timeout 2h        # about 13 min on 4 cores
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceMentalityByGap' -v -count=1 -timeout 2h          # about 8 min on 4 cores
```

(`go test`'s default 10-minute timeout is too short on a small machine: pass `-timeout`.)

`TestBalanceEngineComparison` in `internal/matches/tick/balance_test.go` plays both engines on the same `enginetest.Input` teams: 11 starters and 7 substitutes, balanced unless stated, full condition, no commands. A team's "strength" is the rating its profile is built around, and it is close to its overall. Each row is 3,000 matches: seeds 1, 42 and 2026 × fixtures 1–1000, with each engine's own `matches.FixtureRandom` stream. Every match is played as a knockout. The contract guarantees that the knockout rule leaves the 90 minutes unchanged, so one run gives both the regulation result and the shootout. With 3,000 matches, a rate near 25% is good to about ±1.6 points (95%), one near 50% to about ±1.8, and a goal average to about ±0.04.

These are synthetic teams, not career squads; the career-season comparison is in ["Career seasons: tick against simple"](#career-seasons-tick-against-simple).

### Equal teams, 60 v 60

| | simple | tick | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match | 2.71 | 2.46 | 2.6–2.9 |
| Home–away goals | 1.37–1.35 | 1.34–1.11 | 1.5–1.2 |
| Home / draw / away % | 37.2 / 26.3 / 36.6 | 42.4 / 26.8 / 30.8 | 45 / 26 / 29 |
| Draw % if the two scores were independent | 25.9 | 27.3 | |
| 0–0 % | 5.8 | 7.6 | 7–8 |
| 4 or more goals % | 28.3 | 23.0 | 25–30 |

| Total goals (% of matches) | 0 | 1 | 2 | 3 | 4 | 5 | 6+ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| simple | 5.8 | 18.7 | 25.6 | 21.6 | 14.5 | 7.6 | 6.2 |
| tick | 7.6 | 21.6 | 26.7 | 21.0 | 12.5 | 7.0 | 3.5 |

**`tick` v6 is now a little low, where v1 was a little high.** 2.46 goals is under the 2.6–2.9 range, 4+ goals are under a quarter, and the home edge (42% against 31% away) is a few points short of real football; the 0–0 rate and the draw rate are right. The offside rule and the gap-based contests took goals out. Not worth a note: it is inside what `match` was tuning, and 2.4 is where `match` said it would land. The ratio is what matters for play, and home goals are 1.2 times away goals, close to the real 1.25. `simple` is inside the real range on goals but has lost its home advantage: 37.2% home wins against 36.6% away, and 1.37 home goals to 1.35 (v3: 38.7% against 34.6%). Its `HomeAdvantagePermille` is 1050, a 5% chance boost that is about 0.07 goals, below what 3,000 matches can resolve (±0.04 on each average). `simple` is the career default, so a managed club gains nothing from playing at home. Filed as `match--simple-home-advantage`.

### Rating gap (home strength v away strength)

| Match | simple H / D / A % | simple goals | tick H / D / A % | tick goals |
| --- | --- | --- | --- | --- |
| 70 v 50 | 62.5 / 21.0 / 16.5 | 2.88 | 80.8 / 13.4 / 5.8 | 3.18 |
| 65 v 55 | 50.4 / 23.5 / 26.1 | 2.75 | 62.1 / 22.0 / 15.9 | 2.62 |
| 62 v 58 | 43.2 / 25.2 / 31.5 | 2.72 | 51.4 / 24.2 / 24.4 | 2.54 |
| 60 v 60 | 37.2 / 26.3 / 36.6 | 2.71 | 42.4 / 26.8 / 30.8 | 2.46 |
| 58 v 62 | 32.3 / 24.9 / 42.8 | 2.75 | 36.0 / 26.7 / 37.3 | 2.52 |
| 55 v 65 | 26.4 / 24.1 / 49.5 | 2.76 | 25.0 / 26.0 / 49.0 | 2.52 |
| 50 v 70 | 16.3 / 21.5 / 62.2 | 2.86 | 11.0 / 18.5 / 70.5 | 2.81 |

In a career, squad averages range from 51 to 65 (see the market section), so the gaps that matter are the 62 v 58 and 65 v 55 rows. **`tick`'s gap now turns into wins without runaway scores:** a home side 10 points stronger wins 62% (`simple`: 50%) and a 20-point mismatch averages 3.2 goals (4.3 at v1) with 39% of matches reaching 4 goals (61% at v1). The weaker side still wins 6% of 70 v 50 matches in `tick` and 17% in `simple`. Both are plausible; `tick` is steeper, which is the more realistic of the two between a title favourite and a relegation candidate.

### Quality level

| Match | simple goals | simple draw % | tick goals | tick draw % | tick 0–0 % | tick 4+ goals % |
| --- | --- | --- | --- | --- | --- | --- |
| 40 v 40 | 2.78 | 25.7 | 2.53 | 25.9 | 7.3 | 23.9 |
| 60 v 60 | 2.71 | 26.3 | 2.46 | 26.8 | 7.6 | 23.0 |
| 80 v 80 | 2.68 | 25.2 | 2.52 | 26.5 | 7.6 | 23.9 |

**Resolved: goals no longer grow with absolute quality.** `tick` was at 2.1, 2.9 and 4.6 goals at 40, 60 and 80 (v1); it is now 2.5 at every level (the 0.07 spread is above the ±0.04 sampling margin but small), with draw and 0–0 rates flat. `match--tick-goals-by-level` is delivered.

### Mentality

Points per match are 3 × wins + draws, from the home or the named side's view.

| Home v away (60 v 60) | simple goals | simple H / D / A % | tick goals | tick H / D / A % |
| --- | --- | --- | --- | --- |
| balanced v balanced | 2.71 | 37.2 / 26.3 / 36.6 | 2.46 | 42.4 / 26.8 / 30.8 |
| attacking v attacking | 3.79 | 40.5 / 20.2 / 39.3 | 3.13 | 45.3 / 22.7 / 32.0 |
| defensive v defensive | 1.83 | 33.5 / 32.9 / 33.6 | 1.39 | 35.2 / 39.1 / 25.7 |
| attacking v balanced | 3.23 | 40.4 / 23.5 / 36.1 | 2.87 | 51.4 / 23.6 / 25.0 |
| balanced v attacking | 3.23 | 37.2 / 23.4 / 39.5 | 2.76 | 37.5 / 24.9 / 37.6 |
| defensive v balanced | 2.23 | 33.0 / 30.4 / 36.6 | 1.85 | 38.5 / 31.8 / 29.7 |
| balanced v defensive | 2.23 | 36.7 / 30.0 / 33.2 | 1.87 | 40.3 / 32.2 / 27.5 |
| attacking v defensive | 2.62 | 40.7 / 26.4 / 32.9 | 2.17 | 47.3 / 27.7 / 25.0 |
| defensive v attacking | 2.62 | 33.2 / 27.0 / 39.8 | 2.08 | 35.3 / 30.0 / 34.7 |

The same by the rating gap (`TestBalanceMentalityByGap`: the side named plays the mentality, the other is balanced unless stated), points per match for that side:

| Match | balanced | defensive | attacking | goals for / against, balanced → attacking (`tick`) |
| --- | --- | --- | --- | --- |
| Underdog, 55 v 65 at home: `tick` | 1.01 | 0.95 | 1.16 | 0.99 / 1.53 → 1.26 / 1.58 |
| Underdog, 55 v 65 at home: `simple` | 1.03 | 0.99 | 1.06 | 1.13 / 1.63 → 1.36 / 1.91 |
| Favourite, 65 v 55 at home: `tick` | 2.08 | 1.97 | 2.28 | 1.82 / 0.79 → 2.27 / 0.81 |
| Favourite, 65 v 55 at home: `simple` | 1.75 | 1.62 | 1.83 | 1.64 / 1.11 → 2.01 / 1.29 |
| Underdog, 65 v 55 away: `tick` | 0.70 | 0.66 | 0.81 | 0.79 / 1.82 → 1.05 / 1.93 |
| Underdog, 65 v 55 away: `simple` | 1.02 | 0.99 | 1.04 | 1.11 / 1.64 → 1.35 / 1.91 |

**In `simple` mentality is a trade-off. In `tick` v6 attacking is still a free upgrade, and defensive never pays.** The backfire of v1 is gone (a defensive side now concedes and scores less, 1.85 goals with a defensive home side against 2.46, and 0–0s rise from 7.6% to 15%), and so is the 20-point swing: attacking against a balanced side is worth +0.24 points a match at home and +0.19 away (about 4–9 points over a 38-match season; `simple` +0.07), and defensive costs 0.04–0.11. That makes it matter, but as a dominant choice, not a choice: attacking beats balanced for the favourite (+0.20), for the underdog at home (+0.15) and away (+0.11),. The reason is in the goals: attacking adds 0.25–0.45 goals scored a match for `tick` and only 0.02–0.11 conceded (about 1–3 standard errors); `simple` concedes 0.2–0.3 more. A defensive underdog gets more draws (+3.6 points of draw rate) and fewer wins (−3.1) and ends 0.06 points a match worse off, so defensive is not a tool for the weaker side either. Nothing outside the goal model charges for attacking: `medical` and `app` do not read mentality (condition drain and injury exposure depend on minutes only). The AI always plays balanced (`ai.SelectionVersion` 3). The sweep sets one mentality for 90 minutes, so protecting a lead (switching late) is not measured. Filed as `match--attacking-is-free`.

### Shootouts

A knockout tie goes to penalties exactly when it is level after 90 minutes: 26–27% of ties between equal teams in either engine at the current versions (26.8% in `tick`). The sweep checks that the shootout count equals the draw count in every row. There is no extra time, which roughly doubles or triples the real-world shootout rate. That is a rules choice for `competitions` and `match`, not a calibration issue.

| Home (stronger) side wins the shootout, % | 60 v 60 | 62 v 58 | 65 v 55 | 70 v 50 |
| --- | --- | --- | --- | --- |
| simple | 49 | 54 | 55 | 61 |
| tick | 53 | 54 | 57 | 60 |

At 50 v 70 the home side is the weaker one and wins 35% (`simple`) or 42% (`tick`) of the shootouts. Both sides together score 8.1–8.6 penalties per shootout in either engine.

**Resolved: shootouts no longer follow the rating gap.** At v1 and v3 the stronger side won 74–80% of shootouts at 65 v 55 and 88–90% at 70 v 50; it now wins 55–57% and 60–61%, close to the coin toss real shootouts are. `match--shootout-favourite` is delivered in both engines.

### History

- **2026-09-28**, `tick` v1, `simple` v3: `tick` drew 24.9% and scored 2.9 goals at 60 v 60, 4.6 at 80 v 80 and 2.1 at 40 v 40, a 10-point mismatch gave 72% wins and a 20-point one 4.3 goals. Attacking raised the win rate by 20 points with no cost at the back; a defensive side against a balanced one conceded 4.7 goals. A stronger side won 74–80% of shootouts at 65 v 55. Filed `match--tick-goals-by-level`, `match--tick-mentality` and `match--shootout-favourite`.
- **2026-10-01**, `tick` v6, `simple` v4: this section.

## Career seasons: tick against simple

Measured 2026-10-01 at `tick.ModelVersion` 6 and `simple.ModelVersion` 4 (both `DefaultParams`), `medical.Version` 3, `worldgen.Version` 8, `content.Version` 9, `competitions.ScheduleVersion` 3, `ai.SelectionVersion` 3, at commit `5f07b58`. Answers `balance--tick-career-seasons`; the match statistics in it answer the career half of `balance--tick-match-stats`. Nothing implausible appeared beyond the two notes already open against `match` (`match--simple-home-advantage`, and `match--tick-stats-calibration` for the statistics).

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1   # about 30 s on 32 cores
```

`TestBalanceCareerEngines` in `internal/app/balance_test.go` plays the same three seeded AI-only careers (seeds 7, 42 and 2026, three seasons each) on both engines: one world per (seed, engine), so the market, promotions and cups carry over, and each engine plays every fixture of the career. The engines face the same fixtures (asserted). "Upsets" are league matches won by the club with the lower squad average before the season; the gap rows bucket that average gap. Each row is 9 seasons × 224 league matches = 2,016 matches.

### League matches

| | simple | tick | Synthetic 60 v 60, simple / tick | Top leagues, roughly |
| --- | --- | --- | --- | --- |
| Goals per match | 2.18 | 2.23 | 2.71 / 2.46 | 2.6–2.9 |
| Home–away goals | 1.09–1.08 | 1.26–0.97 | 1.37–1.35 / 1.34–1.11 | 1.5–1.2 |
| Home / draw / away % | 35.4 / 30.3 / 34.3 | 42.8 / 27.8 / 29.4 | 37.2 / 26.3 / 36.6 / 42.4 / 26.8 / 30.8 | 45 / 26 / 29 |
| Upsets (weaker club wins) % | 27.2 | 28.5 | | |
| Stronger side win %, gap 0–1 / 2–4 / 5–8 / 9+ | 37 / 36 / 37 / — | 41 / 36 / 40 / 25 | | |
| matches in those buckets | 628 / 916 / 144 / 0 | 608 / 932 / 134 / 12 | | |

**Career matches score less than synthetic ones.** 2.18–2.23 goals against 2.71 and 2.46 at 60 v 60. Real squads, with attributes spread across roles and a best XI that is stronger than the squad average, produce fewer goals than the synthetic balanced profiles; the draw rate stays right (27.8% in `tick`). `tick` keeps its home edge on real squads (42.8% against 29.4%, home goals 1.30 times away) and `simple` loses any venue effect again (35.4% against 34.3%, 1.01): that is `match--simple-home-advantage` measured on career squads.

**Results separate less by squad average than the synthetic gap table suggests.** At gaps up to 8 points the stronger side by pre-season average wins 36–41% of all matches (51–57% of the decided ones), where the synthetic rows give it 44% at gap 4 (62 v 58 and 58 v 62 pooled) and 55% at gap 10 (65 v 55 and 55 v 65). The best XI compresses the gap — both clubs field their strongest eleven — so the engine sees a smaller difference than the 20-man averages do. The 9+ bucket has 12 matches (the stronger side won 3) on `tick` and none on `simple`, too few to read. Most of the 27–28% "upsets" are near-equal clubs beating each other. Not a note: the mechanism is selection, not the engine.

**Final tables and shootouts agree across engines.**

| | simple | tick |
| --- | --- | --- |
| Champion points (of 42) | 26.2 | 26.4 |
| Last points | 11.8 | 11.6 |
| Spread | 14.4 | 14.8 |
| Continental Cup matches a season | 7.0 | 7.0 |
| Cup matches level after 90 minutes, % | 30 | 27 |
| Promotion play-off ties a season | 4.0 | 4.0 |
| Ties to penalties, % | 25 | 19 |

Means over 36 league tables (4 leagues × 9 seasons). The champion takes 1.87 points a match and the last 0.84 — the 8-club leagues are tighter at the top than a real 20-club table (about 2.2 for the champion) and as tight at the bottom. A quarter to a third of cup ties go to penalties, same as the synthetic knockout rate and high because there is no extra time (rules choice, see [Shootouts](#shootouts)). Workload (condition and injuries) is engine-independent and lives under [Injuries](#injuries).

### History

- **2026-10-01**, `tick` v6, `simple` v4: this section.

## Match statistics: tick against real football

Measured 2026-10-01 at `tick.ModelVersion` 6, commit `5f07b58`. Answers `balance--tick-match-stats`. The real-football figures are `match`'s calibration targets from that note. `simple` reports no statistics (no `DetailedStats` capability). Filed to `match` as `match--tick-stats-calibration`.

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceMatchStats -v -count=1 -timeout 2h   # about 45 s on 32 cores
```

`TestBalanceMatchStats` in `internal/matches/tick/balance_test.go`, 3,000 synthetic matches a row (seeds 1, 42, 2026 × 1,000 fixtures, `enginetest.Input` teams). The career rows are `TestBalanceCareerEngines`'s 2,016 league matches on `tick` (above). Both sides of a match are listed (home / away where it matters).

### Against real football (per side)

| Stat | Real top leagues | tick 60 v 60 | tick careers | tick 70 v 50 (stronger / weaker) |
| --- | --- | --- | --- | --- |
| Shots | 12–13 | 10.8 / 9.5 | 11.7 / 10.2 | 16.4 / 6.3 |
| On target | 4–5 | 4.4 / 3.7 | 4.2 / 3.5 | 7.9 / 2.0 |
| Saves | 2–3 | 2.6 / 3.1 | 2.5 / 3.0 | 1.5 / 5.3 |
| Passes | 400–600 | 954 / 924 | 960 / 925 | 1045 / 823 |
| Completion % | 75–85 | 79 / 79 | 80 / 79 | 81 / 77 |
| Tackles | 15–20 | 45.7 / 43.4 | 41.3 / 37.8 | 52.2 / 34.7 |
| Offsides | ~2 | 1.8 / 1.7 | 1.8 / 1.8 | 2.0 / 1.3 |
| Possession, stronger side % | 60–65 at a clear gap | 50.9 (even) | 51.0 (even) | 56.6 at gap 20 |

**Passes run 1.7–2.5 times the target** (823–1,045 a side against 400–600) and **tackles 2–3 times** (35–52 against 15–20). Completion is right. **Possession does not separate enough**: 56.6% for a 20-point stronger side and 53.8% at gap 10, against the 60–65% target; equal teams are fine at 51/49. **Shots are a little low and too lopsided**: 20–21 a match between equal teams against the real 24–26, and at gap 20 the split is 16.4–6.3 where real football at gap 10 is about 12.6–9.3. Offsides are right, and the mentality rows reproduce `match`'s own v6 figures (about 4.4 a side when both attack, 0.1 when both defend). Shots, on target, saves and goals are internally consistent (on target = goals + the opponent's saves). The career rows match the synthetic profile almost exactly — real squads change goals and results, not these statistics.

### Synthetic teams, all rows (3,000 matches each)

| Scenario | Side | Shots | On target | Saves | Passes | Completion % | Tackles | Offsides | Possession % |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 60 v 60 | home | 10.8 | 4.4 | 2.6 | 954 | 79 | 45.7 | 1.8 | 50.9 |
| 60 v 60 | away | 9.5 | 3.7 | 3.1 | 924 | 79 | 43.4 | 1.7 | 49.1 |
| 65 v 55 | home | 13.4 | 5.9 | 2.0 | 1001 | 80 | 49.0 | 1.9 | 53.8 |
| 65 v 55 | away | 7.7 | 2.8 | 4.1 | 873 | 78 | 39.2 | 1.5 | 46.2 |
| 55 v 65 | home | 8.8 | 3.3 | 3.5 | 905 | 78 | 41.9 | 1.6 | 48.0 |
| 55 v 65 | away | 11.7 | 5.0 | 2.3 | 973 | 80 | 46.9 | 1.8 | 52.0 |
| 70 v 50 | home | 16.4 | 7.9 | 1.5 | 1045 | 81 | 52.2 | 2.0 | 56.6 |
| 70 v 50 | away | 6.3 | 2.0 | 5.3 | 823 | 77 | 34.7 | 1.3 | 43.4 |
| 50 v 70 | home | 7.2 | 2.5 | 4.6 | 854 | 77 | 37.6 | 1.5 | 45.2 |
| 50 v 70 | away | 14.5 | 6.6 | 1.8 | 1018 | 80 | 50.5 | 2.0 | 54.8 |
| 80 v 80 | home | 10.7 | 4.4 | 2.6 | 952 | 79 | 46.9 | 1.7 | 50.8 |
| 80 v 80 | away | 9.6 | 3.8 | 3.1 | 925 | 79 | 45.4 | 1.7 | 49.2 |
| both attacking | home | 13.4 | 5.7 | 3.4 | 960 | 79 | 44.4 | 4.4 | 50.8 |
| both attacking | away | 11.9 | 4.8 | 4.0 | 934 | 79 | 42.1 | 4.2 | 49.2 |
| both defensive | home | 6.6 | 2.5 | 1.4 | 939 | 81 | 42.0 | 0.1 | 51.4 |
| both defensive | away | 5.6 | 2.0 | 1.8 | 895 | 80 | 40.1 | 0.1 | 48.6 |
| home attacking | home | 13.4 | 5.5 | 2.7 | 959 | 79 | 46.6 | 2.7 | 49.9 |
| home attacking | away | 9.5 | 3.8 | 3.8 | 929 | 79 | 41.6 | 2.8 | 50.1 |

An attacking side shoots more and is caught offside more (2.7–4.4 a side) but gains nothing in possession (49.9% for the attacking home side against a balanced one). A defensive block concedes few shots and is almost never caught offside (0.1).

### History

- **2026-10-01**, `tick` v6: this section.

## Injuries

Measured 2026-10-01 at `medical.Version` 3, over the 18 career seasons of [Career seasons](#career-seasons-tick-against-simple) (3 seeds × 3 seasons × 2 engines = 576 club-seasons). Answers the rates half of `balance--injuries-delivered`; the rotation-policy half is still to do. `medical` reads minutes and condition, not the engine, so both engines agree within noise (75.9–76.2 injuries a season); the rows below pool all 18 seasons. Commands as for `TestBalanceCareerEngines`.

| | per season | per club a season | per injury | per injured player a season | per injured club-season |
| --- | --- | --- | --- | --- | --- |
| Injuries | 76 | 2.4 | | 2.6 injuries | 2.6 injuries |
| Days out | 1,047 | 32.7 | 13.7 days | 15.1 days (max 119) | 35.6 days |

**About 76 injuries a season across 32 clubs** (0.12 a player a year on 20-man squads), close to `squad`'s own estimate of 70 on seed 42. A layoff averages 13.7 days; the 29–120 day tail produces single seasons of up to 119 days. Against real football (roughly one reportable injury a player a year) the rate is low: at 2.4 a club a season injuries are flavor, not a management concern. Filed to `squad` as `squad--injury-rates`.

**A club is never short of fit players.** In about 11,000 club-batches (every club before every league round, play-off tie and cup match) not one club could fail to field a legal lineup from its fit players alone, so the emergency rule (injured players play) never fired: 0 emergency starts. Squads of 20 absorb the current rates easily.

**Condition before every round is 99.8 of 100.** The daily recovery restores a match's drain long before the next kickoff — league rounds are weekly and the cup's denser rounds too. The fatigue term in `medical.Roll` (missing condition × `InjuryFatigueStep`) therefore sees almost no missing condition when injuries are rolled, and rotating tired players cannot pay through condition; if rotation is to matter, the drain or the recovery has to give it a reason first.

Still to measure: whether rotating tired players pays in points against always playing the best XI (the manager-policy comparison in the balance backlog), and injury rates again if `squad` moves `DefaultParams`.

### History

- **2026-10-01**, `medical.Version` 3: this section.
