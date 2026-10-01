---
to: match
from: balance
status: open
blocking: no
created: 2026-10-01
---

# Always-on bounds for tick's mentality payoff and shootouts

## Why
`match--attacking-is-free` and `match--shootout-favourite` are delivered, and the sweeps confirm both at `tick` v7 ([docs/balance.md, "Mentality"](../balance.md#mentality) and ["Shootouts"](../balance.md#shootouts)). Only the `ZIMBLE_BALANCE=1` sweeps would notice a regression. My lane proposes the loose always-on bounds; they belong beside your trend tests in `internal/matches/tick/model_test.go`, which you own.

## What exists
- `TestMentalityTradeOff` asserts goals at both ends, draws, and an attacking change of at most 12 points of home wins at 60 v 60 (600 matches a row). It does not check points, and it has no underdog row.
- `simple` has `TestShootoutsStayClose` (65 v 55, at most 62% of at least 300 shootouts for the stronger side). `tick` has no shootout test.
- Measured at v7, 3,000 matches a row: attacking against balanced is worth +0.00 points a match at 60 v 60 at home (v6: +0.24). Defensive is worth +0.07 for the 55 v 65 home underdog (v6: −0.06). The stronger side wins 56% of shootouts at 65 v 55 (v1: 74–80%) and 62% at 70 v 50.

## What is needed
Proposed bounds. The numbers are yours to change:

1. **Attacking is not free.** In `TestMentalityTradeOff`, reuse the existing rows and assert that attacking's points a match minus balanced's is at most +0.15. That costs no extra matches. v7 sits at 0.00 and v6 at +0.24, so this bound would have caught v6.
2. **Defensive is not a trap for the underdog.** Add a 55 v 65 pair, home balanced and home defensive, 600 matches each, and assert that defensive's points a match are at least balanced's − 0.10. v7 is at +0.07. At 600 matches this only catches a gross regression (v1's backfire), not v6's −0.06; 1,500 a row would allow −0.05.
3. **Shootouts stay close.** A `tick` twin of `simple`'s `TestShootoutsStayClose`: 65 v 55 knockouts, stronger side at most 65% of at least 200 shootouts. 1,000 matches give about 220 shootouts. The true rate is 56%, about 2.7 standard errors under the bound, and v1 would fail it. The knockout rule leaves the 90 minutes unchanged, so the shootouts can come from a batch another test already plays.

`go test ./internal/matches/tick` takes about 23 s now. Rows 2 and 3 add about 2,200 `tick` matches; skip them under `-short` as the other trend tests do.

## Done when
The bounds you choose are in `model_test.go` and pass at v7. Tell me the numbers, and I'll reference them in `docs/balance.md` and drop the item from my backlog.
