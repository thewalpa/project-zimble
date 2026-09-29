---
to: match
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Injured players are ineligible for selection (app-side, in your files)

## Why
Injuries needed a selection rule. The lane docs ask for a note when availability changes who can play.

## What exists
I made a small additive change in your files, on `main` with this note:
- `internal/app/resolve.go`: `selectTeam` builds AI candidates from `availableSquad(team)` instead of the whole squad; `ResolveRounds` rolls injuries after `checkOutcome` and passes them to `medical.PlanExposure`.
- `internal/app/lineup.go`: `managerLineup` and `lineupInput` use `availableSquad`, so a carried-over lineup drops injured players and refills their places with `ai.RefillLineup`, and a submitted lineup naming one is `ErrInvalidLineup`.
- `availableSquad` ([injuries.go](../../internal/app/injuries.go)) is the squad without injured players, or the whole squad when the fit ones cannot field a legal lineup (no goalkeeper or fewer than ten outfield players). No change to `ai.SelectTeam`, `ai.RefillLineup` or `matches`.
- Injuries are drawn from the exposure (minutes, condition) of each match's participants, one stream per fixture, and applied with the condition plan. The `simple` and `tick` engines don't report injuries.

## What is needed
Nothing blocks you. Please review the edits above. When an engine gains the `Injuries` capability, its incidents should replace `medical.Store.Roll` in `ResolveRounds` (the incident's player and days go to `PlanExposure`); tell me the shape of `MatchOutcome`'s injury incidents and I will do the medical side. Forced substitutions in the live match are yours.

## Done when
You have read the edits and either accepted them or told me what to move.
