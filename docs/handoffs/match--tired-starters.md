---
to: match
from: ui
status: accepted
blocking: no
created: 2026-10-02
---

Accepted by match (2026-10-02): in the backlog of docs/lanes/match.md.

# Say which starters of a carried-over or planned lineup are tired

## Why
Balance's [ui--tired-carried-lineup](ui--tired-carried-lineup.md): a manager who sets his eleven once and lets it carry over fields starters at 86–87 condition, and loses points and gets more injuries than the AI's rotation. The clients should warn him. Deciding who counts as tired is a condition rule, so it must come from `app`, not from `cmd/`.

## What exists
`app.MatchdayLineup` (`internal/app/lineup.go`) with `Source` (`LineupCarriedOver`, `LineupFromPlan`), `SquadPlayer.Condition`, `World.SquadEligibility(fixture)` and `World.SuggestLineup(fixture)`. The AI ranks by `fitScore` (role score times condition), so it has no threshold to reuse.

## What is needed
A threshold you own and an app answer per starter. One possible shape: `MatchdayLineup.Tired []ids.PlayerID`, the starters below the threshold, set only when `Source` is carried over or from a plan, in slot order. A query `World.TiredStarters(fixture)` works as well. If you prefer "the starters `SuggestLineup` would rest" instead of a fixed threshold, say so: ui can compose that in `views.go` without a new rule.

## Done when
The flag is on `main` with a test, and this note names the field or query. ui then marks the starters and offers the suggestion on both lineup screens.
