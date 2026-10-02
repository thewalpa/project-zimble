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

### Rerun at `ai.TransfersVersion` 7 (generation 9, `LeagueVersion` 7)

Measured 2026-10-02 on branch `ccr-4e8a8cc8-digbuz` (commit after `f1fddce`) at `ai.TransfersVersion` 7, `ai.ContractsVersion` 2, `worldgen.Version` 9, `content.LeagueVersion` 7, `competitions.ScheduleVersion` 4, `medical.Version` 4, `simple.ModelVersion` 6, storage schema 32 (cup awards posted); same seeds and commands as above. It answers `balance--recruitment-role-fallback` and the market half of `balance--passive-manager-investigation`. `TestBalanceAIMarket*` now also prints each short window (seed, year, club, position, free agents at the close).

| | v6 baseline (`squad`'s investigation, same seeds) | v7 |
| --- | --- | --- |
| AI-only: windows closing with an AI club short | 14 of 300 (14 players) | **4 of 300 (4 players)** |
| Passive: windows short / listed players unsold | 3 of 120 (3) / 52–59% | **0 of 120** / 56–58% |
| Recruiting: windows short | 27 of 120 (40 players) | 28 of 120 (35 players) |
| AI-only: completed transfers a window, 30-year mean | 48 | 48 |
| AI-only: lowest / median / highest balance, year 30 | 0.97M / 7.06M / 22.49M | 1.03M / 9.61M / 27.74M |
| Passive manager: balance year 30, squad average year 5 / 30 | 28.0M, 51 / 51 | 29.7M, 52 / 51 |
| Recruiting manager: balance year 30, squad average year 30 | 22.9M, 61 | 16.2M, 63 |
| Recruiting manager: career squad average per seed, titles in 30 seasons | 58–60, 4–11 | 59–63, 4–8 |
| Free-agent pool at the open, year 2 / 10 / 30 | AI-only 4 / 4 / 4; passive 8 / 15 / 19; recruiting 6 / 7 / 10 | AI-only 4 / 4 / 4; passive 8 / 15 / 16; recruiting 6 / 9 / 8 |
| Pool at the close, year 2 / 10 / 30 | AI-only 0; passive 0 / 5 / 9; recruiting 0 / 0 / 2 | AI-only 0; passive 1 / 6 / 7; recruiting 0 / 1 / 1 |

**Role fallback works where it was aimed.** The AI-only market now leaves 4 vacancies in 300 windows (seeds 1 y2, 13 y4 and y6, 99 y3; one position each; no free agent in the pool at the close), and the passive manager's 3 shortages are gone. Nobody falls below zero and population stays 640–663.

**The recruiting manager's world still has gaps: 28 of 120 windows, 35 players** (MF 14, DF 9, FW 8, GK 4; seeds 7: 7 windows, 42: 5, 99: 10, 2026: 6). In 14 of those windows the free-agent pool is empty at the close; in the other 14 it holds 1–4 players (the sweep does not record their positions). The shortages cluster in the first years (seed 99: years 2–4, seed 7: 2–5, seed 42: year 2) and scatter afterwards. The rate did not move against v6 (27 of 120); the fallback removed the proven seed-42 / club-18 case, but club 18 is still short in year 2 and year 7 in that run (an unfillable MF, then a DF). This sweep cannot tell reserve-blocked listings from missing supply: that split needs the investigation overlay in `squad-market-investigation.md`. The recruiting manager's own signings (3 a year) and purchases take the same free agents the AI clubs would refill from, which is the plausible mechanism and not isolated.

**The passive manager's world is unchanged in kind.** 56–58% of listed players go unsold (24–28 listed, 14–16 unsold from year 10), the pool at the open grows to 16 and keeps 7 at the close, the best in it rates 43 against squad averages of 59. Reading `balance--passive-manager-investigation`: unsold fringe is a different thing from a positional shortage, and the short-window count (0 of 120) now says the shortage half is closed. Only the unsold share and the long-lived pool remain, and they cost the game nothing yet (no AI club is short).

**Money: gate and prize income keep every club growing.** The AI-only median is 3.6M at year 10, 7.0M at year 20 and 9.6M at year 30 (3.0M, 4.6M and 6.5M at the first baseline; 7.1M at year 30 in the `simple` v5 rerun) and the richest club 27.7M (22.5M): the Continental Cup's 3.1M a year (first paid in year 2, see [Money by division](#money-by-division)) lands in the first division's eight cup clubs, and the effect shows as the compounding gap after year 10. No club goes below zero in 300 club-careers and the lowest balance holds near 0.9–1.1M.

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceAIMarket -v -count=1   # about 110 s + 45 s + 45 s on 4 cores
```

### History

- **2026-09-28**, 16 clubs, `ai.TransfersVersion` 2: 27 transfers a window, every bid completed, the 16 best players 12 a window, strength gap 6–8, median balance 8.5M at year 30, free-agent pool empty (AI-only) or 2–4 (passive). Filed `squad--star-churn` and `squad--free-agent-pool`.
- **2026-10-01**, 32 clubs, `ai.TransfersVersion` 6: this section, at `simple` v4 (commit `15ff2fa`), and rerun at `simple` v5 (commit `64874ae`).
- **2026-10-02**, `ai.TransfersVersion` 7, `worldgen.Version` 9, `LeagueVersion` 7, cup prizes posted: the rerun above. The tables above it are the version-6 baseline and are historical.

## Match engines: tick against simple

Measured 2026-10-02 at `tick.ModelVersion` 8 and `simple.ModelVersion` 5, both with their `DefaultParams`, at commit `6de7f91`. Reruns the comparison after `match--tick-stats-calibration` (tick v8); `simple` is unchanged since the v7 run, and its columns reproduce exactly. Earlier measurements are in the [history](#history-1).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceEngineComparison' -v -count=1 -timeout 2h   # about 3 min on 32 cores, 22 on 4
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceMentalityByGap' -v -count=1 -timeout 2h     # about 2.5 min on 32 cores, 13 on 4
```

(`go test`'s default 10-minute timeout is too short on a small machine: pass `-timeout`.)

`TestBalanceEngineComparison` in `internal/matches/tick/balance_test.go` plays both engines on the same `enginetest.Input` teams: 11 starters and 7 substitutes, balanced unless stated, full condition, no commands. A team's "strength" is the rating its profile is built around, and it is close to its overall. Each row is 3,000 matches: seeds 1, 42 and 2026 × fixtures 1–1000, with each engine's own `matches.FixtureRandom` stream. Every match is played as a knockout. The contract guarantees that the knockout rule leaves the 90 minutes unchanged, so one run gives both the regulation result and the shootout. With 3,000 matches, a rate near 25% is good to about ±1.6 points (95%), one near 50% to about ±1.8, a goal average to about ±0.04, and points per match to about ±0.05.

These are synthetic teams, not career squads; the career-season comparison is in ["Career seasons: tick against simple"](#career-seasons-tick-against-simple).

### Equal teams, 60 v 60

| | simple | tick | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match | 2.71 | 2.42 | 2.6–2.9 |
| Home–away goals | 1.54–1.17 | 1.33–1.09 | 1.5–1.2 |
| Home / draw / away % | 44.9 / 26.1 / 29.1 | 43.0 / 26.9 / 30.1 | 45 / 26 / 29 |
| Draw % if the two scores were independent | 25.5 | 27.5 | |
| 0–0 % | 6.9 | 7.6 | 7–8 |
| 4 or more goals % | 28.1 | 21.7 | 25–30 |

| Total goals (% of matches) | 0 | 1 | 2 | 3 | 4 | 5 | 6+ |
| --- | --- | --- | --- | --- | --- | --- | --- |
| simple | 6.9 | 17.0 | 25.1 | 22.8 | 14.3 | 8.0 | 5.8 |
| tick | 7.6 | 21.8 | 26.6 | 22.3 | 12.5 | 6.1 | 3.1 |

**`tick` v8 is back on the real column on draws, 0–0s and the home edge,** and still short on goals. 43.0 / 26.9 / 30.1 against the real 45 / 26 / 29; 7.6% 0–0s (v7 9.4; real 7–8) and 26.9% draws (29.1); the home edge is 13 points (11 at v7). Goals are 2.42 (v7 2.40), below the 2.6–2.9 range, and the scoring is thin at the top: 21.7% of matches have four or more goals (real 25–30). The goal level is the open `match--career-goals`, and the career rows are lower still (2.16, see [Career seasons](#career-seasons-tick-against-simple)). `simple` has a home edge (44.9 against 29.1) and is unchanged.

### Rating gap (home strength v away strength)

| Match | simple H / D / A % | simple goals | tick H / D / A % | tick goals |
| --- | --- | --- | --- | --- |
| 70 v 50 | 70.8 / 17.8 / 11.4 | 2.97 | 82.8 / 11.9 / 5.3 | 3.07 |
| 65 v 55 | 58.2 / 23.3 / 18.4 | 2.81 | 64.3 / 21.8 / 13.9 | 2.61 |
| 62 v 58 | 50.4 / 24.6 / 25.0 | 2.73 | 50.1 / 25.5 / 24.4 | 2.52 |
| 60 v 60 | 44.9 / 26.1 / 29.1 | 2.71 | 43.0 / 26.9 / 30.1 | 2.42 |
| 58 v 62 | 40.5 / 25.4 / 34.1 | 2.70 | 34.8 / 26.5 / 38.7 | 2.42 |
| 55 v 65 | 33.4 / 25.1 / 41.6 | 2.69 | 22.1 / 26.1 / 51.9 | 2.41 |
| 50 v 70 | 22.7 / 23.8 / 53.5 | 2.71 | 10.8 / 19.4 / 69.8 | 2.73 |

In a career, squad averages range from 51 to 65 (see the market section), so the gaps that matter are the 62 v 58 and 65 v 55 rows. `tick` v8 is steeper than v7 and than `simple`: a home side 10 points stronger wins 64% (v7 63%, `simple` 58%), a 10-point weaker home side 22% (v7 25%, `simple` 33%), and a 20-point mismatch 83% (v7 80%) with 3.1 goals. The steeper gap is the engine's; the career rows show a flatter one (the strongest eleven, not the squad average, plays), see [Career seasons](#career-seasons-tick-against-simple).

### Quality level

| Match | simple goals | simple draw % | tick goals | tick draw % | tick 0–0 % | tick 4+ goals % |
| --- | --- | --- | --- | --- | --- | --- |
| 40 v 40 | 2.78 | 25.3 | 2.49 | 27.1 | 8.8 | 23.9 |
| 60 v 60 | 2.71 | 26.1 | 2.42 | 26.9 | 7.6 | 21.7 |
| 80 v 80 | 2.69 | 25.6 | 2.45 | 27.1 | 8.7 | 23.9 |

Goals stay flat across levels in both engines (`tick` 2.42–2.49, a 0.07 spread, against 2.1–4.6 at v1).

### Mentality

Points per match are 3 × wins + draws, from the named side's view; Δ is against the same side playing balanced in the same match.

| Home v away (60 v 60) | simple goals | simple H / D / A % | tick goals | tick H / D / A % |
| --- | --- | --- | --- | --- |
| balanced v balanced | 2.71 | 44.9 / 26.1 / 29.1 | 2.42 | 43.0 / 26.9 / 30.1 |
| attacking v attacking | 3.75 | 49.5 / 20.6 / 30.0 | 3.23 | 45.0 / 22.0 / 33.0 |
| defensive v defensive | 1.82 | 40.8 / 32.3 / 26.9 | 1.41 | 35.8 / 39.8 / 24.4 |
| attacking v balanced | 3.20 | 48.0 / 23.7 / 28.3 | 2.76 | 45.5 / 24.7 / 29.8 |
| balanced v attacking | 3.18 | 44.8 / 24.8 / 30.4 | 2.77 | 44.2 / 24.6 / 31.2 |
| defensive v balanced | 2.21 | 41.2 / 28.3 / 30.5 | 1.97 | 40.6 / 30.9 / 28.5 |
| balanced v defensive | 2.22 | 45.0 / 27.3 / 27.7 | 1.95 | 39.8 / 30.6 / 29.6 |
| attacking v defensive | 2.65 | 48.0 / 25.4 / 26.6 | 2.34 | 41.0 / 27.0 / 32.0 |
| defensive v attacking | 2.60 | 40.6 / 27.3 / 32.1 | 2.33 | 44.4 / 28.5 / 27.1 |

By the rating gap and venue (`TestBalanceMentalityByGap` for the gap rows, `TestBalanceEngineComparison` for 60 v 60), points per match for the named side:

| Side | engine | balanced | defensive (Δ) | attacking (Δ) | goals for / against: balanced → defensive → attacking |
| --- | --- | --- | --- | --- | --- |
| Equal, 60 v 60 at home | tick | 1.56 | 1.53 (−0.03) | 1.61 (+0.05) | 1.33 / 1.09 → 1.10 / 0.87 → 1.55 / 1.21 |
| | simple | 1.61 | 1.52 (−0.09) | 1.68 (+0.07) | 1.54 / 1.17 → 1.22 / 0.98 → 1.84 / 1.36 |
| Equal, 60 v 60 away | tick | 1.17 | 1.19 (+0.02) | 1.18 (+0.01) | 1.09 / 1.33 → 0.88 / 1.07 → 1.25 / 1.52 |
| | simple | 1.13 | 1.10 (−0.03) | 1.16 (+0.03) | 1.17 / 1.54 → 0.93 / 1.30 → 1.42 / 1.77 |
| Underdog, 55 v 65 at home | tick | 0.92 | 1.00 (+0.07) | 0.96 (+0.03) | 0.90 / 1.51 → 0.75 / 1.21 → 1.10 / 1.74 |
| | simple | 1.25 | 1.21 (−0.05) | 1.31 (+0.05) | 1.27 / 1.41 → 1.02 / 1.20 → 1.53 / 1.63 |
| Favourite, 65 v 55 at home | tick | 2.15 | 2.07 (−0.07) | 2.16 (+0.01) | 1.86 / 0.75 → 1.55 / 0.59 → 2.08 / 0.89 |
| | simple | 1.98 | 1.85 (−0.13) | 2.06 (+0.08) | 1.84 / 0.96 → 1.47 / 0.81 → 2.21 / 1.12 |
| Underdog, 65 v 55 away | tick | 0.64 | 0.70 (+0.07) | 0.61 (−0.02) | 0.75 / 1.86 → 0.59 / 1.50 → 0.88 / 2.10 |
| | simple | 0.79 | 0.79 (+0.00) | 0.81 (+0.02) | 0.96 / 1.84 → 0.76 / 1.56 → 1.17 / 2.12 |

**`tick`: mentality is still a trade-off at v8.** Attacking against a balanced side is worth −0.02 to +0.05 points a match for the named side (v6: +0.11 to +0.24; `match`'s always-on bound is +0.15), at +0.16–0.22 goals for and +0.12–0.19 against at equal strength. Defensive pays for the underdog (+0.07 at home and away at 55 v 65 and 65 v 55) and costs the favourite 0.07; it also draws more (29.9% against 26.1% for the home underdog). Every Δ is within ±0.08, a choice of style rather than a dominant setting.

**`simple`, the career default, still has the v6 shape at a smaller scale.** Attacking beats balanced in every row (+0.02 to +0.08), because it adds 0.21–0.37 goals scored against 0.16–0.28 conceded (more scored than conceded in four of the five rows), and defensive never beats balanced, even for the underdog (−0.13 to +0.00). The single rows are one to two sampling margins, but all ten point the same way. A career manager on the default engine should always attack and never defend; filed as `match--simple-mentality`. Neither engine charges for attacking outside the goal model (`medical` reads minutes only), the AI always plays balanced (`ai.SelectionVersion` 3), and switching late to protect a lead is not measured.

### Shootouts

A knockout tie goes to penalties exactly when it is level after 90 minutes: 26–27% of ties between equal teams in either engine (26.9% in `tick`). The sweep checks that the shootout count equals the draw count in every row. There is no extra time, which roughly doubles or triples the real-world shootout rate; a rules choice for `competitions` and `match`.

| Home (stronger) side wins the shootout, % | 60 v 60 | 62 v 58 | 65 v 55 | 70 v 50 |
| --- | --- | --- | --- | --- |
| simple | 51 | 52 | 56 | 61 |
| tick | 53 | 54 | 56 | 58 |

At 50 v 70 the home side is the weaker one and wins 39% of the shootouts in both engines. Both sides together score 8.0–8.7 penalties per shootout. Shootouts stay close to a coin toss with a modest edge for the better side, as at v6 (`match--shootout-favourite` holds). Both engines have an always-on shootout bound (`TestShootoutsStayClose`, delivered with `match--mentality-shootout-bounds`).

### History

- **2026-09-28**, `tick` v1, `simple` v3: `tick` drew 24.9% and scored 2.9 goals at 60 v 60, 4.6 at 80 v 80 and 2.1 at 40 v 40, a 10-point mismatch gave 72% wins and a 20-point one 4.3 goals. Attacking raised the win rate by 20 points with no cost at the back; a defensive side against a balanced one conceded 4.7 goals. A stronger side won 74–80% of shootouts at 65 v 55. Filed `match--tick-goals-by-level`, `match--tick-mentality` and `match--shootout-favourite`.
- **2026-10-01**, `tick` v6, `simple` v4 (commit `78fc6b2`): `tick` 2.46 goals and 42.4 / 26.8 / 30.8 at 60 v 60; `simple` 2.71 goals and no home edge (37.2 / 26.3 / 36.6). Attacking against balanced was worth +0.11 to +0.24 points a match on `tick` and defensive −0.04 to −0.11; shootouts 53–60% for the stronger side. Filed `match--attacking-is-free` and `match--simple-home-advantage`.
- **2026-10-01**, `tick` v7, `simple` v5: `tick` 2.40 goals and 41.0 / 29.1 / 29.9 at 60 v 60, 9.4% 0–0s; attacking worth −0.04 to +0.04; home side wins 48–62% of shootouts from 60 v 60 to 70 v 50.
- **2026-10-02**, `tick` v8, `simple` v5: this section.

## Career seasons: tick against simple

Measured 2026-10-02 at `tick.ModelVersion` 8 and `simple.ModelVersion` 5 (both `DefaultParams`), `medical.Version` 4, `worldgen.Version` 8, `content.Version` 9, `competitions.ScheduleVersion` 4, `ai.SelectionVersion` 3, at commit `6de7f91`. The previous run (`tick` v7, `simple` v5, `medical` 3, `ScheduleVersion` 3) is in the [history](#history-2).

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1   # about 80 s on 4 cores
```

`TestBalanceCareerEngines` in `internal/app/balance_test.go` plays the same three seeded AI-only careers (seeds 7, 42 and 2026, three seasons each) on both engines: one world per (seed, engine), so the market, promotions and cups carry over, and each engine plays every fixture of the career. The engines face the same fixtures (asserted). Since `Continue` resolves batches without a user fixture by itself, the sweep stops the clock a minute before each kickoff to sample the squads and then continues to it; it fails if a season measures no matches. A season is a football year as in `playSeason`: the league, the promotion play-offs, then the cup matches still to come. Since `ScheduleVersion` 4 a cup edition is drawn from league season N and played during season N+1, so **season 1 has no cup** and the cup rows count the 6 seasons per engine that have one; the sweep classifies each match by its competition's format, not by the stage of the sweep. "Upsets" are league matches won by the club with the lower squad average before the season; the gap rows bucket that average gap. Each row is 9 seasons × 224 league matches = 2,016 matches.

### League matches

| | simple | tick | Synthetic 60 v 60, simple / tick | Top leagues, roughly |
| --- | --- | --- | --- | --- |
| Goals per match | 2.17 | 2.16 | 2.71 / 2.42 | 2.6–2.9 |
| Home–away goals | 1.25–0.93 | 1.20–0.96 | 1.54–1.17 / 1.33–1.09 | 1.5–1.2 |
| Home / draw / away % | 44.2 / 28.4 / 27.3 | 41.2 / 29.4 / 29.4 | 44.9 / 26.1 / 29.1 · 43.0 / 26.9 / 30.1 | 45 / 26 / 29 |
| Upsets (weaker club wins) % | 29.6 | 27.9 | | |
| Stronger side win %, gap 0–1 / 2–4 / 5–8 / 9+ | 36 / 35 / 46 / 29 | 39 / 35 / 42 / 60 | | |
| matches in those buckets | 618 / 920 / 130 / 14 | 644 / 900 / 130 / 10 | | |

**Career matches still score 0.4–0.5 goals less than real football.** `simple` 2.17 (unchanged), `tick` 2.16, down from 2.30 at v7: `tick` v8's per-minute calibration took 0.14 goals from careers. This is the `match--career-goals` note, now with a worse `tick` figure. The synthetic 60 v 60 columns are the v8 rows from [Match engines](#match-engines-tick-against-simple): `tick` loses 0.26 goals between its synthetic and its career rows (2.42 to 2.16), `simple` 0.54 (2.71 to 2.17), so the career level drifts unwatched under both.

**Home edge and upsets hold** (41–44% home wins, 27–29% away; about 28–30% of matches won by the weaker club). Results separate by squad average no more than at v7: the stronger side wins 35–46% of matches up to a gap of 8; the 9+ bucket has 14 and 10 matches, too few to read.

**Final tables and shootouts agree across engines.**

| | simple | tick |
| --- | --- | --- |
| Champion points (of 42) | 26.1 | 26.9 |
| Last points | 11.6 | 11.1 |
| Spread | 14.6 | 15.8 |
| Continental Cup matches a season with a cup | 7.0 | 7.0 |
| Cup matches level after 90 minutes, % | 24 | 31 |
| Promotion play-off ties a season | 4.0 | 4.0 |
| Ties to penalties, % | 22 | 33 |

Means over 36 league tables (4 leagues × 9 seasons). The champion takes 1.86–1.92 points a match and the last 0.79–0.83. The cup and play-off rates rest on 42 cup matches and 36 ties per engine (±8–15 points), so the gaps between engines are noise. Workload (condition and injuries) is engine-independent and lives under [Injuries](#injuries).

### Rerun at `simple` v6 (generation 9, `LeagueVersion` 7)

Measured 2026-10-02 at `simple.ModelVersion` 6, `tick.ModelVersion` 8 (unchanged), `worldgen.Version` 9, `content.LeagueVersion` 7 (three-week league intervals), `ScheduleVersion` 4, `medical.Version` 4, schema 32, same seeds and command. Answers `balance--simple-v6-rerun` and the career half of `balance--august-may-calendar`. The `tick` column also moves, because generation 9 and the August-to-May calendar changed every career world; the engines' own parameters did not. The tables above are the `simple` v5 / `LeagueVersion` 6 run and are historical.

| | simple v6 | tick v8 | Top leagues, roughly |
| --- | --- | --- | --- |
| Goals per match | **2.60** (2.17 at v5) | 2.19 (2.16) | 2.6–2.9 |
| Home–away goals | 1.48–1.12 | 1.23–0.95 | 1.5–1.2 |
| Home / draw / away % | 46.0 / 26.1 / 27.8 | 43.1 / 28.0 / 28.9 | 45 / 26 / 29 |
| Upsets (weaker club wins) % | 31.2 | 31.1 | |
| Stronger side win %, gap 0–1 / 2–4 / 5–8 / 9+ | 34 / 40 / 39 / 46 | 31 / 38 / 41 / 44 | |
| matches in those buckets | 408 / 944 / 416 / 26 | 400 / 952 / 418 / 34 | |
| Champion / last points (of 42) | 27.4 / 11.7 | 26.5 / 11.9 | |
| Cup matches level after 90 min, % / play-off ties to pens, % | 40 / 33 | 33 / 28 | |

**`simple` now sits on the real goal level** (2.60, inside the 2.5–2.9 target of `match--career-goals`) with the home edge and draw rate on their reference column (46.0 / 26.1 / 27.8 against 45 / 26 / 29), so the draw rate is not off. **`tick` is still 0.4 below** (2.19; it was 2.16): the `tick` half of `match--career-goals` stays open.

**The upset rate (31.1–31.2%) is higher than at v5 (29.6 and 27.9%) in both engines**, `tick` included, whose parameters did not change, so the cause is the career world, not an engine. I did not isolate it; the likeliest candidates are generation 9 (youth ranges developed to the starting age) and the three-week calendar, which keep squads closer in strength (about 1,350 of the 1,800 matches an engine plays fall in the gap 0–4 buckets). The stronger side wins only 31–34% of matches at a gap of 0–1 and 38–46% beyond, so **a favourite is worth little in either engine**; the 9+ bucket has 26–34 matches, too few to read. I have no real-football reference by gap, so I file nothing; it is a number to keep watching.

**Final tables stay near real football's** (the champion takes 1.96 points a match in `simple` and 1.89 in `tick`, the last 0.84 and 0.85). `simple`'s spread (15.7) exceeds `tick`'s (14.6): the higher goal rate separates more. Cup and play-off rates rest on 42 cup matches and 36 ties per engine (±8–15 points), so 40 against 33% penalties is noise. `simple` scoring 2.60 goals does not change workload or injuries (same 9.17 injuries a club, 19.5 days).

### History

- **2026-10-01**, `tick` v6, `simple` v4 (commit `5f07b58`): 2.18 (`simple`) and 2.23 (`tick`) goals; `simple` without a home edge (35.4 / 30.3 / 34.3), `tick` 42.8 / 27.8 / 29.4; champion 26.2–26.4 points, last 11.6–11.8.
- **2026-10-01**, `tick` v7, `simple` v5 (commit `64874ae`, `medical` 3, `ScheduleVersion` 3): 2.16 (`simple`) and 2.30 (`tick`) goals, home edge on both (43.7 and 43.0% home wins); champion 26.1 and 26.9 points, last 12.2 and 10.9.
- **2026-10-02**, `tick` v8, `simple` v5, `medical` 4, `ScheduleVersion` 4: this section. The sweep follows the new cup calendar (season 1 has no cup edition).
- **2026-10-02**, `simple` v6, `tick` v8, generation 9, `LeagueVersion` 7: the rerun above (2.60 and 2.19 goals; both engines see 31% upsets).

## Match statistics: tick against real football

Measured 2026-10-02 at `tick.ModelVersion` 8, commit `6de7f91` (first measured at v6, see the [history](#history-3)). Answers `balance--tick-match-stats` and, with the career rows, `balance--tick-v8-rerun`. The real-football figures are `match`'s calibration targets from that note. `simple` reports no statistics (no `DetailedStats` capability).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalanceMatchStats -v -count=1 -timeout 2h   # about 2 min on 32 cores, 12 on 4
```

`TestBalanceMatchStats` in `internal/matches/tick/balance_test.go`, 3,000 synthetic matches a row (seeds 1, 42, 2026 × 1,000 fixtures, `enginetest.Input` teams). The career rows are `TestBalanceCareerEngines`'s 2,016 league matches on `tick` (above). Both sides of a match are listed (home / away where it matters).

### Against real football (per side)

| Stat | Real top leagues | tick 60 v 60 | tick careers | tick 70 v 50 (stronger / weaker) |
| --- | --- | --- | --- | --- |
| Shots | 12–13 | 11.9 / 10.2 | 12.4 / 10.6 | 19.0 / 5.7 |
| On target | 4–5 | 5.6 / 4.6 | 5.3 / 4.4 | 10.1 / 2.3 |
| Saves | 2–3 | 3.6 / 4.3 | 3.5 / 4.1 | 1.8 / 7.7 |
| Passes | 400–600 | 965 / 905 | 969 / 907 | 1148 / 710 |
| Completion % | 75–85 | 79 / 77 | 81 / 79 | 85 / 68 |
| Tackles | 15–20 | 26.9 / 26.4 | 24.2 / 22.6 | 26.8 / 23.1 |
| Offsides | ~2 | 1.9 / 1.7 | 2.0 / 1.7 | 2.4 / 1.1 |
| Possession, stronger side % | 60–65 at a clear gap | 51.7 (even) | 51.8 (even) | 62.7 at gap 20 |

**v8 moved the targets `match` aimed at.** Tackles fall from 35–52 to 23–27 a side (v7: 45.8 / 43.4 at 60 v 60), **possession now separates**: 62.7% for a 20-point stronger side and 57.5% at gap 10, inside the 60–65% target (v7: 56.7 and 53.8), and **shots between equal teams rise** to 22.1 a match (v7 20.3, real 24–26). Equal teams stay at 51.7/48.3, as in real football. **Passes stay about 1.6–2.4 times the target** (710–1,148 a side against 400–600), for the reason `match` gives (tick has about 88 minutes of ball in play against about 58 real); completion is right.

**Shots on target are now a little high and the saves with them**: 5.6 / 4.6 on target at 60 v 60 against 4–5 (v7 4.4 / 3.6), because on-target and saves now count shots at the goal line. On target equals goals plus the opponent's saves, as before. **The shot split is still lopsided**: at gap 20 it is 19.0–5.7 (v7 16.3–6.2), where real football at gap 10 is about 12.6–9.3, and at gap 10 15.2–7.7. Offsides are right (about 4.4 a side when both attack, 0.1 when both defend). The career rows match the synthetic profile within a few tenths, except tackles (24.2 / 22.6 against 26.9 / 26.4), a difference I did not trace.

**The per-ball-in-play-minute framing is fair, but I cannot add a ball-in-play row from outside.** The engine reports no ball-in-play time in `matches.MatchStats` or the event log, so the row would be `match`'s own figure. Taking its 88 against 58 minutes at face value, tackles scale to 17–18 a side and passes to about 630 at 60 v 60, which puts tackles inside 15–20 and passes about 5% above the 600 ceiling; the framing therefore agrees with the targets. The scaling is arithmetic on a number I did not measure.

### Synthetic teams, all rows (3,000 matches each)

| Scenario | Side | Shots | On target | Saves | Passes | Completion % | Tackles | Offsides | Possession % |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 60 v 60 | home | 11.9 | 5.6 | 3.6 | 965 | 79 | 26.9 | 1.9 | 51.7 |
| 60 v 60 | away | 10.2 | 4.6 | 4.3 | 905 | 77 | 26.4 | 1.7 | 48.3 |
| 65 v 55 | home | 15.2 | 7.6 | 2.6 | 1064 | 83 | 27.0 | 2.2 | 57.5 |
| 65 v 55 | away | 7.7 | 3.3 | 5.8 | 803 | 73 | 25.1 | 1.4 | 42.5 |
| 55 v 65 | home | 9.1 | 4.0 | 4.8 | 864 | 75 | 26.0 | 1.6 | 46.0 |
| 55 v 65 | away | 13.1 | 6.3 | 3.1 | 1006 | 81 | 26.9 | 2.0 | 54.0 |
| 70 v 50 | home | 19.0 | 10.1 | 1.8 | 1148 | 85 | 26.8 | 2.4 | 62.7 |
| 70 v 50 | away | 5.7 | 2.3 | 7.7 | 710 | 68 | 23.1 | 1.1 | 37.3 |
| 50 v 70 | home | 7.0 | 2.9 | 6.3 | 768 | 71 | 24.4 | 1.3 | 40.5 |
| 50 v 70 | away | 16.4 | 8.3 | 2.3 | 1097 | 84 | 26.9 | 2.3 | 59.5 |
| 80 v 80 | home | 11.8 | 5.6 | 3.6 | 966 | 79 | 27.9 | 1.9 | 51.8 |
| 80 v 80 | away | 10.2 | 4.7 | 4.3 | 904 | 77 | 27.7 | 1.7 | 48.2 |
| both attacking | home | 16.4 | 7.5 | 4.9 | 989 | 79 | 24.3 | 4.6 | 51.8 |
| both attacking | away | 14.3 | 6.4 | 5.8 | 926 | 77 | 24.0 | 4.2 | 48.2 |
| both defensive | home | 7.3 | 3.4 | 2.1 | 949 | 81 | 25.2 | 0.2 | 52.2 |
| both defensive | away | 6.0 | 2.7 | 2.6 | 872 | 78 | 25.1 | 0.1 | 47.8 |
| home attacking | home | 13.8 | 6.4 | 4.2 | 970 | 79 | 25.4 | 2.4 | 52.3 |
| home attacking | away | 12.1 | 5.4 | 4.9 | 921 | 77 | 25.9 | 3.2 | 47.7 |

An attacking side shoots more and is caught offside more (2.4–4.6 a side); the balanced side facing it shoots more too (12.1 against 10.2) and is caught offside more (3.2, against 1.7). Possession barely moves with mentality (52.3% for the attacking home side, 51.7% balanced). A defensive block concedes few shots and is almost never caught offside (0.1–0.2).

### History

- **2026-10-01**, `tick` v6 (commit `5f07b58`): the same figures within 0.1, except the mentality rows: both attacking 13.4–11.9 shots, an attacking home side 13.4–9.5 shots and 49.9% possession. Filed `match--tick-stats-calibration`.
- **2026-10-01**, `tick` v7 (commit `64874ae`): the same figures within 0.1 of v6 outside the mentality rows. Passes 1.7–2.5 times the target, tackles 2–3 times, possession 56.7% at gap 20, shots 20–21 a match between equal teams.
- **2026-10-02**, `tick` v8: this section. Tackles 23–27 a side, possession 62.7% at gap 20, shots 22.1 a match between equal teams, passes unchanged.

## Injuries

Measured 2026-10-02 at `medical.Version` 4, `tick` v8 and `simple` v5, `ScheduleVersion` 4, commit `6de7f91`, over the 18 career seasons of [Career seasons](#career-seasons-tick-against-simple) (3 seeds × 3 seasons × 2 engines = 576 club-seasons). Answers `balance--injuries-delivered` and `balance--injury-calibration-delivered`. `medical` reads minutes and condition, not the engine, so both engines agree within noise; the rows below pool all 18 seasons.

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceCareerEngines -v -count=1       # rates and condition, about 80 s on 4 cores
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceRotation -v -count=1 -timeout 3h # the policy comparison, about 10 min on 4 cores
```

| | per season | per club a season | per injury | per injured player a season | per injured club-season |
| --- | --- | --- | --- | --- | --- |
| Injuries | 354 | 11.1 | | 1.3 injuries | 11.1 injuries |
| Days out | 6,950 | 217 | 19.7 days | 25.6 days (max 202) | 217 days |

**Rates now meet `squad`'s 0.5–1.0 target.** 354 injuries a season across 32 clubs is 11.1 a club, about 0.55 a player a year on 20-man squads (it was 76 and 0.12 at `medical.Version` 3), and a layoff averages 19.7 days (`squad`'s own run: 11.1 a club, 19.4 days). A club loses about 217 player-days a season.

**Starters are tired.** A starter's condition before his match averages 95.3–95.4 (both engines), with 44% of starts below 100 and 17% below 90; the mean over every active player, bench and free agents included, is 95.1. The fatigue term now has something to work on, and `squad`'s own figure (96.0) agrees within seed noise.

**A club is still never short of fit players.** 0 short-of-fit club-batches and 0 emergency starts in about 11,000 club-batches: the emergency rule (injured players play) never fires at 11 injuries a club a season.

### Rerun on the default football year (`LeagueVersion` 7, generation 9)

Measured 2026-10-02 at `medical.Version` 4, `simple` v6 and `tick` v8, `worldgen.Version` 9, `content.LeagueVersion` 7 (an August-to-May season with three-week league intervals, the cup played midweek in season 2 onward), `ScheduleVersion` 4, over the same 18 career seasons. Answers the career half of `balance--august-may-calendar` and `balance--football-year-medical-measurements`. The tables below this one (11.1 injuries a club, starters at 95.3) were measured on the earlier weekly calendar and are **historical**; `squad`'s weekly regression still guards that calendar.

| | weekly calendar (`LeagueVersion` 5–6) | default football year (`LeagueVersion` 7) |
| --- | --- | --- |
| Injuries a club a season | 11.1 | **9.17** (293 a season over 32 clubs) |
| Per player a year (20-man squads) | 0.55 | 0.46 |
| Days a layoff | 19.7 | 19.5–19.6 |
| Days out per injured player a season | 25.6 | 27.3 |
| Condition before a round, every active player | 95.1 | 99.2 |
| Starters' condition at kickoff | 95.3 (17% below 90) | **99.8** (2% below 100, 1% below 90) |
| Short-of-fit club-batches, emergency starts | 0, 0 | 0, 0 |

**The calendar, not the medical rules, ended the tired starters.** With three-week league intervals the daily recovery task restores a squad between matches: 99% of starts are at 90 or above. The injury rate is 0.46 a player a year, just under `squad`'s 0.5–1.0 target and inside the 0.429–0.487 that `squad` measured on its own seeds, so I confirm its figure. Both engines agree within noise (293.3 and 293.6 a season).

**Congestion exists only where the cup puts it** (below): 3% of the starts in a cup-exposed year follow a gap under 7 days, and 24–61% of those starters are below 90 condition (the 2.2–2.4% overall `squad` measured is the same population).

### Rotation policy on matched worlds (weekly calendar; historical)

The `TestBalanceRotation` sweep plays one football year of the same seeded world once per lineup policy for each managed club. The manager signs and renews nobody, so the arms differ only in his lineups; results are compared on matched (seed, club) pairs. The policies are **carry** (submit the AI's selection for the first match, nothing after: the world carries it forward and refills only injured places), **rotate** (submit the AI's selection every match: `RoleScore × condition`, so a tired player gives way) and **best** (the same selection with every condition read as 100: the strongest eleven whoever is tired). A manager who never submits gets the AI's selection every match, identical to rotate: the carry-over chain starts only from a stored lineup.

`simple`, 128 pairs (seeds 1–4, every club):

| Policy | League points (of 42) | Injuries | Days lost | Starters' condition | Starts below 90 % | Players used |
| --- | --- | --- | --- | --- | --- | --- |
| carry | 17.94 | 15.10 | 297 | 86.6 | 39 | 15.7 |
| rotate | 18.80 | 11.04 | 214 | 96.4 | 12 | 17.0 |
| best | 18.19 | 14.85 | 294 | 87.0 | 38 | 15.6 |

Differences against rotate (mean ± standard error): carry −0.87 ± 0.19 points, +4.06 ± 0.21 injuries, +82.7 ± 6.5 days; best −0.62 ± 0.18 points, +3.81 ± 0.22 injuries, +80.1 ± 6.4 days.

`tick`, 24 pairs (seeds 7, 42, 2026, every fourth club):

| Policy | League points | Injuries | Days lost | Starters' condition | Starts below 90 % | Players used |
| --- | --- | --- | --- | --- | --- | --- |
| carry | 16.33 | 15.25 | 288 | 85.7 | 41 | 15.6 |
| rotate | 19.29 | 11.21 | 212 | 95.9 | 15 | 17.1 |
| best | 17.17 | 14.67 | 283 | 86.3 | 39 | 15.6 |

Differences against rotate: carry −2.96 ± 1.21 points, +4.04 ± 0.51 injuries, +75.2 ± 14.4 days; best −2.12 ± 1.09 points, +3.46 ± 0.40 injuries, +70.4 ± 10.6 days.

**Rotation pays, on both engines and on every measure.** Rotating the squad by condition earns 0.6–0.9 points a season more than the strongest eleven or a fixed one on `simple` (about 3–5%) and 2–3 points on `tick` (24 pairs, so ±1.1–1.2), with over a quarter fewer injuries and days lost. Best and carry differ little: both leave the starters at 86–87 condition, because a 4-4-2 starter without a break plays on tired. No policy is dominant or pointless; the AI's default is the best of the three, so a manager has nothing to gain by taking the lineup into his own hands unless he beats the AI at it. The squad's depth matters: rotate uses 17 players a year against 15.6. Not measured: a manager who rotates by choice rather than by the AI's rule, in-match substitutions, and money (injured players keep their wages).

### Rotation policy on the football year, with and without a cup

Measured 2026-10-02 at the versions of the [rerun above](#rerun-on-the-default-football-year-leagueversion-7-generation-9), commit after `f1fddce`. `TestBalanceRotation` now plays two football years per arm and measures the last: **year 1** (no cup edition) and **year 2** (the Continental Cup is played during the league season, midweek, four days after a matchday). The manager's lineup policy applies in every year; he signs and renews nobody, so contracts that end after year 1 take the same players from every arm. It also reports cup matches a club played and its starts by the days since its previous match (under 7, 7–20, 21 or more). `simple` 128 pairs (seeds 1–4, every club), `tick` 24 pairs (seeds 7, 42, 2026, every fourth club); an arm plays the same world as its siblings.

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalanceRotation -v -count=1 -timeout 4h   # about 15 min on 4 cores
```

League points (of 42) and injuries, difference against rotate in parentheses (mean ± standard error):

| | year 1, simple | year 2, simple | year 1, tick | year 2, tick |
| --- | --- | --- | --- | --- |
| carry | 18.98 (−0.21 ± 0.14), 9.62 inj. | 17.09 (**−0.69 ± 0.22**), 9.42 | 18.08 (−0.21 ± 0.75), 9.17 | 15.58 (**−3.33 ± 1.25**), 9.00 |
| rotate | 19.19, 9.65 inj. | 17.77, 9.45 | 18.29, 8.92 | 18.92, 9.42 |
| best | 19.19 (+0.00 ± 0.00), 9.66 | 17.70 (−0.07 ± 0.06), 9.52 | 18.29 (+0.00), 8.92 | 19.08 (+0.17 ± 0.12), 9.58 |

Starters' condition at kickoff is 99.6–100.0 in every cell, days lost 159–190 a club, and a club uses 12.8–13.3 different players a league year (it was 15.6–17.1 on the weekly calendar).

Cup exposure and rest, year 2 (every club, so the average club plays 0.4–0.5 cup matches; about eight of 32 clubs have any):

| | simple: carry | rotate | best | tick: carry | rotate | best |
| --- | --- | --- | --- | --- | --- | --- |
| Starts under 7 days after the previous match, % | 3.1 | 3.2 | 3.3 | 2.6 | 3.1 | 4.0 |
| of them below 90 condition, % | 60.7 | **42.9** | 56.7 | 52.5 | **24.0** | 50.6 |
| Starts 7–20 days after, % (below 90, %) | 3.6 (0.9) | 3.7 (0.5) | 3.6 (0.8) | 4.0 (1.9) | 3.7 (0.7) | 4.0 (0.6) |
| Starts 21+ days after or the first, % (below 90, %) | 93.3 (0.0) | 93.1 (0.0) | 93.1 (0.0) | 93.4 (0.0) | 93.2 (0.0) | 92.1 (0.0) |

**On the default calendar the lineup decision is worth nothing in year 1 and little in year 2.** In a year without a cup every arm is within 0.2 points and 0.1 injuries of every other (the "rotate" and "best" arms are identical on `tick` and within 0.02 injuries on `simple`: nobody is tired, so the AI's condition term changes almost no choice; the largest gap, carry on `simple`, is −0.21 ± 0.14). In the cup year **carrying one eleven costs 0.7 points on `simple` (4%) and 3.3 on `tick` (18%, on 24 pairs)**, because a cup club's starters arrive tired to 61 and 53% of its short-rest matches. Rotating by condition cuts that to 43 and 24%, and the strongest-eleven policy ("best") is as good as rotating in points (+0.17 ± 0.12 on `tick`) but not in tiredness. Injuries do not follow tiredness here (−0.4 to +0.2, within one or two standard errors): injury risk in `medical` is drawn from exposure, not from condition.

**Rotation is not lost because "almost every fixture is rested"; it is lost because the calendar removes the problem outside cups.** 93–98% of starts follow three or more weeks of rest (leaving out the first match), and in them nobody is below 90. Where a rest gap exists (the cup), rotating by condition still pays, so the rule keeps its purpose; it is simply used about 3% of the time. This answers the question in `balance--football-year-medical-measurements`: the weekly regression measured congestion that the default calendar no longer produces, and a manager who never touches his lineup (the AI's selection each match, identical to "rotate") loses nothing. I do not propose a medical change: slowing recovery for all calendars is not justified, and more congestion is a calendar decision with `competitions`. Not measured: a club in several cup rounds (the average is 0.4 cup matches; clubs that reach the final play more, and their year is where the effect would be largest), a manager who also renews and signs, and money.

### History

- **2026-10-01**, `medical.Version` 3: 76 injuries a season (0.12 a player), 13.7 days a layoff, condition 99.8 before every round, 0 short-of-fit club-batches. Filed `squad--injury-rates`.
- **2026-10-02**, `medical.Version` 4: this section (354 injuries, 19.7 days, starters 95.3, rotation comparison) on the weekly calendar.
- **2026-10-02**, `LeagueVersion` 7, generation 9: the football-year rerun and the two-year rotation comparison above (9.17 injuries a club, starters 99.8; rotation matters only in the cup year).

## Population

Measured 2026-10-01 at commit `37a4f99`: `players.DevelopmentVersion` 1, `worldgen.Version` 8, `worldgen.YouthVersion` 4, `content.Version` 9, `content.LeagueVersion` 6, `ai.ContractsVersion` 2, `ai.TransfersVersion` 6, `ai.SelectionVersion` 3, `medical.Version` 4, `competitions.ScheduleVersion` 3, `simple.ModelVersion` 5 (the career default). AI-only, 32 clubs, 30 years, seeds 1, 2, 3, 5, 7, 11, 13, 42, 99 and 2026. Each year plays the season, the play-offs and the cup, then runs to the contract-year end, where everything below is measured (after the player year's development, retirements and youth intake, and the contract year's expiries and signings). A club's division is the one it plays in the coming season, read from the league tables, so promotion and relegation are followed.

```sh
ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1   # about 30 s on 32 cores
```

The same test reports [Attributes](#attributes) and [Money by division](#money-by-division). The always-on guard for this section is `squad`'s `TestSquadsStayLegalAndBalancedOverTheYears` (seed 7, 15 years: 600–680 active players, overall within 6 of the start, mean age 22–28), which these numbers sit well inside; no separate balance bound is needed.

| year | active | free | retired | youth | retire age | squads | age mean (p10–p90) | aged ≤20 | aged ≥31 | overall mean (p10–p90) | division 1 | division 2 | div. 2 clubs above div. 1's median |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 640 | 0 | | | | 20–20 | 25 (18–33) | 21% | 21% | 60 (49–70) | 60 | 59 | 6.4 of 16 |
| 1 | 640 | 4 | 26 | 26 | 34 | 18–20 | 26 (19–33) | 20% | 23% | 59 (47–70) | 59 | 59 | 5.5 |
| 3 | 640 | 4 | 41 | 41 | 35 | 19–20 | 26 (18–33) | 21% | 22% | 58 (44–72) | 58 | 58 | 5.5 |
| 5 | 640 | 4 | 35 | 35 | 35 | 19–21 | 25 (18–33) | 25% | 22% | 58 (42–73) | 58 | 58 | 5.0 |
| 10 | 640 | 4 | 34 | 34 | 35 | 18–20 | 25 (18–33) | 24% | 23% | 59 (43–75) | 59 | 58 | 4.5 |
| 20 | 641 | 4 | 29 | 29 | 35 | 19–21 | 26 (18–33) | 21% | 22% | 59 (45–74) | 59 | 59 | 6.8 |
| 30 | 640 | 4 | 36 | 36 | 35 | 18–21 | 25 (18–33) | 24% | 23% | 59 (44–73) | 59 | 58 | 5.6 |

Means over seeds; ages and overalls pooled. Over the 30 years each seed retires 1,028–1,060 players and takes in the same number of youths (two seeds one more); the active population stays at 640–643, the mean age at 25–26 and the mean overall at 57–60.

**The population is stable.** Retirements equal youth intake every year (the academy replaces each retiree at his position), so the population, the age profile and the mean overall hold for 30 years. Squads are 18–21 at the contract-year end: with the free-agent reserve some AI clubs sit a player or two under the roster of 20 until the window (see [Free agents](#free-agents)).

**Generation is out of equilibrium for its first decade.** Generated players draw their attributes independently of their age, so at year 0 a 17-year-old is as good as a 27-year-old (overall 55–62 in every age band; see [by age band](#attributes)). Development then grows the generated teenagers by 4 a year and shrinks the veterans: the overall spread widens from 49–70 (p10–p90) to 43–75 by year 10 and settles at 44–74 once the generated players have retired. In the settled world a 16–20-year-old rates about 11 below a player at his peak. Filed to `data` as [data--generated-age-curve](handoffs/data--generated-age-curve.md).

**The second divisions are as strong as the first, for good.** The divisions are generated equally strong (60 against 59), and 30 years of promotion and relegation never separate them: the gap between the divisions' mean squad averages stays within −2..+3 in every seed and year, and on average 4–7 of a seed's 16 second-division clubs rate above the first division's median club at every checkpoint. Nothing sustains a gap: youth intake and gate receipts are the same in every division, and the first division earns only its cup gates ([Money by division](#money-by-division)). Filed to `data` as [data--weaker-lower-divisions](handoffs/data--weaker-lower-divisions.md).

### Rerun at generation 9 (age-adjusted generation)

Measured 2026-10-02 at `worldgen.Version` 9, `worldgen.YouthVersion` 4, `players.DevelopmentVersion` 1, `content.Version` 9, `LeagueVersion` 7, `ScheduleVersion` 4, `ai.TransfersVersion` 7, `ai.ContractsVersion` 2, `medical.Version` 4, `simple` v6; same seeds and command. Answers `balance--generated-age-curve-delivered`. The table above is generation 8 (historical).

| | year 0 | year 1 | year 3 | year 10 | year 20 | year 30 |
| --- | --- | --- | --- | --- | --- | --- |
| Active players (min–max over seeds) | 640 | 640 | 640 | 640 | 640 | 640 (640–642 over the career) |
| Age mean (p10–p90) | 25 (18–33) | 26 (19–33) | 26 (18–33) | 25 (18–33) | 26 (18–33) | 25 (18–33) |
| Overall mean (p10–p90) | 59 (45–73) | 59 (44–73) | 59 (44–73) | 58 (44–73) | 59 (45–73) | 59 (44–73) |
| Division 1 / division 2 mean | 60 / 59 | 59 / 59 | 59 / 58 | 59 / 58 | 59 / 59 | 59 / 58 |

Overall mean by age band (≤20 / 21–25 / 26–30 / ≥31) at generation against year 30, 10 seeds pooled:

| | generation | year 30 |
| --- | --- | --- |
| GK | 49 / 58 / 60 / 52 | 47 / 58 / 59 / 52 |
| DF | 53 / 62 / 63 / 53 | 53 / 63 / 63 / 52 |
| MF | 56 / 66 / 66 / 55 | 56 / 66 / 66 / 55 |
| FW | 55 / 64 / 65 / 56 | 54 / 64 / 65 / 56 |

**The first-decade transient is gone.** Every age band at generation is within 2 of year 30 (largest: goalkeepers aged 20 or under, 49 against 47), the spread is 45–73 at generation and 44–73 at year 30 (it was 49–70 widening to 43–75), the overall mean holds at 58–59 and the share aged ≤20 is 20–25% throughout. That is inside `data`'s own thresholds (bands within 3, spread within 2), so no note is filed. 1,028–1,060 players retire per seed and the same number of youths arrive; the population stays 640–642. The settled world agrees with the initial one, as `data` intended.

**The lasting division gap is still absent.** Mean squad average by division: 59 / 58 or 59 at every checkpoint, gap −1..+3 per seed-year, 4.1–6.3 of 16 second-division clubs above the first division's median. Generation 9 does not touch it (`data--weaker-lower-divisions` is still open, now inside `data--division-economy-design`).

### History

- **2026-10-01**: this section (the population half of the baseline), first measured at `f2e6b75` (`medical.Version` 3) and restated at `37a4f99` (`medical.Version` 4): population and attributes within a point, the division gap's range −2..+4 then, and money by division within the seed noise noted there.
- **2026-10-02**, generation 9: the rerun above.

## Attributes

The five attributes added at `content.Version` 7 (dribbling, heading, strength, acceleration, positioning), with the first six for comparison, per position, from the [Population](#population) sweep (10 seeds pooled). Answers `balance--match-attribute-spreads`.

Mean (p10–p90) at generation and after 30 years (10 and 20 years lie between and are in the test's log):

| | year | goalkeeping | defending | passing | finishing | pace | stamina | dribbling | heading | strength | acceleration | positioning | overall |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| GK | 0 | 69 (52–86) | 24 (13–35) | 40 (26–54) | 9 (2–16) | 30 (14–45) | 48 (31–65) | 9 (2–16) | 21 (9–34) | 48 (31–65) | 29 (14–45) | 39 (26–54) | 55 (44–66) |
| GK | 30 | 69 (50–88) | 25 (11–38) | 40 (22–58) | 15 (4–25) | 29 (12–46) | 46 (26–66) | 15 (4–24) | 25 (11–38) | 49 (29–68) | 30 (12–47) | 41 (23–58) | 54 (39–69) |
| DF | 0 | 6 (2–10) | 67 (47–86) | 43 (26–60) | 21 (8–34) | 50 (31–70) | 60 (41–79) | 30 (15–45) | 64 (47–80) | 61 (41–80) | 50 (31–70) | 67 (47–85) | 59 (49–69) |
| DF | 30 | 14 (3–23) | 66 (45–87) | 43 (23–62) | 25 (12–39) | 49 (27–71) | 59 (37–81) | 31 (14–47) | 63 (44–82) | 61 (41–82) | 50 (28–72) | 67 (46–88) | 58 (43–73) |
| MF | 0 | 6 (2–10) | 44 (26–60) | 66 (47–85) | 43 (26–60) | 53 (36–70) | 67 (47–86) | 50 (31–69) | 37 (21–54) | 45 (26–64) | 53 (36–70) | 53 (36–70) | 62 (52–72) |
| MF | 30 | 13 (3–23) | 43 (23–62) | 66 (45–86) | 42 (23–62) | 51 (31–71) | 65 (43–86) | 50 (29–71) | 37 (18–55) | 46 (25–67) | 52 (32–71) | 53 (34–73) | 61 (46–75) |
| FW | 0 | 6 (2–11) | 22 (9–35) | 48 (30–65) | 67 (47–86) | 66 (47–86) | 54 (36–70) | 60 (42–79) | 51 (31–70) | 51 (31–70) | 67 (47–86) | 60 (41–80) | 60 (50–70) |
| FW | 30 | 14 (3–23) | 25 (12–39) | 48 (29–68) | 66 (46–87) | 65 (44–86) | 52 (33–71) | 61 (40–81) | 50 (29–71) | 52 (30–73) | 65 (44–87) | 62 (42–83) | 60 (45–73) |

Means by age band after 30 years (the settled world):

| | ages | dribbling | heading | strength | acceleration | positioning | overall |
| --- | --- | --- | --- | --- | --- | --- | --- |
| DF | 21–25 | 35 | 67 | 63 | 55 | 71 | 63 |
| DF | 26–30 | 35 | 67 | 65 | 55 | 71 | 63 |
| DF | 31–36 | 28 | 61 | 60 | 42 | 67 | 52 |
| MF | 26–30 | 56 | 42 | 50 | 57 | 59 | 66 |
| MF | 31–36 | 47 | 35 | 46 | 46 | 52 | 55 |
| FW | 26–30 | 65 | 54 | 56 | 70 | 66 | 64 |
| FW | 31–36 | 59 | 47 | 51 | 59 | 62 | 55 |

**The five new attributes hold their level.** Over 30 years each outfield position's mean dribbling, heading, strength, acceleration and positioning stays within 2 of its generated mean, and the p10–p90 spreads widen by 2–5 points, as the first six do. The youth gap and the growth rules keep them level. The exceptions are the attributes generated near the floor: a goalkeeper's dribbling and heading (below).

**Nothing implausible in the order.** Goalkeepers never out-head defenders (25 against 63) and stay well below them in strength (49 against 61). Positioning does not run away: it matches defending at a defender's peak (71 each), as at generation (67 each), and its slower decline from 29 to 32 leaves a 31–36-year-old defender 4 points down on his peak against 9 for defending and 13 for acceleration, which reads as experience. It is ahead of the key attributes only for veterans, and it is not in `Overall`.

**The lowest attributes creep up and out of their ranges.** Outfield goalkeeping rises from 6 (generated range 1–11) to 13–14 with a p90 of 23; a goalkeeper's finishing and dribbling from 9 (range 1–17) to 15 with a p90 of 25; other low attributes by 3–4 (a goalkeeper's heading 21 to 25, a defender's finishing and a forward's defending 21–22 to 25). The youth range is each position's range lowered by `Youth.RatingGap` (13) but never below 1, so a 1–11 range becomes 1–1 instead of the −12..−2 the gap intends. Development then adds the same growth to it as to every attribute (about 24 points from 16 to 24), so these attributes settle up to 8 points above their generated level. Harmless while no outfield player keeps goal and no goalkeeper shoots, but it is an unintended drift in generated facts the tick engine may read. Filed to `data` as [data--youth-floor-attributes](handoffs/data--youth-floor-attributes.md).

### Rerun at generation 9

Measured 2026-10-02 (same run as the [Population](#population) rerun). Outfield goalkeeping now starts at 14 (generation 8: 6), a goalkeeper's finishing and dribbling at 16 (9), and year 30 gives 14 (13–14), 15 and 15: **the drift of the lowest attributes has disappeared in the measured output, because generation now starts them at their settled level.** Other attributes hold within 2 of generation at every position and year (for example a defender's heading 64 / 64 / 64 at years 0 / 20 / 30; a forward's dribbling 60 / 61 / 61). The cause the earlier note names (a youth range clamped at 1 and development adding growth to the floor) still exists in the rules and nobody has changed it, so a changed youth or development rule can bring the drift back: `data--youth-floor-attributes` stays open for `data` and `squad` to decide, but nothing is visibly wrong now. I add the always-on attribute bound once they answer, as planned.

### History

- **2026-10-01**: this section.
- **2026-10-02**, generation 9: the rerun above.

## Money by division

From the [Population](#population) sweep: AI-only, 10 seeds, 30 years, each club counted in the division it played that year (16 clubs a division a seed). Thousands of whole units, at each contract-year end, over the contract year just ended. There are no other postings yet: the cup prize table exists in content (`content.LeagueVersion` 6) but nothing pays it (`squad--cup-prize-postings`). The sweep checks every club's balance change against its gate, wages, fees and payoffs every year.

| year | div | balance mean (min–max) | gate | wages | gate − wages | net fees | below 0 | below 500k | balance fell | gate < wages |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 1 | 2,194 (1,143–4,872) | 1,922 | 1,683 | +238 | −43 | 0 | 0 | 81 of 160 | 17 |
| 1 | 2 | 2,140 (898–4,228) | 1,750 | 1,653 | +96 | +44 | 0 | 0 | 76 | 32 |
| 5 | 1 | 3,045 (878–12,360) | 1,922 | 1,601 | +320 | +23 | 0 | 0 | 66 | 7 |
| 5 | 2 | 3,139 (880–13,692) | 1,750 | 1,562 | +187 | −22 | 0 | 0 | 81 | 6 |
| 10 | 1 | 3,656 (719–17,890) | 1,922 | 1,669 | +252 | +72 | 0 | 0 | 69 | 26 |
| 10 | 2 | 4,713 (816–18,381) | 1,750 | 1,594 | +155 | −71 | 0 | 0 | 89 | 32 |
| 20 | 1 | 5,878 (695–20,865) | 1,922 | 1,664 | +257 | +120 | 0 | 0 | 83 | 23 |
| 20 | 2 | 5,775 (806–22,259) | 1,750 | 1,643 | +107 | −119 | 0 | 0 | 90 | 40 |
| 30 | 1 | 6,994 (707–28,689) | 1,922 | 1,660 | +261 | −28 | 0 | 0 | 82 | 21 |
| 30 | 2 | 8,461 (1,006–29,784) | 1,750 | 1,606 | +143 | +29 | 0 | 0 | 86 | 28 |

**Money only grows.** An average club's gate exceeds its wage bill (by 230–320k a year in the first division, 95–190k in the second), and there is no other cost, so the mean balance climbs from 2.0M to 4.2M at year 10 and 7.7M at year 30: about 180k a club a year. The richest club ends on 16–30M per seed. The poorest club per seed has 0.7–1.0M at year 10 and 0.7–1.8M at year 30, consistent with AI clubs keeping a wage reserve (`ai.ReserveWeeks` 26): in 320 club-careers no club ever went below zero or below 500k at a contract-year end, and only 0–2 clubs per seed saw their balance fall in two years of three. 40–58% of the clubs' balances fall in any one year (transfer fees), and 4–32% of clubs have a wage bill above their gate, but none trends towards insolvency.

**The divisions earn almost the same.** Gate receipts are a flat 250k a home match, so the only difference is the first division's Continental Cup home matches: 1,922k against 1,750k a year. After the first three years the division a club played in says little about its balance: the second division's mean is as often above the first's as below (8.5M against 7.0M at year 30; 4.7M against 3.7M at year 10; 5.8M against 5.9M at year 20). Clubs move between divisions, so the split by division is noisier than the whole: rows move by up to 0.8M between the `medical.Version` 3 and 4 runs while the overall mean holds at 7.7M. The cup prize table, once paid, adds 3.1M a year to the first division's eight cup clubs. Filed to `squad` as [squad--money-only-grows](handoffs/squad--money-only-grows.md).

### Rerun with cup prizes posted (schema 32, `LeagueVersion` 7)

Measured 2026-10-02 at schema 32 (the Continental Cup's awards are posted, 3.1M an edition, `finance.KindPrize`), `LeagueVersion` 7, `worldgen.Version` 9, `ai.TransfersVersion` 7, `medical.Version` 4, `simple` v6; same seeds and command. Answers `balance--cup-prize-accounting`. The table above (no prizes, `LeagueVersion` 6) is historical. The test's "other" column is the prize income, 194k a first-division club a year (3.1M over the 16 first-division clubs); a new "inc-loss" column counts club-seasons whose gate plus prizes do not cover wages. Thousands of whole units, 10 seeds pooled (160 club-seasons a division and year):

| year | div | balance mean (min–max) | gate | prizes | wages | gate + prizes − wages | gate < wages | gate + prizes < wages |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 1 | 2,216 (812–5,549) | 1,813 | 0 | 1,691 | +121 | 40 | 40 |
| 1 | 2 | 1,984 (837–5,776) | 1,750 | 0 | 1,670 | +79 | 40 | 40 |
| 2 | 1 | 2,509 (835–7,154) | 1,922 | 194 | 1,687 | +428 | 31 | 22 |
| 2 | 2 | 2,224 (856–9,610) | 1,750 | 0 | 1,645 | +104 | 32 | 32 |
| 10 | 1 | 4,724 (810–18,155) | 1,922 | 194 | 1,650 | +466 | 26 | 18 |
| 10 | 2 | 4,743 (797–19,841) | 1,750 | 0 | 1,592 | +157 | 23 | 23 |
| 20 | 1 | 7,674 (944–25,007) | 1,922 | 194 | 1,672 | +444 | 26 | 20 |
| 20 | 2 | 7,783 (724–23,936) | 1,750 | 0 | 1,630 | +119 | 37 | 37 |
| 30 | 1 | 10,478 (660–32,421) | 1,922 | 194 | 1,654 | +462 | 26 | 21 |
| 30 | 2 | 10,722 (720–30,332) | 1,750 | 0 | 1,612 | +138 | 38 | 38 |

**Year 1 has no cup income and so no first-division advantage**: the first division's gate is 1,813k (no Continental Cup home matches in season 1, since a cup edition is drawn from season N and played in season N+1), prizes are first paid in year 2 (by design), and the first division's surplus rises from +121k to +428–466k after.

**Prizes widen the surplus, as `squad` expected.** The mean balance is 10.5M (division 1) and 10.7M (division 2) at year 30 against 7.0M and 8.5M without prizes; the mean over both divisions rises about 280k a club a year (180k before). A first-division club's yearly income (gate and prizes) is 2,116k against 1,750k in the second division, **21% more**, meeting the "at least 20%" in `squad--money-only-grows`; but the balance by division still says nothing about the division played in: a promoted or relegated club carries its money with it, and the second division's mean is level with the first's at years 10, 20 and 30. The prize is paid to the cup clubs, not to the division.

Against the targets in `squad--money-only-grows` at year 30:

| Target | Measured |
| --- | --- |
| Median balance within ±50% of the opening 2.0M | AI-only median 9.6M (4.8×): **not met** |
| Richest club below about 5× the opening balance | mean richest 27.7M (13.9×), per seed 23–32M: **not met** |
| First-division club earns at least 20% more | 21% with prizes, 4% (gate 1,813 against 1,750) in year 1: **met after year 1** |
| 5–15% of club-seasons lose money without going broke | gate + prizes below wages in 7–14% of first-division club-seasons after year 1 (25% in year 1) and 11–25% of second-division ones; no club below zero; **one** of 320 club-years below 500k (269k, year 25, first division): **met in division 1, above in division 2** |

**Budget pressure and recruitment.** Prizes do not create money pressure anywhere: the first-division surplus is +430–470k a club a year and the second's +100–160k. 11–25% of second-division club-seasons have gate below wages (7–14% of first-division ones with prizes) and none goes below zero; the AI keeps a 26-week wage reserve (`ai.ReserveWeeks`). The AI market's completions per window (48) did not move with the extra money, so prizes have no visible effect on recruitment. Whether the AI should spend more of a growing surplus is `squad`'s economy decision, not something this sweep measures.

### History

- **2026-10-01**: this section (the money half of the baseline), restated at `medical.Version` 4 (commit `37a4f99`); at v3 (`f2e6b75`) the same conclusions, with the divisions at 7.7M each at year 30.
- **2026-10-02**, schema 32, `LeagueVersion` 7: the prize rerun above.
