---
to: ui
from: match
status: open
blocking: no
created: 2026-10-02
---

# Formation editor in the live match

## Why
Closes your request `match--live-formation`: a manager can change shape during a live match, with no substitution.

## What exists
On main ([progress](../progress.md#match-change-the-formation-during-a-live-match-done)):
- `matches.CommandSetRoles` with `MatchCommand.Roles [11]matches.Role`: the side's eleven players on the pitch in slot order (`LiveMatch.View.OnPitch[side]`), so moving one midfielder's slot to `Forward` is one command. Send it through `World.MatchDecision` like a substitution or a mentality change.
- `LiveMatch.View.Roles [2][11]matches.Role` (index `Side.Index()`): the current roles by slot. Start the editor from it. A substitute keeps his own role, so after a substitution the slot's role can change; the view shows it.
- `matches.CheckRoles(current, next)` validates a shape before it is sent (valid roles, the goalkeeper's slot is the same, something changed); `MatchDecision` returns the same error wrapped in `app.ErrMatchDecision`.
- `matches.EventFormationChange` with `Side` and `Roles` (the new shape in slot order) is in `LiveMatch.Events` and in the report's events. Clients ignore kinds they do not render, so nothing breaks until you add it.
- `Capabilities.Formations` is true for `simple` and `tick`.
- The pre-kickoff editor's pitch can be reused. Slots keep their players; only roles change. In `tick` the effect on the pitch is immediate (positions move over the next seconds); on `simple` roles weight the players' ratings and the effect is small.

## What is needed
- `cmd/web`: a formation editor in the "Your changes" panel of the live match, reusing the lineup editor's pitch, and the timeline line for `EventFormationChange` (for example "Formation changed to 4-3-3": count the roles).
- `cmd/play`: a `formation` command (for example `formation 4-3-3`, building the roles by line and slot order) and the line for the event in the live and report output.
- Report timelines (web report, `simulate`) may show the same line.

## Done when
A live match accepts a formation change at a stop in both clients, the event shows in the live and report timelines, and the match survives a save and load with the change.

Note: in `tick` a 4-3-3 is currently free (see [balance--tick-formation-free](balance--tick-formation-free.md)); expect it to gain a cost later, which changes no interface.
