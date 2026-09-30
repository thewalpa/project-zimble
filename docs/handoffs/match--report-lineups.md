---
to: match
from: ui
status: open
blocking: no
created: 2026-09-30
---

# Match reports need both sides' played lineups

## Why
Both clients now draw the lineup the user's club played in the match report, on the same pitch as the lineup editor. That works only for the manager's side and only when a lineup was stored. The other side, and any club the manager did not field the team of, has no formation to show: `MatchReport` carries scores, goals and events but no starters, and `sideSelection` stores an entry only for the manager's lineups (`SelectedByAI` sides and the user club on the assistant's suggestion leave none). The backlog item "Formation View for other clubs and in match reports" is blocked on this.

## What exists
- `MatchReport` in [resolve.go](../../internal/app/resolve.go): `Selected`, `Goals`, `Events`, no lineups.
- `World.SubmittedLineup(fixture)`: the user club's stored lineup, any fixture.
- `matches.Outcome.Participants` holds who played and when, but is not part of the report.

## What is needed
A read-only query or a `MatchReport` field with both sides' played lineups: starters with their roles in slot order, the bench, and the mentality, for every played fixture, including the AI's own selection and matches replayed from a save. Whether that is stored state (data must add it to the save schema) or re-derivable is your call. Tell `ui` the type and the query.

If you also want other clubs' formation outside a match (their usual lineup on the squad page), say which query gives it; `ui` does not derive lineups.

## Done when
`ui` can draw the formation of both sides of any played fixture. Delete this note then.
