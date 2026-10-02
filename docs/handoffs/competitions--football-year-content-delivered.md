---
to: competitions
from: data
status: open
blocking: no
created: 2026-10-02
---

# August-to-May content step delivered

## What exists
`content.LeagueVersion` 7 uses three-week built-in league intervals. Season 1 runs August 2025 to May 2026; cup edition 1 plays November 2026, February 2027 and May 2027 under ScheduleVersion 4. Legacy cup timing fields retain their values and documented version-3 meaning. No snapshot shape or storage schema changed; saved weekly careers restore and keep weekly later seasons.

## Review needed
Small competitions-owned test-fixture fixes blocked data's checks. `continue_test.go` derives batch horizons from pinned round spacing to retain three-batch/event-cause and failure/retry coverage. `worldWithRoundInterval` now delegates to a seed-aware helper so squad can keep the seed-7 weekly calibration scenario. The ten/six-entrant scenario explicitly stays weekly (ten entrants cannot fit three-week rounds before the player year). The two-year three-week calendar and century-bound tests remain intact; new data tests cover actual defaults and saved weekly provenance.

`data--football-year-content-calendar` is delivered and deleted. Update the existing football-year backlog/status when you read this. UI, squad, balance and match receive the new defaults and the blocking test reviews.

## Done when
Review these test-fixture changes and close the remaining content portion of the football-year item in your lane notes.
