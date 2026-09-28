---
to: match
from: balance
status: open
blocking: no
created: 2026-09-28
---

# Penalty shootouts favour the stronger side far too much

## Why
Real shootouts are close to a coin toss, even between unequal teams: that is what makes them the underdog's chance. In both engines, a shootout follows the rating gap about as strongly as open play does. This is live in the career now, through `simple` in the Continental Cup.

## What exists
Measured at `simple.ModelVersion` 3 and `tick.ModelVersion` 1: 3,000 knockout matches per row, seeds 1, 42 and 2026 × fixtures 1–1000. See [docs/balance.md, "Shootouts"](../balance.md#shootouts).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1
```

| Stronger side (home) wins the shootout, % | 60 v 60 | 62 v 58 | 65 v 55 | 70 v 50 |
| --- | --- | --- | --- | --- |
| simple | 49 | 59 | 74 | 88 |
| tick | 53 | 65 | 80 | 90 |

A side 10 points weaker (65 v 55, the widest gap between career squads) reaches penalties 16–24% of the time and then wins only 20–26% of them. Both engines use the same rule: `ShootoutConversionPPM` × (taker Finishing + offset) / (keeper Goalkeeping + offset), clamped to `MinShootoutPPM` 50% to `MaxShootoutPPM` 93% (`shootout` in [simple/model.go](../../internal/matches/simple/model.go) and [tick/play.go](../../internal/matches/tick/play.go)). The stronger side has both the better takers and the better keeper, so the two effects multiply, and a range that wide lets the rating gap dominate.

## What is needed
Narrow how much rating moves shootout conversion in both engines, and bump both `ModelVersion`s. Real conversion is about 70–80% for most takers.

## Done when
In the sweep above, the stronger side wins at most about 60% of shootouts at 65 v 55 and at most about 65% at 70 v 50, in both engines.
