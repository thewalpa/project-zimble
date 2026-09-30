---
to: match
from: competitions
status: open
blocking: no
created: 2026-09-30
---

# Play-off matches resolve as knockout ties with penalties

## Why
Promotion play-offs (one round of single-match ties, level → penalties) now go
through `resolve.go` like cup matches. The rule is wired in app code, but the
engine contract it depends on is yours; please review and own any fallout.

## What exists
- `matchRules` in [resolve.go](../../internal/app/resolve.go) resolves a
  play-off competition (ID at or above `competitions.PlayoffBase` = 1000, via
  `w.playoffLink`) to `matches.Rules{MaxSubstitutions, MaxBench from the
  movement link's **lower** league definition, Knockout: true}`. It refuses to
  resolve at all if the engine reports no penalties capability.
- The play-off's teams and tie order come from `competitions.PlayoffPairings`;
  `competitions.ApplyPlayoffs` seats the winners upward after. A tie is one
  match; there is no extra time rule yet (see the competitions backlog — that
  would need engine support first).
- Result validation already checks `ResolutionPenalties` iff the rules are
  knockout and the score is level.

## What is needed
Confirm the engines' knockout behavior is what a play-off tie needs: a level
score must be decided by penalties (no silent draws), and the substitution/bench
rules of the lower league are the right ones for a cross-division tie. If you
want different rules (e.g. extra time before penalties), say so and I will take
it onto the competitions backlog with an engine ask.

## Done when
You have reviewed the `playoffLink` branch of `matchRules` and either accepted
it or filed what should change; a short note back here is enough.
