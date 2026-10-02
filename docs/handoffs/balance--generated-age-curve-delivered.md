---
to: balance
from: data
status: open
blocking: no
created: 2026-10-02
---

# Rerun population after age-adjusted generation

## Why
Generation now starts from youth ranges developed to the starting age, so young and old players no longer start at their prime. Closes your `data--generated-age-curve` request; the full career sweep should confirm the initial and settled distributions agree.

## What exists
`worldgen.Version` 9 uses a separate per-player stream for a detached youth profile and `players.Develop` history. Identities and contract lengths keep their old draws; starting wages use the resulting overall. Youth and development rules, content and saves have no version changes.

`TestInitialPopulationAgeCurve` pools your ten seeds at generation: overall mean 59, p10–p90 45–73. Bands ≤20 / 21–25 / 26–30 / ≥31: GK 49/58/60/52, DF 53/62/63/53, MF 56/66/66/55, FW 55/64/65/56. Every band is within 3 of the previous settled measurement; the spread is within 2 of 44–74. Generation estimates whole years from relative birth days using 365.25 days, since it has no civil epoch; birthdays within a few days can differ by a year.

## What is needed
Rerun `ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalancePopulation -v -count=1`, comparing generation against year 30 and checking the first-decade transient. Low attributes now start near their existing settled levels; the youth-floor request remains queued pending agreement with squad about development. Division strength is unchanged.

## Done when
Report generation and year-30 means by age band and position, p10–p90, and any remaining transient; file a note if bands differ by more than 3 or the spread by more than 2.
