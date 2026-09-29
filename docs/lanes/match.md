# Lane: match

The match engine and everything on matchday: the match contract, the engine that implements it, lineups and tactics, AI selection, the manager's live match, and turning a round of fixtures into official results.

## Owns

- `internal/matches` (the contract: `MatchInput`, `MatchOutcome`, roles, tactics, rules, incidents).
- `internal/matches/simple`, `internal/matches/tick`, and any future engine.
- `internal/matches/enginetest`: the contract suite every engine's tests run.
- `internal/selection` (submitted lineups) and `internal/ai/selection.go`.
- `internal/app/lineup.go`, `live.go`, `resolve.go`: preparing match input from world state, the live-match replay log, and recording outcomes.
- Versions: `simple.ModelVersion`, `tick.ModelVersion`, `ai.SelectionVersion`, with their goldens.

## Rules for this lane

- **Engines stay isolated.** An engine imports only `matches` and core, owns only session-local state and never changes the world. Everything it needs arrives in `MatchInput` and everything it produces leaves in `MatchOutcome`.
- **The contract is shared.** `matches` is read by `app`, `ai`, `selection` and both clients. Prefer additive changes. When a field changes meaning, or an outcome gains something worth showing (a new incident, statistics), file notes to `ui`, to `competitions` if it affects results, and to `squad` if it affects condition or money.
- **Recording results touches other lanes' modules.** `resolve.go` hands results to the `competitions` module (its own lane), and condition drain and gate receipts to `medical` and `finance` (the `squad` lane). Keep those calls as they are. If a change needs new behavior in those modules, write a note to the owning lane.
- **Engines are replaceable.** A new engine must pass the same contract tests as `simple` (`enginetest.Contract`): legal participant minutes, consistent scores and incidents, valid substitutions, and a deterministic replay from the same `RandomState`. [docs/architecture.md](../architecture.md#match-engine-contract) says replacing the engine without changing callers is the architecture's best proof.
- **Balance is tested statistically:** large seeded batches must show plausible trends, such as a stronger team winning more often, or more goals under an attacking mentality. Don't test by restating formulas.
- Integer and permille arithmetic only. Changing results for a seed requires bumping `ModelVersion` and updating the goldens.

## Depends on

- **`competitions`:** decides which rounds are ready and when `Continue` stops for a matchday. Knockout rules (extra time, two legs) come from it.
- **`squad`:** condition, injuries and availability that decide who can play. Future injuries will arrive as a new input or an eligibility rule: agree on the shape through a note.
- **`data`:** player attributes and generated content. Ask for a new attribute rather than deriving one inside the engine.
- **`ui`:** shows lineups, the live match and reports. Tell it about every new decision point or incident.

## Now

**Tick engine tuning before phase 2.** `internal/matches/tick` simulates the ball and all 22 players five times a second, behind the same contract as `simple`, and emits positional frames (phase 1). It reads all eleven attributes through `matches.Ratings`, but only Dribbling of the five new ones so far. No career match uses it yet. Next are balance's two accepted notes: [mentality as a real trade-off](../handoffs/match--tick-mentality.md), then [goals that depend on the gap, not the level](../handoffs/match--tick-goals-by-level.md). Phase 2 follows. Before it, settle one career football model for interacting competitions (PAR-09 below). See the roadmap below.

## Tick engine roadmap

Each phase ends with all three checks green, `tick.ModelVersion` bumped when output changes, and the trend tests in `internal/matches/tick/model_test.go` still holding (2.0–3.5 goals between equal teams, home advantage, stronger sides and attacking mentality score more).

1. **Done: first playable model.** Formation spots, marking, pressing, passing, dribbling, shooting, tackles, saves, deflections, restarts and a shootout; `PositionalFrames` in the contract; the shared contract suite; about 20 ms per match.
2. **Playable in a career.**
   - Choose the engine per match in `app`: the manager's live match on `tick`, background matches on `simple` until `tick` is fast enough. The engine ID and version already travel in `MatchOutcome` and the live-match log; check with `data` whether the save needs to record the choice (a schema bump) or can derive it.
   - Expose frames for the live match through an `app` query and file a `ui` note: a 2D pitch view in `cmd/web` (canvas, frames decimated to a few per second), text commentary in `cmd/play`.
   - Ask `balance` to compare `tick` with `simple` over league seasons: goals, home and draw rates, upsets.
3. **Match statistics** (the backlog item) from `tick`'s counters: shots, shots on target, possession, passes and completion, tackles, saves. Advertised through `DetailedStats`, "unavailable" from `simple`. Needs a contract addition and a `ui` note for reports.
4. **Richer football**, one rule per step, each with its trend test:
   - offside, instead of capping runs at the last defender;
   - fouls, free kicks, penalties and cards (`Cards` capability, with `competitions` for suspensions);
   - the ball in the air: crosses, headers, long balls, goalkeepers catching crosses (needs aerial attributes from `data`);
   - smarter movement: runs into space, overlaps, a compact block, keeper distribution;
   - formations and roles beyond GK/DF/MF/FW (the backlog item), fed from `selection`;
   - live injuries (`Injuries` capability) and workload from distance and sprints rather than minutes, handed to `squad` for condition.
5. **Speed and persistence.** Under 5 ms per match so every fixture can use it (fewer square roots, cheaper marking), then checkpoints for mid-match saves. Switching the default engine changes every seeded career, so it needs `balance`'s sign-off and notes to all lanes.

## What the tick engine needs from other lanes

- **`data`:** delivered: the five phase-4 attributes are in `matches.Ratings`. Heading, Strength, Acceleration and Positioning wait for their phase-4 rules.
- **`balance`:** the synthetic-team review is delivered in [docs/balance.md](../balance.md#match-engines-tick-against-simple), and the shootout note is answered. The mentality and goals-by-level notes are accepted and come next; the career-season comparison follows phase 2.
- **`ui`:** a pitch view once frames are reachable through `app` (phase 2; the note follows that work).
- **`squad`:** later, workload from distance run instead of minutes played (phase 4).
- **`competitions`:** nothing yet; extra time would need the reserved `Resolution` value.

## Backlog

- **AI in-match decisions:** the AI opponent reacts at half time and after goals (substitutions, mentality), through the same decision types the manager uses. Deterministic, and replayable from the live-match log.
- **Delegate lineups to the assistant again:** a way for the manager to let the AI pick every week after having submitted a lineup (today a submitted lineup carries over until replaced). Needs a stored "delegated" choice, so a save schema bump.
- **Match statistics** in the outcome (shots, possession share), with a `ui` note for reports. `tick` counts them already; see roadmap phase 3. Reports already keep every match event (`MatchReport.Events`); statistics would sit beside them and need another schema bump.
- **Auto-resolving batches** of rounds that involve no user fixture, led by `competitions`, which owns `Continue`.
- **Statistical balance:** answer `balance`'s notes about goal rates, home advantage and upsets.
- **Formations and roles** beyond GK/DF/MF/FW, when selection needs them (roadmap phase 4 for `tick`).

- **A saved team plan outside matchday:** let the manager edit preferred starters, bench and mentality without a ready fixture. Keep the plan separate from fixture submissions; use it to prepare the next lineup, then revalidate employment and availability at kickoff and report dropped players. Coordinate with `ui`'s always-available lineup editor and `squad`'s injuries. Save/load must preserve the plan, and editing it must not rewrite past lineups or a live match.
- **Explain lineup readiness:** expose structured reasons a player or lineup cannot be selected, such as an unavailable player, duplicate selection or missing role. Reuse command validation so the preview and submission agree; `ui` renders the explanation, while `squad` and `competitions` supply availability and eligibility facts. Include a test that availability changing after preview causes a clear rejection at submission.

### AI/player rule parity audit

- **P2 — Equal management capabilities (PAR-08):** extend the existing AI in-match/delegated-lineup items to cover live and background fixtures through the same legal commands and decision opportunities. AI currently always starts Balanced and never substitutes; a human can do both. Replay deterministic AI choices independently of user stepping frequency and verify minutes, fatigue, injuries and saved delegation. See [audit](../ai-manager-parity.md#par-08-only-the-human-currently-uses-in-match-management).
- **P3 — Engine choice independent of controller (PAR-09):** before roadmap phase 2, agree one career football model for interacting competitions, with frames optional. The proposed human-live `tick`/background `simple` split changes football outcomes, not just presentation. If retaining tiers, pin their rule explicitly with data and require balance's career evidence; watch/skip must not change the model for the same fixture and decisions. See [audit](../ai-manager-parity.md#par-09-planned-engine-split-could-make-watching-change-football-rules).
