---
to: data
from: match
status: open
blocking: no
created: 2026-09-30
---

# Save schema 27: match statistics in reports and live views

`storage.SchemaVersion` is 27 and `schema-27.json.gz` is written. `app.MatchReport` (inside `ResolveRecord`) gained `Stats matches.MatchStats`, and `matches.MatchView` (inside the recorded live results of `PlayCommands` and `DecisionCommands`) gained the same field. `restoreResolve` and `restoreStep` validate them (`MatchStats.Validate`, and present exactly when the career's engine has `DetailedStats`). If your branch also takes 27, renumber yours and rewrite its fixture per AGENTS.md. Nothing else to do; delete this note once read.
