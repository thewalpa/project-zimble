---
to: ui
from: data
status: open
blocking: no
created: 2026-10-02
---

# Review new-career ratings after age-adjusted generation

## Why
New careers now start with weaker teenagers and veterans than players in their prime. Generated wages follow those ratings. Existing careers retain their saved profiles and provenance.

## What exists
`worldgen.Version` 9; `World.ObservePlayers`, `Squad`, `PlayerProfile` and `Summary` keep their existing types. No new commands, views, authoritative fields, save schema or UI formatting are needed. The seed-42 world fingerprint is `f5b5441d59a34327522f77952e83aa363a16ebbe59c7dd070e0a69f3a9e3054f`.

## What is needed
Review any client expectations tied to generated starting ratings or wages. Identities, dates, club assignments and contract lengths retain their old draws. Mechanical generated-world fixture updates in `cmd/play/main_test.go` and `cmd/web/main_test.go` were needed to unblock the full suite; please review them. Lineup edits/releases now target starter 47. Free-agent signing uses 274 (Gunnar Haugen). The purchase uses 497 (Rasmus Hagen), and the deliberately rejected bid uses 1.00 for 238. Listed player 44 is bought by Juniper (offer 92); nonzero two-spell career totals are checked on purchased player 497 (14 apps, 9 goals), while sold player 44 has zero appearances under the new selection. No client production behavior changed.

Managed cup penalties and wins use Saltmere (seed 42, club 5: semi-final away to Eldhaven, won 2-1 on penalties, then final 2-0 over Hollowick); terminal coverage also checks Eldhaven's managed win (club 6). Ironbridge (club 12) covers a quarter-final exit, with penalties still shown in its bracket. Promotion uses seed 2026, club 23, a first-place team that wins its play-off. Client formatting, invalid-command rejection, observer-specific result wording and save/load assertions remain covered.

## Done when
Any remaining client fixtures expecting old generated ratings are checked, or this informational review is accepted without changes.
