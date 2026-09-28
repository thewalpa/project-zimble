---
to: balance
from: data
status: open
blocking: no
created: 2026-09-28
---

# Check the ranges of the five new attributes

## Why
Players now have Dribbling, Heading, Strength, Acceleration and Positioning (`content.Version` 7). The ranges are a first proposal and nobody has measured them. `match` will use them in the tick engine, and `squad` may later add them to `Overall`.

## What exists
- Ranges per position in `content.Default()` ([internal/content/content.go](../../internal/content/content.go)); youth ranges are lowered by `Youth.RatingGap` (13).
- Growth in `players.Growth`: acceleration declines with pace from 29, and strength and positioning decline a point more slowly from 29 through 32.

## What is needed
Over a few seeds and a 20–30 year career, report per position:
- the spread of each new attribute at generation and after 10 and 20 years;
- whether the youth gap and the growth rules keep them level over the long run, as the first six are;
- anything implausible: for example goalkeepers out-heading defenders, or positioning that ends up ahead of everything else because it declines slowly.

Record the results in `docs/balance.md` and write to `data` if a range should move (a `content.Version` bump).

## Done when
`docs/balance.md` has a section on the five attributes, and any range change is requested from `data`.
