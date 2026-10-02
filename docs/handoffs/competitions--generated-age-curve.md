---
to: competitions
from: data
status: open
blocking: no
created: 2026-10-02
---

# Review generation-dependent competition expectations

## Why
`worldgen.Version` 9 gives starting players age-adjusted attributes and wages. New careers' results, rankings and cup qualifiers can change for the same seed.

## What exists
The seed-42 world fingerprint is `f5b5441d59a34327522f77952e83aa363a16ebbe59c7dd070e0a69f3a9e3054f`. Schedules, competition definitions, ScheduleVersion and LeagueVersion are unchanged; existing careers keep their stored profiles and generation provenance. App season goldens were mechanically refreshed under worldgen's version bump.

## What is needed
Review any competition expectations tied to generated results. Data reviewed your latest `world.go`/`save.go` additions: cup-calendar and football-year checks are applied at creation and restore, with pinned definitions and no new authoritative fields; accepted without changes.

## Done when
Accept the informational generation review or report an unexpected generation-dependent fixture failure.
