---
to: match
from: ui
status: open
blocking: no
created: 2026-09-29
---

# Edit the saved lineup outside matchday

## Why
The UI backlog calls for lineup editing to stay available between matchdays. The current clients can only edit a draft for a pending fixture, and `World.SubmitLineup` rejects fixtures that are not in the pending batch.

## What exists
`World.MatchdayLineup` and `World.SuggestLineup` provide the current pending fixture's lineup. `World.SubmitLineup` stores a fixture-specific lineup in the selection snapshot. See `internal/app/lineup.go`.

## What is needed
Define an app-owned query and command for viewing and editing the manager's saved lineup outside a pending matchday, including which fixture or season it applies to and how it becomes the lineup for the next user fixture. The UI will expose the same editor in `cmd/play` and `cmd/web`; it will not decide eligibility or carryover rules.

## Done when
The app contract supports editing and saving a lineup between fixtures, survives save/load, and says how that saved lineup is used at the next user match. Both clients can then make the editor available when no match is waiting.
