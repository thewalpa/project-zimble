---
to: balance
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Refresh rotation on cup-exposed football years

## Why
Squad refreshed generation-v9 medical measurements on the default football year.
Retain medical v4: long breaks restore starters, while short turnarounds still
reduce their condition. The historical weekly rotation results cannot establish
the size of that decision on the new calendar.

## What exists
[The measurements](../squad-medical-calibration.md) record all versions, seeds,
denominators and reproduction. Across seeds 7, 42 and 2026 over three years:
0.429–0.487 injuries per squad player-year, 0.30–0.35 out per club kickoff,
2.17–2.39% tired starts overall, but 65.52–70.30% after under seven days.
None of the 14,520 starts after at least three weeks (or a first match) in
each run is tired. No emergency starts. The separate weekly regression remains
unchanged in its bounds (minimum 29 congested, 41 after at least a week).

`TestMedicalLevelsOnTheFootballYear` now guards the default scenario separately.
No medical, engine or calendar rule/version changes in this work.

## What is needed
Combine with `balance--august-may-calendar` and the generation-v9 refresh:
rerun career-engine workload and matched carry/rotate/best comparisons. Include
at least a cup-exposed year: `TestBalanceRotation` currently measures only
season 1, which has no cup and cannot measure the default's four-day cup gaps.
Keep renewal/signing policy equal across arms when extending its horizon.
Report rest buckets and cup exposure alongside points, injuries and days lost,
so an arm with no congestion is recognizable.

If rotation loses its value only because almost every fixture is rested,
report that distinction before proposing medical changes. More frequent
congestion needs a joint calendar decision with competitions; slowing recovery
for all calendars is not justified by these measurements alone.

## Done when
Updated career and rotation evidence names generation v9 and LeagueVersion 7,
includes cup congestion, and separates calendar exposure from medical policy.
