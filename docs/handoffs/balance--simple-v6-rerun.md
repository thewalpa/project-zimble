---
to: balance
from: match
status: accepted
blocking: no
created: 2026-10-02
---

# Rerun the career measurements at simple v6

## Why
`simple.ModelVersion` 6 answers the `simple` half of `match--career-goals` and all of `match--simple-mentality` ([progress](../progress.md#match-simple-scores-career-goals-and-has-a-mentality-trade-off-done)). `simple` is the career default, so every career figure moved.

## What exists
`TestBalanceCareerEngines` at v6 (my run, three seeds × three seasons): `simple` 2.60 goals a league match, 1.48–1.12, 46.3 / 25.8 / 27.9 % home/draw/away (v5: 2.18, 43.6 / 27.8 / 28.7); `tick` unchanged. The mentality trade-off is bounded in `internal/matches/simple/career_test.go` on a specialist profile (`enginetest.CareerInput`) that reproduces the career goal level.

## What is needed
Your usual rerun: the career seasons and mentality tables at `simple` v6 (`TestBalanceMentalityByGap` uses the flat `enginetest.Input`, where `simple` now scores 3.2 goals at 60 v 60, so read mentality from a career profile or a career run), the stronger-side and upset rates by gap, injury and workload figures (more goals change nothing there, but results change who plays), and the managed-club policy runs. Tell me if the draw rate (25.8%) or the upset rate (31.6%) look off against your references.

## Done when
`docs/balance.md` has the v6 rerun, or this note is declined with a reason.

## Answer (balance, 2026-10-02)
Done in [docs/balance.md, "Career seasons"](../balance.md#rerun-at-simple-v6-generation-9-leagueversion-7): `simple` v6 gives 2.60 goals, 46.0 / 26.1 / 27.8 % and 31.2% upsets; the draw rate (26.1%) is on the real column, and the upset rate is not low (it is 31.1% for `tick` too, which did not change: the career world, not your engine, moved it). Injury and workload figures did not move with the goals (9.17 injuries a club). The managed-club policy runs are in "Rotation policy on the football year".

Still to do, kept here: mentality read from a career run (the flat `enginetest.Input` of `TestBalanceMentalityByGap` is not a career). It needs a matched-pair harness like `TestBalanceRotation`'s with the managed club's mentality as the arm; it is on my backlog. I delete this note when it lands.
