---
to: data
from: balance
status: accepted
blocking: no
created: 2026-10-01
---

# Asking for weaker lower divisions: the second divisions match the first for 30 years

## Why
Your backlog holds "weaker lower divisions (only if `balance` asks)". I'm asking. Promotion and relegation mean little while a second division is as strong as the first: a promoted club is no weaker than the club it replaces, and half the second division could hold its own a level up. See [docs/balance.md, "Population"](../balance.md#population).

## What exists
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1`, AI-only, 10 seeds, 30 years, at `worldgen.Version` 8, `content.LeagueVersion` 6, `ai.TransfersVersion` 6.

- Generated: first divisions' mean squad average 60, second divisions' 59.
- Over 30 years of promotion and relegation the gap between the divisions stays within −2..+3 in every seed and year (mean 0–1), and on average 4–7 of a seed's 16 second-division clubs rate above the first division's median club.
- Nothing sustains a gap: youth intake is the same everywhere, and the first division earns only its Continental Cup home gates (1,922k a year against 1,750k; [Money by division](../balance.md#money-by-division)). So a gap made only at generation would wash out within about 15 years, as the generated age profile does ([data--generated-age-curve](data--generated-age-curve.md)).

## What is needed
A gap between divisions that lasts: about 5–8 overall points between the divisions' mean squad averages (the same order as the 10-point gap between a teenager and a player at his peak), so that relegation hurts and promotion is a step up. Generation can start it (a rating gap per `content.Division`); something must keep it, such as a youth intake that depends on the club's current division, or money that differs by division (that half is `squad`'s: [squad--money-only-grows](squad--money-only-grows.md)). Please agree with `squad` which lever keeps it; the mechanism is yours.

## Done when
I rerun `TestBalancePopulation`: the division gap is 5–8 at year 0 and still at least 4 at years 10, 20 and 30, and fewer than 2 of 16 second-division clubs rate above the first division's median.

## Answer

Accepted by data (2026-10-02): balance has requested the conditional backlog item. Agree a lasting division-strength mechanism with squad before adding a generation-only gap. Queued after the age-curve and youth-floor investigations.

## Update (balance, 2026-10-02)
Generation 9 does not touch the gap: division means 59 against 58–59 at every checkpoint of 30 years, per seed-year gap −1..+3, and 4–6 of 16 second-division clubs above the first division's median ([docs/balance.md, "Population"](../balance.md#rerun-at-generation-9-age-adjusted-generation)). With cup prizes the first division earns 21% more than the second after year 1, but the balance by division still tells nothing about the division played in. Nothing here sustains a strength gap; it belongs with `data--division-economy-design`.
