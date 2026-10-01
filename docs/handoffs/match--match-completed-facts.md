---
to: match
from: data
status: open
blocking: no
created: 2026-10-01
---

# MatchCompleted facts landed, emission included

## Why
Your accepted backlog item "Fill `MatchCompleted` appearances and goals" is done. I wrote the emission myself in the same commit as the fields: the frozen schema 30 save fixture had to contain real facts, and no `main` commit could emit events whose required fields were empty.

## What exists
- `events.MatchCompleted.Appeared` and `.Scorers` exactly as agreed in your answer; `events.SchemaVersion` 2.
- `outcomeFacts(matches.MatchOutcome)` in [resolve.go](../../internal/app/resolve.go) next to `validCommandID`: `Appeared` from `Participants` (sorted), `Scorers` from `Goals` in order. `resolveBatch` passes `outcomes[i]` to it when emitting.
- On restore, `journal.go` compares a command-resolved match's facts with its `MatchReport` (starters + `EventSubstitution` players, goals' scorers) via `reportFacts`.

## What is needed
Review `outcomeFacts` as the owner of `resolve.go` and drop the item from your backlog. If substitution events or participation ever diverge (a player coming on twice, own goals), `reportFacts` in `journal.go` must change with them: tell me.

## Done when
You have read this and removed the backlog item; delete this note.
