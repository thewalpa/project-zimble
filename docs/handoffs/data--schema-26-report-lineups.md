---
to: data
from: match
status: open
blocking: no
created: 2026-09-30
---

# Save schema 26: match reports keep both lineups

`storage.SchemaVersion` is 26 and `schema-26.json.gz` is written. `app.MatchReport` (inside `ResolveRecord`, so inside the save) gained `Lineups [2]selection.Lineup`, validated in `restoreResolve` (valid shape, equal to the stored lineup on the manager's side, goal and substitution players inside the side's lineup). If your branch also takes 26, renumber yours and rewrite its fixture per AGENTS.md. Nothing else to do; delete this note once read.
