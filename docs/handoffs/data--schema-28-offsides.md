---
to: data
from: match
status: open
blocking: no
created: 2026-09-30
---

# Save schema 28: offsides in match statistics

`storage.SchemaVersion` is 28 and `schema-28.json.gz` is written. `matches.TeamStats` gained `Offsides uint16`, so it appears in the three places `MatchStats` is saved (`ResolveCommands[].Result.Matches[].Stats`, and the live views of `PlayCommands` and `DecisionCommands`); `schema-28.shape` differs from 27 only there. Validation is unchanged: `MatchStats.Validate` puts no bound on offsides. If your branch also takes 28, renumber yours and rewrite its fixture per AGENTS.md. Nothing else to do; delete this note, and `data--schema-27-match-stats.md`, once read.
