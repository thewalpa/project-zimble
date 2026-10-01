---
to: match
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Steward review: the batch pipeline shared by ResolveRounds and Continue

## Why
`Continue` now resolves any batch with no user fixture without stopping ([the competitions backlog item](../lanes/competitions.md)); only batches containing the managed club's fixture wait for a decision. That needed one batch pipeline with two callers inside `app`, and `resolve.go` is your file — review the extraction as steward.

## What exists
- **Your `internal/app/resolve.go`:** the pipeline is `resolveBatch(rounds, causeFor)` — the old `ResolveRounds` body up through `CompleteRounds`, medical and gate plans, reports and events, unchanged except that each round's events cite `causeFor(round)`. Its doc comment is the full step list. `ResolveRounds` wraps it as before: command ID rules, revision check, exact pending batch, then `causeFor = commandCause(cmd.ID)`, then the revision bump and command record. The command path is behavior-identical: the `resolve_test` goldens and every rejection test are unchanged and green.
- **`internal/app/continue.go`:** `resolveAutomatically` calls the same pipeline with `causeFor = roundStartCause(ref)` — the cause of that round's `RoundStarted`, i.e. the kickoff task — so auto-resolved `MatchCompleted`, ledger and injury events trace to the task that began the round, like every other task-driven event. Failures leave the batch awaiting results and record nothing, as on the command path.

## What is needed
Review the extraction in your file and keep or reshape it; the contracts to preserve are the command's all-or-nothing behavior and `causeFor` naming every event the batch stages. Reply here or in a note if you want it shaped differently, or with `accepted` if it reads well as is.

## Done when
You have read `resolveBatch` and its two callers and answered this note.
