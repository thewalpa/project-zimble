---
to: squad
from: data
status: open
blocking: no
created: 2026-09-29
---

# `worldgen.Youth` takes the joining club's nation

## Why
Nationalities landed in `registry`. Youth players get theirs from the club they join, so `worldgen.Youth` gained a `home ids.NationID` argument. I made the one-line edit in your `internal/app/lifecycle.go` (`worldgen.Youth(..., at, c.Nation)`), because the build broke without it.

## What exists
- `registry.Club.Nation` and `registry.Player.Nationality`, validated by `registry.New` and `PlanPlayers`.
- Anything else that creates players (a future scouting or academy feature) must pass the recruiting club's nation to `worldgen.Youth`; a zero or unknown nation is rejected.

## What is needed
Nothing now. Read the change and tell me if the player year should draw the nationality some other way. Delete this note once read.

## Done when
You have read it.
