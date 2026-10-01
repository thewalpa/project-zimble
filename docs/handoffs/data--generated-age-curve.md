---
to: data
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Generated players have no age curve, so the first decade is a transient

## Why
A new career should start in the world it settles into. Today the generated world doesn't: its teenagers are as good as its 27-year-olds, and the first ten years are spent correcting that. See [docs/balance.md, "Population"](../balance.md#population) and ["Attributes"](../balance.md#attributes).

## What exists
`ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1`, AI-only, seeds 1, 2, 3, 5, 7, 11, 13, 42, 99, 2026, 30 years, at `worldgen.Version` 8 (commit `37a4f99`).

- At year 0, attribute means are the same in every age band: overall 55 (GK), 59 (DF), 62 (MF), 60 (FW) whether a player is 16–20, 21–25, 26–30 or 31–36. `worldgen` draws the birth date independently of the attributes.
- In the settled world (year 30) a 16–20-year-old rates about 11 below a 21–30-year-old (DF 52 against 63, MF 55 against 66, FW 54 against 64, GK 48 against 59), and a 31–36-year-old about 9 below too.
- So development grows the generated teenagers into stars and shrinks the generated veterans: the overall spread (p10–p90) goes from 49–70 at year 0 to 42–73 at year 5 and 43–75 at year 10, then settles at 44–74. The mean holds at 58–60 throughout.

## What is needed
Generate attributes that fit the player's age, so year 0 looks like year 30: for example draw the ranges as the settled profile at the player's age (lower for the young and the old by roughly what `players.Growth` adds or removes), keeping the overall mean where it is. A `worldgen.Version` bump.

## Done when
I rerun `TestBalancePopulation`: at year 0 the age bands differ as they do at year 30 (within about 3 points), and the overall p10–p90 at year 0 is within 2 of year 30's.
