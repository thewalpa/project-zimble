---
to: ui
from: match
status: open
blocking: no
created: 2026-09-28
---

# Show where the matchday lineup comes from

## Why
This answers your note `match--preserve-lineup-selection`. The manager's lineup now carries over: a pending fixture with no lineup submitted is played with the lineup the user club played last, and the match report says `SelectedByManager`. The clients still show the assistant's suggestion in that case, so what they show is no longer what gets played.

## What exists
In [internal/app/lineup.go](../../internal/app/lineup.go), on `main`:
- `World.MatchdayLineup(fixture) (MatchdayLineup, error)` returns the lineup that `ResolveRounds` and `PlayMatch` will field if the manager submits nothing more. It takes the same fixtures as `SuggestLineup`.
- `MatchdayLineup.Source` is a `LineupSource`, one of:
  - `LineupFromSubmission`: the manager submitted a lineup for this fixture.
  - `LineupCarriedOver`: the lineup from `From`, the user club's previous match that had one.
  - `LineupSuggested`: the assistant's pick, because the manager has never picked a lineup (or the carried one could not be completed).
  
  `String()` gives `submitted`, `carried over` and `suggested`.
- `MatchdayLineup.Dropped`: for a carried-over lineup, the players from the old lineup who are not in this one. A player is dropped when he left the squad (the AI refilled his starting place with a player in the same role) or when the bench was longer than this competition allows (cups and leagues can differ).
- `SuggestLineup` is unchanged and still returns the assistant's fresh pick. `SubmittedLineup(fixture)` now also returns the lineup that was carried into a fixture once that fixture has been played.

## What is needed
In both `cmd/play` and `cmd/web`:
1. Replace the `SubmittedLineup`, else `SuggestLineup` fallback (`cmd/play/main.go` around lines 925 and 969, `cmd/web/views.go` around line 492) with `MatchdayLineup`. The lineup screen, and the pre-match check that tells the manager whether they picked a side, should label the source: "Your lineup for this match", "Carried over from the last match (vs X)", or "The assistant's suggestion".
2. When `Dropped` is not empty, name those players: for example "Smith has left the club; Jones takes his place". Do this on the lineup screen and before a match is played from the home screen.
3. Keep an action that loads `SuggestLineup` into the editor ("Ask the assistant"). Submitting it makes it the new standing lineup.
4. `cmd/simulate` needs no change: `-mentality` submits every fixture.

## Done when
With `-seed 42 -club 3`: submit a changed lineup in round 1, then continue to round 2. The lineup screen in both clients shows round 1's lineup marked as carried over. Playing without submitting reports "your lineup". Release a starter between the rounds, and round 2's lineup names him as dropped and shows his replacement.
