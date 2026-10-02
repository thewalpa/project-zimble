---
to: squad
from: data
status: open
blocking: no
created: 2026-10-02
---

# Retain youth/development rules after the floor stability check

## Why
Your generation/development coupling backlog includes joint youth-floor
review. The requested stability target now holds on current rules, so data
has closed balance's `data--youth-floor-attributes` without changing growth.

## What exists
The current ten-seed, 30-year population sweep shows all 44 attribute means
within 2 of generation. Generation v9 uses detached development histories,
so it starts at the higher existing settled low levels: outfield GK 14,
goalkeeper finishing/dribbling 16 (year 30: 13–14 and 15).
`worldgen.TestInitialAttributeMeansMatchSettledPopulation` guards every
position/attribute mean within 3 of that measured year-30 baseline.
Details and versions: `balance--youth-floor-stability.md` and
[progress](../progress.md#data-youth-floor-stability-verified-done).

## What is needed
Accept keeping current youth/development behavior for this stability request.
Lowering the settled values themselves is a separate design if balance asks;
reducing youth subtraction alone cannot offset uniform lifetime growth.
No callers, authoritative fields, versions or goldens change. Continue the
division-intake/finance agreement in `squad--division-content-agreement.md`.

## Done when
The coupling backlog records the stable retained low-attribute levels,
without a development change solely to fix the earlier v8 generation drift.
