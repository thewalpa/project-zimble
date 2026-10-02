---
to: match
from: ui
status: accepted
blocking: no
created: 2026-10-02
---

Accepted by match (2026-10-02): in the backlog of docs/lanes/match.md.

# Change the formation during a live match

## Why
The owner asked to change tactics during a live match. Mentality is the only tactic the engines know, and it can already be changed at any minute of the web live match. A manager also needs to change shape during a match, for example from 4-4-2 to 4-3-3 when chasing a goal, or move a player into another role, without making a substitution.

## What exists
- `matches.MatchCommand` has two kinds, `CommandSubstitute` and `CommandSetMentality` ([matches.go](../../internal/matches/matches.go)). Each starter's `Role` is fixed in `MatchInput` for the whole match, and a substitute takes over the role of the player he replaces.
- `app.MatchDecision` applies any `MatchCommand` for the manager's side, and the replay log saves it. The web sends decisions from `/decide` at the clock's minute ([cmd/web/live.go](../../cmd/web/live.go)).
- `selection.Slot` (player and role) already describes a formation before kickoff.
- For your review: the web live clock reads ahead with `World.PreviewLive` ([views.go](../../internal/app/views.go)). It replays the recorded stops and advances further without recording anything, so it depends on `replay` and on the engines' chunking rule (`AdvanceChunkingDoesNotChangeTheMatch`). `TestPreviewLive` checks that the preview matches later recorded stops. Kick-off records minute 1 to lock the lineup. A change made at minute m records `PlayMatch` to m, then the decision.

## What is needed
A match command that changes the role of one or more of the manager's players on the pitch from the next minute. One possible shape: `CommandSetRoles` carrying the eleven roles in slot order, which allows a whole new formation in one decision. It needs the same validation as a lineup: one goalkeeper, who keeps the goalkeeper slot, and every role valid. It also needs a timeline event, for example `EventFormationChange` with the new shape, so reports and the live timeline can show the change, and a way to read the current roles from `MatchView` (or `LiveMatch`), so the live page can show and edit them. Both engines must honour the new roles (on `simple` the role weights, on `tick` the positions), and the AI should be able to make the same decision for parity (PAR-08).

Once this lands, ui will add a formation editor to the "Your changes" panel of the web live match, reusing the lineup editor's pitch, and a `formation` command to `cmd/play`.

## Done when
A live match accepts a formation change at a stop, both engines play it from the next minute, it survives save and load, and the match with the change resolves to the same result whether it was played live or replayed.
