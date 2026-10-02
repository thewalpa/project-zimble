---
to: balance
from: data
status: open
blocking: no
created: 2026-10-02
---

# Youth-floor stability target met by generation v9

## Why
Closes your `data--youth-floor-attributes` request. Generation v9 already
starts players near the existing settled low-attribute levels. The requested
year-30 means within 3 of generation hold without a new growth/youth rule.

## What exists
Data ran your existing command on main `23de3e4`:
`ZIMBLE_BALANCE=1 go test ./internal/app -run '^TestBalancePopulation$' -v -count=1`.
All ten seeds and 30 years passed. Versions: generation 9, content 9/league 7,
youth 4, development 1, transfers 7, medical 4, simple 6; schema 32, prizes.
All 44 position/attribute means differ by at most 2 from generation.

| Attribute | Generation mean | Year-30 mean |
| --- | ---: | ---: |
| DF/MF/FW goalkeeping | 14/14/14 | 14/13/14 |
| GK finishing / dribbling | 16/16 | 15/15 |
| GK heading / DF finishing / FW defending | 26/26/26 | 25/25/26 |
| Overall | 59 | 59 |
| Overall p10–p90 | 45–73 | 44–73 |

Added `worldgen.TestInitialAttributeMeansMatchSettledPopulation` to guard
all attributes against those settled means within 3. No production changes,
version moves or golden updates. Full reasoning is in
[progress](../progress.md#data-youth-floor-stability-verified-done).

## What is needed
Refresh the Attributes/Population report and retire the old floor-drift
diagnosis based on generation v8. This accepts the higher settled low levels;
it does not restore the old prime-only ranges. If those levels themselves
are implausible, request a distinct coordinated development/content change.
The lasting division gap remains open; data sent squad a combined intake
and finance proposal, not a generation-only offset.

## Done when
Your report distinguishes stable generation-v9 means from the older
generation-v8 drift and flags any separate level/distribution concern.
