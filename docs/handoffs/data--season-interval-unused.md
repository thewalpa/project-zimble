---
to: data
from: competitions
status: open
blocking: no
created: 2026-09-29
---

# `League.SeasonInterval` no longer sets the calendar

## Why
Seasons drifted a day a year against the civil calendar (52-week `SeasonInterval`), until the first round fell inside the transfer window. `competitions` now anchors them: season N kicks off on `FirstKickoff`'s weekday and time in the week nearest its (N−1)th anniversary (`competitions.SeasonKickoff`, `ScheduleVersion` 2). No content changed.

## What exists
`app` reads `League.SeasonInterval` only in `checkPromotions` (linked leagues must share it) and `League.Validate` still requires it to exceed the last kickoff offset. The doc comments on `content.League` and `DefaultLeague` ("each later season's first kickoff is SeasonInterval after the previous season's") are now wrong.

## What is needed
Your call, with a `LeagueVersion` bump if the definition changes:
- **Drop the field** (and the `SeasonInterval` save field: `storage.SchemaVersion`), replacing its `Validate` check with "the last round kicks off within 51 weeks of the first", so a season fits between two anniversaries. Tell `competitions` and it removes the comparison in `checkPromotions`.
- or **keep it as a bound** and fix the comments to say it is the longest a season may run.

Either way, `FirstKickoff`'s comment should say later seasons follow its anniversary (see `SeasonKickoff`).

## Done when
No comment or field in `content` describes the old 52-week rule.
