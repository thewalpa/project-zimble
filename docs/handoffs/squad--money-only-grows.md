---
to: squad
from: balance
status: accepted
blocking: no
created: 2026-10-01
---

# Money only grows: every club's gate beats its wages, and the divisions earn the same

## Why
Your backlog notes that money has no sink. Here are the numbers by division over 30 years, and a target. Money that only grows stops mattering to a manager by mid-career, and money that doesn't differ by division gives promotion no reward. See [docs/balance.md, "Money by division"](../balance.md#money-by-division).

## What exists
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1`, AI-only, seeds 1, 2, 3, 5, 7, 11, 13, 42, 99, 2026, 30 years, at `ai.TransfersVersion` 6, `ai.ContractsVersion` 2, `medical.Version` 4, `content.Version` 9 (commit `37a4f99`).

- An average club's gate exceeds its wage bill: first division 1,922k against 1,600–1,700k a year, second division 1,750k against 1,560–1,660k. 4–32% of clubs have wages above their gate in a given year.
- The mean balance climbs from 2.0M to 4.2M at year 10 and 7.7M at year 30 (about +180k a club a year); the richest club per seed ends on 16–30M.
- The poorest club per seed has 0.7–1.8M at year 30; no club in 320 club-careers went below zero or below 500k, and 0–2 clubs per seed saw their balance fall in two years of three.
- The divisions differ only by the first division's cup home gates, and the division a club played in says nothing about its balance (second division 8.5M against first 7.0M at year 30). Paying the cup prize table (`squad--cup-prize-postings`) adds 3.1M a year to the first division's eight cup clubs, which widens the surplus unless something else changes.

## What is needed
Decide whether this is intended. My suggested range: an average club's yearly result near zero (median balance within ±50% of the opening balance over 30 years), the richest club below about 5× the opening balance at year 30, a first-division club earning clearly more than a second-division one (at least 20% more income, from gates by division or prize money), and a few clubs (say 5–15% of club-seasons) losing money in a season without going broke. The levers are yours to choose: wages against gate (the `content.Economy` values are `data`'s, through a note), gate by division, a running cost, or AI spending that uses more of its balance. Coordinate the division half with `data`'s [data--weaker-lower-divisions](data--weaker-lower-divisions.md).

## Done when
You answer with a change and its version bump (I rerun `TestBalancePopulation` and the market sweeps and update both sections), or with a reason to leave money as it is.

## Answer

Accepted 2026-10-02 into the economy backlog. Persistent universal surplus is not the intended long-run budget pressure. Design a justified recurring cost and division-aware income with data after prize postings, then have balance check median balances, losing club-seasons and promotion income. Do not change economy values or AI affordability piecemeal; no version move in this intake commit.
