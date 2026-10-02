# Medical calibration on the football year

Measured 2026-10-02 against `b4c3d66`, with `worldgen.Version` 9,
`worldgen.YouthVersion` 4, `players.DevelopmentVersion` 1, `content.Version` 9,
`content.LeagueVersion` 7, `competitions.ScheduleVersion` 4, `medical.Version` 4,
`ai.SelectionVersion` 3, `ai.ContractsVersion` 2, `ai.TransfersVersion` 7 and
`simple.ModelVersion` 5. These are new AI-only careers with age-developed
starting stamina, not restored pre-v9 attributes.

Reproduce both the default calendar and the retained weekly stress scenario:

```sh
go test ./internal/app -run 'Test(InjuryAndConditionLevelsOverSeasons|MedicalLevelsOnTheFootballYear)$' -count=1 -v
```

Each run covers three years and all 32 clubs. Injury incidence uses 1,920
player-seasons: annual squad headcounts, including reserves, rather than only
regular starters. Every run samples 15,356 starts across league, cup and
promotion fixtures. An injured player is counted out at his club's kickoff;
layoff length is the assigned recovery days, not the number of matches missed.
Starter condition is sampled before exposure. Rest buckets use the team's
previous fixture, so a rotating player can personally have rested longer.
First matches belong with the longest-rest bucket. World validation passes
at the end of each run.

| Calendar / seed | Injuries | Per squad player-year | Days per injury | Out per club kickoff | Maximum out | Tired starts | Mean starter condition | Minimum condition |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Default / 7 | 936 | 0.487 | 18.87 | 0.34 | 3 | 2.31% | 99.82 | 80 |
| Default / 42 | 823 | 0.429 | 20.39 | 0.30 | 3 | 2.17% | 99.83 | 77 |
| Default / 2026 | 884 | 0.460 | 19.48 | 0.35 | 4 | 2.39% | 99.80 | 73 |
| Weekly stress / 7 | 1,119 | 0.583 | 19.05 | 1.14 | 6 | 44.63% | 94.88 | 29 |

No run requires an injured starter. The weekly stress scenario's minimum
with at least seven days between the team's fixtures is 41, preserving its
existing floor of 40. Its separate congestion floor stays 25. The default
calendar's corresponding minimum is 89 in all three seeds.

The default's low aggregate tired share hides the few short turnarounds:

| Team's fixture gap | Starts per default run | Tired starts, across seeds | Mean starter condition, across seeds |
| --- | ---: | ---: | ---: |
| Under 7 days | 308–330 | 65.52–70.30% | 92.52–93.45 |
| 7 to under 21 days | 506–528 | 23.98–26.68% | 98.85–99.03 |
| At least 21 days, or first match | 14,520 | 0% | 100.00 |

The total 0.459 injuries per squad player-year is slightly below the old
0.5–1.0 target, while the weekly scenario remains inside it. Availability at
kickoff is much higher on the default calendar because recovery continues
through the three-week league gaps. This agrees with data's pre-generation-v9
measurement of 2% tired starters and 0.33 out per club kickoff: starting
stamina changed, but the calendar still explains the main difference.

## Decision

Retain medical v4. The default calendar supplies enough rest for ordinary
league matches and still produces substantial missing condition during
short turnarounds. Increasing drain or slowing recovery enough to carry
fatigue across three-week breaks would also change the weekly scenario,
whose congestion minimum is already 29. Increasing incidence alone would
move the injury count without restoring a fatigue decision on rested
matchdays. These measurements do not justify either change.

Added a separate, always-on default-calendar regression over the three seeds.
It guards broad injury/availability ranges, successful legal selection,
limited aggregate fatigue, recovery after long breaks and visible fatigue
after short turnarounds. The existing weekly regression retains its original
incidence, crisis, tired-share and rested/congested bounds. No production
rule, version, save field or golden changes.

The old matched rotation result in `docs/balance.md` is historical. Balance
should refresh the two-engine careers and rotation comparisons, including
cup-exposed seasons. Its current first-year rotation harness has no cup
edition; with three-week league gaps it cannot measure the four-day cup
congestion represented above. Before changing medical rules to make
rotation matter more often, agree whether the fixture calendar is intended
to supply more frequent congestion. Do not infer that the medical fatigue
term is ineffective from a year with no short turnarounds.
