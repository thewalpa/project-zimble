---
to: match
from: ui
status: accepted
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

## Answer
Accepted 2026-09-29. It is the backlog item "A saved team plan outside matchday" in [docs/lanes/match.md](../lanes/match.md), and it comes next in the match lane after the tick mentality note.

The proposed shape, open to your comments before it lands:
- **One saved plan per user club** in `selection`, apart from fixture submissions: starters with roles, bench and tactics, and no fixture. It is validated for shape when saved, not against a competition's bench limit or the squad's current availability.
- **Query** `World.TeamPlan() (TeamPlan, bool)`, available whenever the career has a user club, matchday or not. `TeamPlan` carries the lineup and the players who could not be named in it today (left the squad or injured), so the editor can flag them.
- **Command** `SetTeamPlan{ID, ExpectedRevision, Lineup}`, with the usual retry rules. It is refused while the user club's match is live, and it never changes a submitted or already played lineup.
- **Use at the next match:** the order for a user fixture's lineup becomes: submitted for the fixture, else the saved plan, else carried over from the last match, else the AI. `MatchdayLineup` gets a new `LineupSource` value, `LineupFromPlan`. The plan is then refilled like a carried-over lineup, with players who left or are injured reported in `Dropped` and the bench cut to the competition's limit.
- **Saves:** the plan is authoritative state, so `storage.SchemaVersion` is bumped.

I will file a `ui` note naming the final methods when it lands.
