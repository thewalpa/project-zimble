---
to: match
from: balance
status: accepted
blocking: no
created: 2026-09-28
---

# Tick: attacking always pays, and defensive backfires

## Why
Before phase 2 puts the manager's live match on `tick`, mentality must be a real choice. Today, one mentality always wins, and another makes things worse.

## What exists
Measured at `tick.ModelVersion` 1: 3,000 matches per row, 60 v 60, seeds 1, 42 and 2026 × fixtures 1–1000. Full tables are in [docs/balance.md, "Mentality"](../balance.md#mentality-60-v-60).

```sh
ZIMBLE_BALANCE=1 go test ./internal/matches/tick -run TestBalance -v -count=1
```

| Home v away | Goals | Home–away goals | H / D / A % |
| --- | --- | --- | --- |
| balanced v balanced | 2.92 | 1.59–1.33 | 44.0 / 24.9 / 31.1 |
| attacking v balanced | 3.61 | 2.41–1.21 | 64.4 / 18.1 / 17.5 |
| balanced v attacking | 3.44 | 1.38–2.07 | 25.3 / 21.2 / 53.5 |
| defensive v balanced | 4.65 | 2.34–2.31 | 40.6 / 18.8 / 40.6 |
| balanced v defensive | 4.74 | 2.64–2.10 | 50.4 / 18.3 / 31.3 |
| defensive v defensive | 1.53 | 0.82–0.71 | 34.5 / 36.9 / 28.6 |

- **Attacking is free:** +20 points of win rate against a balanced side, and the attacking side concedes no more. The AI always plays balanced, so a manager would always attack.
- **Defensive only works when both sides play it.** Against a balanced or attacking opponent, a defensive side concedes more and also scores more (2.34 at home against 1.59 when balanced), and 0–0s fall to 0.7%.

Mentality enters at the line depth (`MentalityDepth`, [play.go:106](../../internal/matches/tick/play.go#L106)), which the attackers' cap `min(d, max(lastLine, ballDepth))` then ties to the opponent's line. It also sets pressers (play.go:145), shot willingness (play.go:274) and pass progress (play.go:340). A mismatch between the two sides' line depths seems the likely place to look, but that is a guess.

`simple` behaves as expected: attacking gives +2–3 points of wins with more goals at both ends, and defensive gives fewer goals, more draws and 2–4 points fewer wins.

Accepted 2026-09-29: next in the match lane after the attribute, shootout and report-event notes, before tick phase 2.

## What is needed
Rework mentality in `tick` so it is a trade-off.
- Attacking scores more and concedes more, for a modest net change against a balanced side: a few points, not 20.
- Defensive concedes fewer, scores fewer and draws more, whatever the opponent plays.
- Bump `tick.ModelVersion`.

## Done when
The sweep above shows, for each of the six rows:
- goals ordered defensive-involved < balanced < attacking-involved;
- the defensive side conceding less than it does when balanced;
- attacking against balanced moving the win rate by at most about 8 points.

A loose version of this could become an always-on bound in `model_test.go`.
