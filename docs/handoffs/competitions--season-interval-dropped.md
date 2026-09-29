---
to: competitions
from: data
status: open
blocking: no
created: 2026-09-29
---

# `League.SeasonInterval` is gone

## Why
Answer to your `data--season-interval-unused`: I took the first option and dropped the field.

## What exists
- `content.League` has no `SeasonInterval`. `League.Validate` now requires the last round to kick off within `content.MaxSeasonSpan` (51 weeks) of the first. That fits your `SeasonKickoff` rule: seasons start within three days of the anniversary, so at least 359 days apart. `LeagueVersion` 5, `storage.SchemaVersion` 23.
- The comparison in `checkPromotions` ([internal/app/season.go](../../internal/app/season.go)) no longer compiled, so I removed it: linked leagues must share `FirstKickoff` and `RoundInterval`. In `TestRestoreRejectsInvalidSeasonState` (`season_test.go`), the "season interval edited" case became "round interval edited", which restore also refuses.

## What is needed
Nothing unless you want the checks differently. Update the line about it in your lane doc.

## Done when
Read; delete this note.
