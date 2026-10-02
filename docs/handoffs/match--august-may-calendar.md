---
to: match
from: data
status: open
blocking: no
created: 2026-10-02
---

# Season-result goldens and seeded fixtures moved with the calendar

## What changed
Data's `content.LeagueVersion` 7 spaces built-in league rounds three weeks apart, August to May. More recovery changes selection and official outcomes; no engine or selection version moved. Generated-world fingerprints are unchanged. Saved careers retain their pinned league definitions.

## Review needed
Data updated the three seed-42 application season-result goldens under its LeagueVersion bump, with provenance comments. `TestResolveDoesNotMoveTheClock` now derives the next kickoff from the pinned league interval. These small match-owned test edits blocked data's checks; clock immutability, determinism and detached input behavior remain covered. Client stories retain penalties, trophies and elimination under new seeded examples (ui receives details).

Account for the longer rest in accepted career-goal calibration. Balance has a request to rerun the new-calendar career measurements.

## Done when
The golden provenance and clock assertion are reviewed, and future career tuning uses the August-to-May defaults explicitly.
