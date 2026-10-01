---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Tick's passes, tackles and possession spread are off against real football

## Why
`balance--tick-match-stats` asked for the career and synthetic comparison against real top-division figures. It is in [docs/balance.md, "Match statistics"](../balance.md#match-statistics-tick-against-real-football) (3,000 synthetic matches a row at `tick` v6, plus the 2,016 career league matches of `TestBalanceCareerEngines`). The career rows match the synthetic profile almost exactly — real squads change goals and results, not these statistics — so the synthetic targets are the right place to tune.

## What exists
Per side, against the note's targets:

| Stat | Target | tick 60 v 60 | tick 70 v 50 (stronger / weaker) |
| --- | --- | --- | --- |
| Passes | 400–600 | 954 / 924 | 1045 / 823 |
| Tackles | 15–20 | 45.7 / 43.4 | 52.2 / 34.7 |
| Possession, stronger side | 60–65 % at a clear gap | 50.9 % even | 56.6 % at gap 20 |
| Shots | 12–13 | 10.8 / 9.5 | 16.4 / 6.3 |

- **Passes 1.7–2.5 times the target** and **tackles 2–3 times**. Completion is right (77–81 %) and offsides are right (1.3–2.0; the mentality rows reproduce your own v6 figures).
- **Possession does not separate enough**: 56.6 % at gap 20 and 53.8 % at gap 10, against the 60–65 % target. Equal teams are fine at 51/49.
- **Shots are a little low between equal teams** (20–21 a match against the real 24–26) **and too lopsided at a gap** (16.4–6.3 at gap 20; real football at gap 10 is about 12.6–9.3). Saves and on-target track the shots.
- An attacking mentality buys shots and offsides but no possession (the attacking home side has 49.9 % against a balanced one) — worth a look alongside `match--attacking-is-free`, since possession is half of what a manager sees.

## What is needed
Scale the pass and tackle event rates toward 400–600 and 15–20, widen the possession handicap so gap 10 reaches 60–65 %, and level the shot split at a gap (more for the weaker side, fewer for the stronger). Tune, bump `tick.ModelVersion`, and I rerun `TestBalanceMatchStats` and `TestBalanceCareerEngines` and refresh the section.

## Done when
The rerun shows passes 400–600, tackles 15–20, possession 60–65 % for a clearly stronger side and shots 12–13 a side at 60 v 60 — or a stated reason for the levels you keep.
