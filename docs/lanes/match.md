# Lane: match

The match engine and everything on matchday: the match contract, the engine that implements it, lineups and tactics, AI selection, the manager's live match, and turning a round of fixtures into official results.

## Owns

- `internal/matches` (the contract: `MatchInput`, `MatchOutcome`, roles, tactics, rules, incidents).
- `internal/matches/simple`, and any future engine such as `internal/matches/detailed`.
- `internal/selection` (submitted lineups) and `internal/ai/selection.go`.
- `internal/app/lineup.go`, `live.go`, `resolve.go`: preparing match input from world state, the live-match replay log, and recording outcomes.
- Versions: `simple.ModelVersion`, `ai.SelectionVersion`, with their goldens.

## Rules for this lane

- **Engines stay isolated.** An engine imports only `matches` and core, owns only session-local state and never changes the world. Everything it needs arrives in `MatchInput` and everything it produces leaves in `MatchOutcome`.
- **The contract is shared.** `matches` is read by `app`, `ai`, `selection` and both clients. Prefer additive changes. When a field changes meaning, or an outcome gains something worth showing (a new incident, statistics), file notes to `ui`, to `competitions` if it affects results, and to `squad` if it affects condition or money.
- **Recording results touches other lanes' modules.** `resolve.go` hands results to the `competitions` module (its own lane), and condition drain and gate receipts to `medical` and `finance` (the `squad` lane). Keep those calls as they are. If a change needs new behavior in those modules, write a note to the owning lane.
- **Engines are replaceable.** A new engine must pass the same contract tests as `simple`: legal participant minutes, consistent scores and incidents, valid substitutions, and a deterministic replay from the same `RandomState`. [docs/architecture.md](../architecture.md#match-engine-contract) says replacing the engine without changing callers is the architecture's best proof.
- **Balance is tested statistically:** large seeded batches must show plausible trends, such as a stronger team winning more often, or more goals under an attacking mentality. Don't test by restating formulas.
- Integer and permille arithmetic only. Changing results for a seed requires bumping `ModelVersion` and updating the goldens.

## Depends on

- **`competitions`:** decides which rounds are ready and when `Continue` stops for a matchday. Knockout rules (extra time, two legs) come from it.
- **`squad`:** condition, injuries and availability that decide who can play. Future injuries will arrive as a new input or an eligibility rule: agree on the shape through a note.
- **`data`:** player attributes and generated content. Ask for a new attribute rather than deriving one inside the engine.
- **`ui`:** shows lineups, the live match and reports. Tell it about every new decision point or incident.

## Now

**AI in-match decisions:** the AI opponent reacts at half time and after goals (substitutions, mentality), through the same decision types the manager uses. Deterministic, and replayable from the live-match log.

## Backlog

- **Match statistics** in the outcome (shots, possession share), with a `ui` note for reports.
- **Auto-resolving batches** of rounds that involve no user fixture, led by `competitions`, which owns `Continue`.
- **Statistical balance:** answer `balance`'s notes about goal rates, home advantage and upsets.
- **A second engine** (`matches/detailed`) behind the same contract, to prove the boundary.
- **Formations and roles** beyond GK/DF/MF/FW, when selection needs them.
