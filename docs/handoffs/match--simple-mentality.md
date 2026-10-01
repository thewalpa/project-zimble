---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# In simple, the career default, attacking always pays and defensive never does

## Why
`tick` v7 made mentality a trade-off. `simple` v5 plays every career fixture by default, and there a manager should still always attack and never defend. The effect is small, but it is the only tactical choice a manager has.

## What exists
[docs/balance.md, "Mentality"](../balance.md#mentality), 3,000 matches a row (`TestBalanceEngineComparison`, `TestBalanceMentalityByGap`), points per match for the side that changes mentality, Δ against balanced:

| Side | simple attacking Δ | simple defensive Δ | tick v7 attacking / defensive Δ |
| --- | --- | --- | --- |
| Equal, 60 v 60 at home | +0.07 | −0.09 | +0.00 / +0.02 |
| Equal, 60 v 60 away | +0.03 | −0.03 | +0.03 / −0.01 |
| Underdog, 55 v 65 at home | +0.05 | −0.05 | +0.00 / +0.07 |
| Favourite, 65 v 55 at home | +0.08 | −0.13 | +0.04 / −0.01 |
| Underdog, 65 v 55 away | +0.02 | +0.00 | −0.04 / +0.04 |

- Attacking in `simple` adds 0.21–0.37 goals scored and 0.16–0.28 conceded (`MentalityOwnPermille` 1200 against `MentalityConcedePermille` 1150 in [params.go](../../internal/matches/simple/params.go)); the points come out ahead in every row.
- Each row alone is one to two sampling margins (±0.05 points a match), but all ten point the same way. Over a 14-match league season the gap is 0.3–1.1 points.

## What is needed
Your call whether `simple` should carry the trade-off too. If it should, the `tick` v7 shape is the target: attacking within about ±0.05 of balanced at equal teams, and defensive at least level with balanced for the underdog. Bump `simple.ModelVersion` and the seeded goldens.

If `simple` stays the cheap, coarse engine on purpose, decline this with that reason. I'll then record it in `docs/balance.md` as a known property of the default engine.

## Done when
`ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run 'TestBalanceEngineComparison|TestBalanceMentalityByGap' -v -count=1 -timeout 2h` shows `simple`'s attacking Δ within ±0.05 at 60 v 60 and defensive Δ ≥ 0 for the 55 v 65 home underdog. Or the note is declined.
