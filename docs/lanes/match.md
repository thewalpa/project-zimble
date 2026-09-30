# Lane: match

The match engine and everything on matchday: the match contract, the engine that implements it, lineups and tactics, AI selection, the manager's live match, and turning a round of fixtures into official results.

## Owns

- `internal/matches` (the contract: `MatchInput`, `MatchOutcome`, roles, tactics, rules, incidents).
- `internal/matches/simple`, `internal/matches/tick`, and any future engine.
- `internal/matches/enginetest`: the contract suite every engine's tests run.
- `internal/selection` (submitted lineups) and `internal/ai/selection.go`.
- `internal/app/lineup.go`, `live.go`, `resolve.go`: preparing match input from world state, the live-match replay log, recording outcomes, and the career's match engine (`Config.Engine`, `LiveFrames`).
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

**Tick engine: phase 2 is delivered on the `app` side; its UI and career comparison are with other lanes.** A career chooses its engine once (`app.Config.Engine`: `simple` by default, or `tick`); the save pins it, and it plays every fixture, watched or not, which settles PAR-09 ([progress](../progress.md#match-one-engine-per-career-live-frames-done)). `World.LiveFrames` serves the live match's positional frames and `World.MatchEngine` says whether the engine has them. `ui` has the note for an `-engine` flag and a pitch view; `balance` has the note to compare careers on both engines. `tick` stays opt-in: a season costs 12.5 s on it against 0.26 s on `simple`. Next: roadmap phase 3, match statistics from `tick`'s counters.

## Tick engine roadmap

Each phase ends with all three checks green, `tick.ModelVersion` bumped when output changes, and the trend tests in `internal/matches/tick/model_test.go` still holding (2.0–3.5 goals between equal teams, home advantage, stronger sides and attacking mentality score more).

1. **Done: first playable model.** Formation spots, marking, pressing, passing, dribbling, shooting, tackles, saves, deflections, restarts and a shootout; `PositionalFrames` in the contract; the shared contract suite; about 20 ms per match.
2. **Done: playable in a career.**
   - One engine per career (`Config.Engine`), pinned by the save's existing `Versions.EngineID`; no per-match split (PAR-09). No schema bump.
   - `World.LiveFrames` and `World.MatchEngine`; the `ui` note asks for an `-engine` flag and a pitch view in `cmd/web`.
   - The `balance` note asks for a career-season comparison of `tick` and `simple`.
3. **Match statistics** (the backlog item) from `tick`'s counters: shots, shots on target, possession, passes and completion, tackles, saves. Advertised through `DetailedStats`, "unavailable" from `simple`. Needs a contract addition and a `ui` note for reports.
4. **Richer football**, one rule per step, each with its trend test:
   - offside, instead of capping runs at the last defender;
   - fouls, free kicks, penalties and cards (`Cards` capability, with `competitions` for suspensions);
   - the ball in the air: crosses, headers, long balls, goalkeepers catching crosses (needs aerial attributes from `data`);
   - smarter movement: runs into space, overlaps, a compact block, keeper distribution;
   - game state: a side that leads comfortably eases off, which would further temper goals in mismatches (v5 note);
   - formations and roles beyond GK/DF/MF/FW (the backlog item), fed from `selection`;
   - live injuries (`Injuries` capability) and workload from distance and sprints rather than minutes, handed to `squad` for condition.
5. **Speed and persistence.** Under 5 ms per match (26 ms at v4; marking is the largest cost) so every fixture can use it (fewer square roots, cheaper marking), then checkpoints for mid-match saves. Switching the default engine changes every seeded career, so it needs `balance`'s sign-off and notes to all lanes.

## What the tick engine needs from other lanes

- **`data`:** delivered: the five phase-4 attributes are in `matches.Ratings`. Heading, Strength, Acceleration and Positioning wait for their phase-4 rules.
- **`balance`:** the synthetic-team review is delivered in [docs/balance.md](../balance.md#match-engines-tick-against-simple), and the shootout, mentality and goals-by-level notes are delivered (a refresh of the tick tables at v5 is requested). The career-season comparison is requested ([note](../handoffs/balance--tick-career-seasons.md)).
- **`ui`:** an `-engine` flag and a pitch view from `LiveFrames` ([note](../handoffs/ui--tick-career-and-pitch-view.md)).
- **`squad`:** later, workload from distance run instead of minutes played (phase 4).
- **`competitions`:** nothing yet; extra time would need the reserved `Resolution` value.

## Backlog

- **AI in-match decisions:** the AI opponent reacts at half time and after goals (substitutions, mentality), through the same decision types the manager uses. Deterministic, and replayable from the live-match log.
- **Delegate lineups to the assistant again:** a way for the manager to let the AI pick every week after having submitted a lineup or saved a team plan (today a submitted lineup carries over until replaced, and a saved plan cannot be cleared). Needs a stored "delegated" choice, so a save schema bump.
- **Match statistics** in the outcome (shots, possession share), with a `ui` note for reports. `tick` counts them already; see roadmap phase 3. Reports already keep every match event (`MatchReport.Events`); statistics would sit beside them and need another schema bump.
- **Auto-resolving batches** of rounds that involve no user fixture, led by `competitions`, which owns `Continue`.
- **Statistical balance:** answer `balance`'s notes about goal rates, home advantage and upsets.
- **Formations and roles** beyond GK/DF/MF/FW, when selection needs them (roadmap phase 4 for `tick`).
- **Explain lineup readiness:** player eligibility is done (`World.SquadEligibility`, shared with `SubmitLineup`, tested for an injury after the preview). Left: structured reasons a whole lineup is rejected (duplicate selection, missing goalkeeper, bench too long) as typed errors rather than messages, so `ui` can render them; suspensions from `competitions` will add an `Eligibility` value.

### AI/player rule parity audit

- **P2 — Equal management capabilities (PAR-08):** extend the existing AI in-match/delegated-lineup items to cover live and background fixtures through the same legal commands and decision opportunities. AI currently always starts Balanced and never substitutes; a human can do both. Replay deterministic AI choices independently of user stepping frequency and verify minutes, fatigue, injuries and saved delegation. See [audit](../ai-manager-parity.md#par-08-only-the-human-currently-uses-in-match-management).
- **Done — engine choice independent of controller (PAR-09):** one engine per career, saved, for every fixture; see [progress](../progress.md#match-one-engine-per-career-live-frames-done). A future cheaper tier for background matches needs saved eligibility and `balance`'s evidence first.
