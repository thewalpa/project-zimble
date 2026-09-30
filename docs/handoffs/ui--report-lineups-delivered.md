---
to: ui
from: match
status: open
blocking: no
created: 2026-09-30
---

# Both sides' played lineups, other clubs' probable lineup, and the flank of a slot

Answers `match--report-lineups` and `match--line-slot-flank` (both deleted).

## What exists
- **`MatchReport.Lineups [2]selection.Lineup`** (home, away; [resolve.go](../../internal/app/resolve.go)): what each side started the match with, whoever chose it: starters in slot order with the roles they played, the bench, and the starting mentality. Substitutions and later mentality changes are in `Events`. Stored in the save (schema 26), so it is there for every match played, the AI's side included, and after a load. `World.MatchReport(fixture)` returns it.
- A result recorded without a report (a save from before this change cannot be loaded, so in practice none) returns a report with empty lineups: check `len(Starters) == 0`.
- **`World.ProbableLineup(team)`** returns the lineup the AI would field for any senior team of a current league if it played today (current condition and injuries, the league's match rules). Use it for another club's formation on its squad page. It is a forecast: the squad changes by kickoff. It rejects the user club's own team (`ErrInvalidCommand`): use `TeamPlan` or `MatchdayLineup` there.
- **Flank:** the first slot of a line is on the team's **left** flank looking upfield from its own goal, the last on its right (documented at `matches.PitchLength`; `TestLineSlotsRunLeftToRight`). On a pitch drawn with the attack at the top and each side's own goal at the bottom, both sides' lines run left to right in slot order. **Don't mirror.**

## What is needed
Draw both sides in the report (web and play) from `MatchReport.Lineups`, and the other club's probable formation on its squad page from `ProbableLineup`. Replace the user-club-only `SubmittedLineup` use in the report.

## Done when
The report of any played fixture shows both formations, and a club's squad page shows what it would field. Delete this note then.
