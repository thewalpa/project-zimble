# Balance

Measurements of the seeded game over long careers: the current numbers, the seeds and versions behind them, and what changed. The `balance` lane keeps this file ([lane doc](lanes/balance.md)). Every number here comes from a test that skips unless `ZIMBLE_BALANCE=1` is set, and the command that reproduces it is given with it.

## AI transfer market

Measured 2026-10-01 on 32 clubs (four leagues of 8) at `ai.TransfersVersion` 6, `ai.ContractsVersion` 2, `ai.SelectionVersion` 3, `players.DevelopmentVersion` 1, `worldgen.Version` 8, `worldgen.YouthVersion` 4, `content.Version` 9, `content.LeagueVersion` 5, `competitions.ScheduleVersion` 3, `medical.Version` 3, at commit `15ff2fa` with `simple.ModelVersion` 4, the career default (rerun at v5: [below](#rerun-at-simple-v5-commit-64874ae)). Knobs: `ai.UpgradeMargin` 8, `ai.ListingPermille` 800, `ai.TransferMargin` 5, `ai.ReserveWeeks` 26, `ai.StarMargin` 10, `ai.KeyPermillePerPoint` 60 (6%), `freeAgentReserve` 4. Answers `balance--rerun-baseline-32-clubs`, `balance--star-churn-delivered`, `balance--free-agent-pool-delivered` and `balance--consent-at-completion-delivered`. The previous baseline (16 clubs, `ai.TransfersVersion` 2) is in the [history](#history).

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

### Rerun at `simple` v5 (commit `64874ae`)

`simple` v5 replays every match of these careers (its home edge moves results, so promotions, prize money and who buys move too). No AI, content, worldgen or medical version changed, and `squad`'s club-observation change (`ce3e82a`) keeps seeded outcomes. The tables above are the `15ff2fa` baseline; the rerun's headline numbers, same seeds and commands:

| | baseline (`simple` v4) | rerun (`simple` v5) |
| --- | --- | --- |
| AI-only: completed transfers a window, 30-year mean | 48 | 48 |
| AI-only: refusals / AI listings / of the 16 best moved, per window | 5 / 20 / 3 | 5 / 20 / 3 |
| AI-only: squad average strongest / weakest / mean, year 30 | 64 / 52 / 59 | 65 / 53 / 59 |
| AI-only: lowest / median / highest balance, year 30 | 1.02M / 6.50M / 24.38M | 0.97M / 7.06M / 22.49M |
| AI-only: windows closing with a club short | 13 of 300 (16 players) | 11 of 300 (14) |
| Passive: completed a window / listed players unsold | 36 / 55–59% | 37 / 51–57% |
| Passive: free-agent pool at the open / close, year 30 | 19 / 9 | 17 / 6 |
| Passive manager: squad average year 5, balance year 30, titles in 30 seasons | 51, 28.5M, 1–4 | 51, 28.0M, 0–1 |
| Recruiting manager: squad average year 30, balance year 30 | 62, 13.2M | 61, 22.9M |
| Recruiting manager: career squad average per seed, titles in 30 seasons | 60–62, 3–7 | 58–60, 4–11 |
| Recruiting: windows closing with an AI club short | 24 of 120 (31 players) | 27 of 120 (39) |

**Nothing in the AI market moved beyond seed noise**, and no club goes below zero. The managed club moved more: the recruiting manager ends 9.7M richer at year 30 and wins more titles (seed 99: 11 in 30 seasons), and the passive one wins fewer (0–1). Both are 4-seed means of a single club, so a few titles or a sale either way move them; I haven't isolated whether home wins, prize money or sales account for the balance. The conclusions above stand.

### History

- **2026-09-28**, 16 clubs, `ai.TransfersVersion` 2: 27 transfers a window, every bid completed, the 16 best players 12 a window, strength gap 6–8, median balance 8.5M at year 30, free-agent pool empty (AI-only) or 2–4 (passive). Filed `squad--star-churn` and `squad--free-agent-pool`.
- **2026-10-01**, 32 clubs, `ai.TransfersVersion` 6: this section, at `simple` v4 (commit `15ff2fa`), and rerun at `simple` v5 (commit `64874ae`).

## Match engines: tick against simple

Measured 2026-10-01 at `tick.ModelVersion` 7 and `simple.ModelVersion` 5, both with their `DefaultParams`, at commit `64874ae`. Reruns the comparison after `match--attacking-is-free` (tick v7) and `match--simple-home-advantage` (simple v5) were delivered; the numbers agree exactly with `match`'s own tables in [progress.md](progress.md) (same seeds and streams). Earlier measurements are in the [history](#history-1).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceEngineComparison' -v -count=1 -timeout 2h   # about 3 min on 32 cores, 13 on 4
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceMentalityByGap' -v -count=1 -timeout 2h     # about 2.5 min on 32 cores, 8 on 4
```

(`go test`'s default 10-minute timeout is too short on a small machine: pass `-timeout`.)

`TestBalanceEngineComparison` in `internal/matches/tick/balance_test.go` plays both engines on the same `enginetest.Input` teams: 11 starters and 7 substitutes, balanced unless stated, full condition, no commands. A team's "strength" is the rating its profile is built around, and it is close to its overall. Each row is 3,000 matches: seeds 1, 42 and 2026 × fixtures 1–1000, with each engine's own `matches.FixtureRandom` stream. Every match is played as a knockout. The contract guarantees that the knockout rule leaves the 90 minutes unchanged, so one run gives both the regulation result and the shootout. With 3,000 matches, a rate near 25% is good to about ±1.6 points (95%), one near 50% to about ±1.8, a goal average to about ±0.04, and points per match to about ±0.05.

These are synthetic teams, not career squads; the career-season comparison is in ["Career seasons: tick against simple"](#career-seasons-tick-against-simple).

### Equal teams, 60 v 60

| | simple | tick | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match | 2.71 | 2.40 | 2.6–2.9 |
| Home–away goals | 1.54–1.17 | 1.31–1.09 | 1.5–1.2 |
| Home / draw / away % | 44.9 / 26.1 / 29.1 | 41.0 / 29.1 / 29.9 | 45 / 26 / 29 |
| Draw % if the two scores were independent | 25.5 | 27.9 | |
| 0–0 % | 6.9 | 9.4 | 7–8 |
| 4 or more goals % | 28.1 | 22.4 | 25–30 |

| Total goals (% of matches) | 0 | 1 | 2 | 3 | 4 | 5 | 6+ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| simple | 6.9 | 17.0 | 25.1 | 22.8 | 14.3 | 8.0 | 5.8 |
| tick | 9.4 | 21.5 | 26.2 | 20.5 | 12.6 | 6.5 | 3.3 |

**Resolved: `simple` has a home edge again.** 44.9% home wins against 29.1% away and 1.54–1.17 goals, on the real-football column; v4 was 37.2 against 36.6. `match--simple-home-advantage` is delivered. **`tick` v7 drifted a little further from the real column:** 2.40 goals (v6 2.46), 29.1% draws (26.8), 9.4% 0–0s (7.6) and a home edge of 11 points (12). Each move is one to two sampling margins; together they say the mentality change took a little out of open play between equal balanced sides. Not a note on its own: goals at 60 v 60 are part of the open `match--tick-stats-calibration` (shots too few between equal teams), and the career level is filed as `match--career-goals` (see [Career seasons](#career-seasons-tick-against-simple)).

### Rating gap (home strength v away strength)

| Match | simple H / D / A % | simple goals | tick H / D / A % | tick goals |
| --- | --- | --- | --- | --- |
| 70 v 50 | 70.8 / 17.8 / 11.4 | 2.97 | 80.2 / 13.8 / 5.9 | 3.16 |
| 65 v 55 | 58.2 / 23.3 / 18.4 | 2.81 | 62.7 / 22.0 / 15.3 | 2.69 |
| 62 v 58 | 50.4 / 24.6 / 25.0 | 2.73 | 52.3 / 24.2 / 23.5 | 2.58 |
| 60 v 60 | 44.9 / 26.1 / 29.1 | 2.71 | 41.0 / 29.1 / 29.9 | 2.40 |
| 58 v 62 | 40.5 / 25.4 / 34.1 | 2.70 | 32.5 / 28.1 / 39.3 | 2.47 |
| 55 v 65 | 33.4 / 25.1 / 41.6 | 2.69 | 24.8 / 24.3 / 50.9 | 2.54 |
| 50 v 70 | 22.7 / 23.8 / 53.5 | 2.71 | 11.7 / 18.4 / 69.8 | 2.82 |

In a career, squad averages range from 51 to 65 (see the market section), so the gaps that matter are the 62 v 58 and 65 v 55 rows. `tick` turns the gap into wins without runaway scores (a 20-point mismatch averages 3.2 goals) and is steeper than `simple`: a home side 10 points stronger wins 63% (`simple` 58%), and a 10-point weaker home side 25% (`simple` 33%). `simple`'s home edge now shows at every gap: its 55 v 65 home side wins a third of the time.

### Quality level

| Match | simple goals | simple draw % | tick goals | tick draw % | tick 0–0 % | tick 4+ goals % |
| --- | --- | --- | --- | --- | --- | --- |
| 40 v 40 | 2.78 | 25.3 | 2.55 | 27.1 | 8.6 | 25.5 |
| 60 v 60 | 2.71 | 26.1 | 2.40 | 29.1 | 9.4 | 22.4 |
| 80 v 80 | 2.69 | 25.6 | 2.52 | 26.4 | 7.4 | 25.1 |

Goals stay flat across levels in both engines (`tick` 2.40–2.55, a 0.15 spread, against 2.1–4.6 at v1). `tick`'s 60 v 60 row is the low one of the three; 40 and 80 agree with each other.

### Mentality

Points per match are 3 × wins + draws, from the named side's view; Δ is against the same side playing balanced in the same match.

| Home v away (60 v 60) | simple goals | simple H / D / A % | tick goals | tick H / D / A % |
| --- | --- | --- | --- | --- |
| balanced v balanced | 2.71 | 44.9 / 26.1 / 29.1 | 2.40 | 41.0 / 29.1 / 29.9 |
| attacking v attacking | 3.75 | 49.5 / 20.6 / 30.0 | 3.26 | 44.2 / 23.5 / 32.3 |
| defensive v defensive | 1.82 | 40.8 / 32.3 / 26.9 | 1.38 | 36.3 / 37.8 / 25.9 |
| attacking v balanced | 3.20 | 48.0 / 23.7 / 28.3 | 2.87 | 42.3 / 25.3 / 32.4 |
| balanced v attacking | 3.18 | 44.8 / 24.8 / 30.4 | 2.90 | 42.7 / 25.0 / 32.3 |
| defensive v balanced | 2.21 | 41.2 / 28.3 / 30.5 | 1.86 | 40.7 / 31.9 / 27.4 |
| balanced v defensive | 2.22 | 45.0 / 27.3 / 27.7 | 1.88 | 40.1 / 30.8 / 29.1 |
| attacking v defensive | 2.65 | 48.0 / 25.4 / 26.6 | 2.26 | 39.9 / 29.4 / 30.7 |
| defensive v attacking | 2.60 | 40.6 / 27.3 / 32.1 | 2.25 | 44.2 / 28.0 / 27.8 |

By the rating gap and venue (`TestBalanceMentalityByGap` for the gap rows, `TestBalanceEngineComparison` for 60 v 60), points per match for the named side:

| Side | engine | balanced | defensive (Δ) | attacking (Δ) | goals for / against: balanced → defensive → attacking |
| --- | --- | --- | --- | --- | --- |
| Equal, 60 v 60 at home | tick | 1.52 | 1.54 (+0.02) | 1.52 (+0.00) | 1.31 / 1.09 → 1.05 / 0.80 → 1.57 / 1.30 |
| | simple | 1.61 | 1.52 (−0.09) | 1.68 (+0.07) | 1.54 / 1.17 → 1.22 / 0.98 → 1.84 / 1.36 |
| Equal, 60 v 60 away | tick | 1.19 | 1.18 (−0.01) | 1.22 (+0.03) | 1.09 / 1.31 → 0.85 / 1.03 → 1.32 / 1.58 |
| | simple | 1.13 | 1.10 (−0.03) | 1.16 (+0.03) | 1.17 / 1.54 → 0.93 / 1.30 → 1.42 / 1.77 |
| Underdog, 55 v 65 at home | tick | 0.99 | 1.05 (+0.07) | 0.99 (+0.00) | 1.00 / 1.54 → 0.76 / 1.14 → 1.18 / 1.81 |
| | simple | 1.25 | 1.21 (−0.05) | 1.31 (+0.05) | 1.27 / 1.41 → 1.02 / 1.20 → 1.53 / 1.63 |
| Favourite, 65 v 55 at home | tick | 2.10 | 2.10 (−0.01) | 2.14 (+0.04) | 1.88 / 0.81 → 1.50 / 0.56 → 2.15 / 0.96 |
| | simple | 1.98 | 1.85 (−0.13) | 2.06 (+0.08) | 1.84 / 0.96 → 1.47 / 0.81 → 2.21 / 1.12 |
| Underdog, 65 v 55 away | tick | 0.68 | 0.72 (+0.04) | 0.64 (−0.04) | 0.81 / 1.88 → 0.62 / 1.46 → 0.94 / 2.19 |
| | simple | 0.79 | 0.79 (+0.00) | 0.81 (+0.02) | 0.96 / 1.84 → 0.76 / 1.56 → 1.17 / 2.12 |

**Resolved in `tick`: mentality is a trade-off.** Attacking against a balanced side is worth −0.04 to +0.04 points a match (v6: +0.11 to +0.24), because it now concedes as much as it adds (+0.15 to +0.31 goals against, +0.13 to +0.27 for). Defensive pays a little for the underdog (+0.07 at home, +0.04 away), costs nothing for the favourite and draws more (31.3% against 24.3% at 55 v 65). Every Δ is within ±0.07, a choice of style rather than a dominant setting: over a 14-match league season the best mentality is worth about a point. `match--attacking-is-free` is delivered.

**`simple`, the career default, still has the v6 shape at a smaller scale.** Attacking beats balanced in every row (+0.02 to +0.08), because it adds 0.21–0.37 goals scored against 0.16–0.28 conceded (more scored than conceded in four of the five rows), and defensive never beats balanced, even for the underdog (−0.13 to +0.00). The single rows are one to two sampling margins, but all ten point the same way. A career manager on the default engine should always attack and never defend; filed as `match--simple-mentality`. Neither engine charges for attacking outside the goal model (`medical` reads minutes only), the AI always plays balanced (`ai.SelectionVersion` 3), and switching late to protect a lead is not measured.

### Shootouts

A knockout tie goes to penalties exactly when it is level after 90 minutes: 26–29% of ties between equal teams in either engine (29.1% in `tick`). The sweep checks that the shootout count equals the draw count in every row. There is no extra time, which roughly doubles or triples the real-world shootout rate; a rules choice for `competitions` and `match`.

| Home (stronger) side wins the shootout, % | 60 v 60 | 62 v 58 | 65 v 55 | 70 v 50 |
| --- | --- | --- | --- | --- |
| simple | 51 | 52 | 56 | 61 |
| tick | 48 | 52 | 56 | 62 |

At 50 v 70 the home side is the weaker one and wins 39% of the shootouts in both engines. Both sides together score 8.0–8.7 penalties per shootout. Shootouts stay close to a coin toss with a modest edge for the better side, as at v6 (`match--shootout-favourite` holds). `simple` guards this with `TestShootoutsStayClose`; `tick` has no shootout bound, proposed in `match--mentality-shootout-bounds`.

### History

- **2026-09-28**, `tick` v1, `simple` v3: `tick` drew 24.9% and scored 2.9 goals at 60 v 60, 4.6 at 80 v 80 and 2.1 at 40 v 40, a 10-point mismatch gave 72% wins and a 20-point one 4.3 goals. Attacking raised the win rate by 20 points with no cost at the back; a defensive side against a balanced one conceded 4.7 goals. A stronger side won 74–80% of shootouts at 65 v 55. Filed `match--tick-goals-by-level`, `match--tick-mentality` and `match--shootout-favourite`.
- **2026-10-01**, `tick` v6, `simple` v4 (commit `78fc6b2`): `tick` 2.46 goals and 42.4 / 26.8 / 30.8 at 60 v 60; `simple` 2.71 goals and no home edge (37.2 / 26.3 / 36.6). Attacking against balanced was worth +0.11 to +0.24 points a match on `tick` and defensive −0.04 to −0.11; shootouts 53–60% for the stronger side. Filed `match--attacking-is-free` and `match--simple-home-advantage`.
- **2026-10-01**, `tick` v7, `simple` v5: this section.

## Career seasons: tick against simple

Measured 2026-10-01 at `tick.ModelVersion` 7 and `simple.ModelVersion` 5 (both `DefaultParams`), `medical.Version` 3, `worldgen.Version` 8, `content.Version` 9, `competitions.ScheduleVersion` 3, `ai.SelectionVersion` 3, at commit `64874ae`. The previous run (`tick` v6, `simple` v4) is in the [history](#history-2).

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1   # about 25 s on 32 cores
```

`TestBalanceCareerEngines` in `internal/app/balance_test.go` plays the same three seeded AI-only careers (seeds 7, 42 and 2026, three seasons each) on both engines: one world per (seed, engine), so the market, promotions and cups carry over, and each engine plays every fixture of the career. The engines face the same fixtures (asserted). Since `Continue` resolves batches without a user fixture by itself (competitions, `f3652ff`), the sweep stops the clock a minute before each kickoff to sample the squads and then continues to it; it fails if a season measures no matches. "Upsets" are league matches won by the club with the lower squad average before the season; the gap rows bucket that average gap. Each row is 9 seasons × 224 league matches = 2,016 matches.

### League matches

| | simple | tick | Synthetic 60 v 60, simple / tick | Top leagues, roughly |
| --- | --- | --- | --- | --- |
| Goals per match | 2.16 | 2.30 | 2.71 / 2.40 | 2.6–2.9 |
| Home–away goals | 1.25–0.92 | 1.29–1.00 | 1.54–1.17 / 1.31–1.09 | 1.5–1.2 |
| Home / draw / away % | 43.7 / 28.6 / 27.7 | 43.0 / 27.2 / 29.8 | 44.9 / 26.1 / 29.1 · 41.0 / 29.1 / 29.9 | 45 / 26 / 29 |
| Upsets (weaker club wins) % | 29.1 | 28.7 | | |
| Stronger side win %, gap 0–1 / 2–4 / 5–8 / 9+ | 41 / 34 / 33 / — | 39 / 37 / 40 / 50 | | |
| matches in those buckets | 642 / 884 / 146 / 2 | 650 / 890 / 126 / 10 | | |

**Both engines now have a home edge on career squads**, and about the same one: 43–44% home wins against 28–30% away. `simple` v4 had none (35.4 against 34.3).

**Career matches score 0.4–0.6 goals less than real football in both engines.** 2.16 (`simple`) and 2.30 (`tick`) goals a match, where both engines' own synthetic 60 v 60 rows give 2.71 and 2.40 and the real range is 2.6–2.9; `simple` loses 0.55 goals between its synthetic and career rows. This is what the player sees in every league table. The mechanism is the squads (attributes spread across roles, best XIs) rather than one engine, but both engines calibrate on the synthetic profiles, so the career level drifts unwatched. Filed as `match--career-goals`.

**Results separate less by squad average than the synthetic gap table suggests**, as at v6: at gaps up to 8 points the stronger side by pre-season average wins 33–41% of all matches, where the synthetic rows give it 42–46% at gap 4 and 50–57% at gap 10 (home and away pooled). Both clubs field their strongest eleven, so the engine sees a smaller gap than the 20-man averages. The 9+ bucket has 2 and 10 matches, too few to read. Not a note: the mechanism is selection, not the engine.

**Final tables and shootouts agree across engines.**

| | simple | tick |
| --- | --- | --- |
| Champion points (of 42) | 26.1 | 26.9 |
| Last points | 12.2 | 10.9 |
| Spread | 13.9 | 15.9 |
| Continental Cup matches a season | 7.0 | 7.0 |
| Cup matches level after 90 minutes, % | 33 | 22 |
| Promotion play-off ties a season | 4.0 | 4.0 |
| Ties to penalties, % | 19 | 28 |

Means over 36 league tables (4 leagues × 9 seasons). The champion takes 1.86–1.92 points a match and the last 0.78–0.87; `tick` spreads the table 2 points wider, as its steeper gap table predicts. The cup and play-off rates rest on 63 cup matches and 36 ties per engine (±11 points), so the swap between engines since v6 (30/27 and 25/19) is noise. Workload (condition and injuries) is engine-independent and lives under [Injuries](#injuries).

### History

- **2026-10-01**, `tick` v6, `simple` v4 (commit `5f07b58`): 2.18 (`simple`) and 2.23 (`tick`) goals; `simple` without a home edge (35.4 / 30.3 / 34.3), `tick` 42.8 / 27.8 / 29.4; champion 26.2–26.4 points, last 11.6–11.8.
- **2026-10-01**, `tick` v7, `simple` v5: this section.

## Match statistics: tick against real football

Measured 2026-10-01 at `tick.ModelVersion` 7, commit `64874ae` (first measured at v6, see the [history](#history-3)). Answers `balance--tick-match-stats`. The real-football figures are `match`'s calibration targets from that note. `simple` reports no statistics (no `DetailedStats` capability). Filed to `match` as `match--tick-stats-calibration`.

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceMatchStats -v -count=1 -timeout 2h   # about 2 min on 32 cores
```

`TestBalanceMatchStats` in `internal/matches/tick/balance_test.go`, 3,000 synthetic matches a row (seeds 1, 42, 2026 × 1,000 fixtures, `enginetest.Input` teams). The career rows are `TestBalanceCareerEngines`'s 2,016 league matches on `tick` (above). Both sides of a match are listed (home / away where it matters).

### Against real football (per side)

| Stat | Real top leagues | tick 60 v 60 | tick careers | tick 70 v 50 (stronger / weaker) |
| --- | --- | --- | --- | --- |
| Shots | 12–13 | 10.8 / 9.5 | 11.7 / 10.1 | 16.3 / 6.2 |
| On target | 4–5 | 4.4 / 3.6 | 4.2 / 3.5 | 7.9 / 2.1 |
| Saves | 2–3 | 2.5 / 3.1 | 2.5 / 2.9 | 1.5 / 5.4 |
| Passes | 400–600 | 954 / 924 | 960 / 924 | 1047 / 821 |
| Completion % | 75–85 | 79 / 79 | 80 / 79 | 81 / 77 |
| Tackles | 15–20 | 45.8 / 43.4 | 41.3 / 37.8 | 52.2 / 34.6 |
| Offsides | ~2 | 1.8 / 1.7 | 1.9 / 1.8 | 2.0 / 1.3 |
| Possession, stronger side % | 60–65 at a clear gap | 50.9 (even) | 51.0 (even) | 56.7 at gap 20 |

**v7 moved none of this**: outside the mentality rows every figure is within 0.1 of v6, so `match--tick-stats-calibration` stands as filed. **Passes run 1.7–2.5 times the target** (821–1,047 a side against 400–600) and **tackles 2–3 times** (35–52 against 15–20). Completion is right. **Possession does not separate enough**: 56.7% for a 20-point stronger side and 53.8% at gap 10, against the 60–65% target; equal teams are fine at 51/49. **Shots are a little low and too lopsided**: 20–21 a match between equal teams against the real 24–26, and at gap 20 the split is 16.3–6.2 where real football at gap 10 is about 12.6–9.3. Offsides are right (about 4.2 a side when both attack, 0.1 when both defend). Shots, on target, saves and goals are internally consistent (on target = goals + the opponent's saves). The career rows match the synthetic profile almost exactly — real squads change goals and results, not these statistics.

### Synthetic teams, all rows (3,000 matches each)

| Scenario | Side | Shots | On target | Saves | Passes | Completion % | Tackles | Offsides | Possession % |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 60 v 60 | home | 10.8 | 4.4 | 2.5 | 954 | 79 | 45.8 | 1.8 | 50.9 |
| 60 v 60 | away | 9.5 | 3.6 | 3.1 | 924 | 79 | 43.4 | 1.7 | 49.1 |
| 65 v 55 | home | 13.4 | 6.0 | 2.0 | 1002 | 80 | 49.1 | 1.9 | 53.8 |
| 65 v 55 | away | 7.8 | 2.8 | 4.2 | 873 | 78 | 39.3 | 1.5 | 46.2 |
| 55 v 65 | home | 8.8 | 3.3 | 3.5 | 904 | 78 | 41.9 | 1.6 | 48.0 |
| 55 v 65 | away | 11.8 | 5.0 | 2.3 | 973 | 80 | 47.0 | 1.8 | 52.0 |
| 70 v 50 | home | 16.3 | 7.9 | 1.5 | 1047 | 81 | 52.2 | 2.0 | 56.7 |
| 70 v 50 | away | 6.2 | 2.1 | 5.4 | 821 | 77 | 34.6 | 1.3 | 43.3 |
| 50 v 70 | home | 7.2 | 2.5 | 4.6 | 855 | 77 | 37.4 | 1.5 | 45.2 |
| 50 v 70 | away | 14.5 | 6.6 | 1.8 | 1017 | 80 | 50.3 | 2.0 | 54.8 |
| 80 v 80 | home | 10.6 | 4.4 | 2.6 | 950 | 79 | 47.3 | 1.8 | 50.7 |
| 80 v 80 | away | 9.5 | 3.7 | 3.0 | 927 | 79 | 45.6 | 1.7 | 49.3 |
| both attacking | home | 15.1 | 6.1 | 3.7 | 972 | 79 | 42.3 | 4.3 | 50.7 |
| both attacking | away | 13.3 | 5.1 | 4.3 | 948 | 79 | 40.1 | 4.1 | 49.3 |
| both defensive | home | 6.3 | 2.5 | 1.4 | 933 | 81 | 43.2 | 0.1 | 51.5 |
| both defensive | away | 5.3 | 2.0 | 1.8 | 885 | 80 | 41.4 | 0.1 | 48.5 |
| home attacking | home | 12.7 | 5.1 | 3.2 | 956 | 79 | 43.9 | 2.3 | 51.1 |
| home attacking | away | 11.3 | 4.4 | 3.6 | 941 | 79 | 42.5 | 3.2 | 48.9 |

An attacking side shoots more and is caught offside more (2.3–4.3 a side); at v7 the balanced side facing it shoots more too (11.3 against 9.5) and is caught offside more (3.2, against 1.7), running into the space behind the high line. Possession still barely moves (51.1% for the attacking home side, 49.9% at v6). A defensive block concedes few shots and is almost never caught offside (0.1).

### History

- **2026-10-01**, `tick` v6 (commit `5f07b58`): the same figures within 0.1, except the mentality rows: both attacking 13.4–11.9 shots, an attacking home side 13.4–9.5 shots and 49.9% possession. Filed `match--tick-stats-calibration`.
- **2026-10-01**, `tick` v7: this section.

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

- **2026-10-01**, `medical.Version` 3: this section, at `tick` v6 and `simple` v4. Rerun at `tick` v7 and `simple` v5 (commit `64874ae`): 76.1 and 75.9 injuries a season, 13.6–13.7 days a layoff, 15.0–15.1 days per injured player (max 119), 0 short-of-fit club-batches and condition 99.8 before every round. The section stands unchanged.
