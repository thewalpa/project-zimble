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

**Tick engine: offside is delivered (phase 4, first rule).** Forwards make runs in behind, back lines hold, passers play through balls, and a receiver beyond the second-last opponent when the ball is played is caught offside, giving a free kick. Equal sides are caught about 1.8 times a side at the old goal level (2.54). `TeamStats.Offsides`, schema 28, `tick.ModelVersion` 6 ([progress](../progress.md#match-offside-in-tick-done)). The mentality effect is smaller than at v5 (both attacking 3.27 goals, both defensive 1.21). `tick` stays opt-in (a season costs about 12.5 s against 0.26 s on `simple`).

**Tick's mentality is a trade-off** (`tick.ModelVersion` 7: `MentalityConcedePermille` prices the room behind a line into the opponents' runs, counter passes and shots — the counter cost `balance` asked for in `match--attacking-is-free`, a fatigue cost left open for `squad`). Attacking is within 0.05 points a match of balanced at equal teams; defensive is the underdog's tool (+0.04–0.07) and draws more ([progress](../progress.md#match-the-mentality-trade-off-in-tick-done)).

**Lineup decisions read club knowledge** (PAR-10 adoption, `match--club-observations`): every selection, suggestion and refill reads the team's club's `ObservePlayers`; `ai.SelectTeam` returns a decision without ratings, and `lineupInput` builds every match input from the authoritative records. Seeded output unchanged ([progress](../progress.md#match-lineup-decisions-from-club-knowledge-done)).

**Tick's statistics are calibrated per minute of ball in play** (`tick.ModelVersion` 8, `match--tick-stats-calibration`). Tackles are at 27 a side, the real rate per minute of ball in play. Possession separates through first touch: 57.5% at gap 10 and 62.7% at gap 20. Equal teams take 11–12 shots a side. On-target is now counted at the goal line, and keepers leave shots going wide. Results by gap and the v7 mentality shape are kept. Passes stay ~1.6× because the ball is in play ~88 minutes (real ~58), and the stoppage rules fix that. The shot split at a gap needs the compact block ([progress](../progress.md#match-ticks-statistics-calibrated-done), [balance rerun](../balance.md#match-statistics-tick-against-real-football)).

**Simple has a home edge again** (`simple.ModelVersion` 5: `HomeAdvantagePermille` 1150, split between a home boost and an away handicap — 45/26/29 and 1.5–1.2 goals at 60 v 60). The seeded client stories were re-pinned to the new results ([progress](../progress.md#match-simple-has-a-home-edge-again-done), [note to `ui`](../handoffs/ui--seed-stories-moved-home-edge.md)). Next: the career goal level and `simple`'s mentality trade-off (one `simple` bump), then fouls, free kicks and cards (phase 4) with realistic restart durations. `tick`'s mentality payoff and shootouts now have always-on bounds in `model_test.go`.

## Tick engine roadmap

Each phase ends with all three checks green, `tick.ModelVersion` bumped when output changes, and the trend tests in `internal/matches/tick/model_test.go` still holding (2.0–3.5 goals between equal teams, home advantage, stronger sides and attacking mentality score more).

1. **Done: first playable model.** Formation spots, marking, pressing, passing, dribbling, shooting, tackles, saves, deflections, restarts and a shootout; `PositionalFrames` in the contract; the shared contract suite; about 20 ms per match.
2. **Done: playable in a career.**
   - One engine per career (`Config.Engine`), pinned by the save's existing `Versions.EngineID`; no per-match split (PAR-09). No schema bump.
   - `World.LiveFrames` and `World.MatchEngine`; the `ui` note asks for an `-engine` flag and a pitch view in `cmd/web`.
   - The `balance` note asks for a career-season comparison of `tick` and `simple`.
3. **Done: match statistics.** `matches.MatchStats` in the view, the outcome and reports, advertised through `DetailedStats`, unavailable from `simple`; schema 27; notes to `ui` and `balance`.
4. **Richer football**, one rule per step, each with its trend test:
   - **done (v6):** offside, with runs in behind, through balls and a back line that holds;
   - **done (v8):** statistics calibrated per minute of ball in play (tackles, possession through first touch, shot counting at the goal line);
   - fouls, free kicks, penalties and cards (`Cards` capability, with `competitions` for suspensions), each restart taking a realistic time (today ~1 minute of a match is dead, against ~30 in real football, which is why passes run 1.6× the real total);
   - the ball in the air: crosses, headers, long balls, goalkeepers catching crosses (needs aerial attributes from `data`);
   - smarter movement: runs into space beyond forwards' runs in behind, overlaps, a compact block (the weaker side's shots: v8's split at a gap is 15.2–7.7 against a real ~12.6–9.3), keeper distribution;
   - shot accuracy: on target 5.6 / 4.6 a side at 60 v 60 (real 4–5) since v8 counts at the goal line; `ShotErrorPermille` moves goals, so tune it with a goal-rate check;
   - game state: a side that leads comfortably eases off, which would further temper goals in mismatches (v5 note);
   - formations and roles beyond GK/DF/MF/FW (the backlog item), fed from `selection`;
   - live injuries (`Injuries` capability) and workload from distance and sprints rather than minutes, handed to `squad` for condition.
5. **Speed and persistence.** Under 5 ms per match (26 ms at v4; marking is the largest cost) so every fixture can use it (fewer square roots, cheaper marking), then checkpoints for mid-match saves. Switching the default engine changes every seeded career, so it needs `balance`'s sign-off and notes to all lanes.

## What the tick engine needs from other lanes

- **`data`:** delivered: the five phase-4 attributes are in `matches.Ratings`. Heading, Strength, Acceleration and Positioning wait for their phase-4 rules.
- **`balance`:** the synthetic-team review is delivered in [docs/balance.md](../balance.md#match-engines-tick-against-simple), and the shootout, mentality and goals-by-level notes are delivered. The career-season and statistics comparisons are delivered; the v8 refresh is delivered ([2026-10-02](../balance.md#match-statistics-tick-against-real-football)).
- **`ui`:** an `-engine` flag and a pitch view from `LiveFrames` ([note](../handoffs/ui--tick-career-and-pitch-view.md)); statistics in reports and the live match ([note](../handoffs/ui--match-stats.md)).
- **`squad`:** later, workload from distance run instead of minutes played (phase 4).
- **`competitions`:** play-off match rules are reviewed and accepted (a tie is knockout to penalties; the link's lower division supplies the squad rules). Extra time before penalties would need the reserved `Resolution` value.

## Backlog

- **Career goal level and `simple`'s mentality** (accepted [match--career-goals](../handoffs/match--career-goals.md) and [match--simple-mentality](../handoffs/match--simple-mentality.md), one `simple.ModelVersion` bump; the August-to-May calendar (`content.LeagueVersion` 7) gives more rest, so measure on it): career league matches score 2.2–2.3 goals on both engines against 2.6–2.9 real, so calibrate against a career-like `enginetest` profile (`TestBalanceCareerEngines` is the check), keeping the career home/draw/away split; and give `simple` tick's trade-off shape (attacking within ±0.05 of balanced at equal teams, defensive at least level for the underdog). `tick`'s career goals follow in its own bump.

- **Change the formation during a live match** (accepted [match--live-formation](../handoffs/match--live-formation.md)): a `CommandSetRoles`, an `EventFormationChange`, current roles in the view, both engines, the AI, the replay log. A contract change: notes to `ui`, `balance`.
- **Tired starters of a carried-over or planned lineup** (accepted [match--tired-starters](../handoffs/match--tired-starters.md)): a condition threshold owned here and `MatchdayLineup.Tired`; small, and independent of the calibration above.

- **AI in-match decisions:** the AI opponent reacts at half time and after goals (substitutions, mentality), through the same decision types the manager uses. Deterministic, and replayable from the live-match log.
- **Delegate lineups to the assistant again:** a way for the manager to let the AI pick every week after having submitted a lineup or saved a team plan (today a submitted lineup carries over until replaced, and a saved plan cannot be cleared). Needs a stored "delegated" choice, so a save schema bump.
- **Auto-resolving batches** of rounds that involve no user fixture, led by `competitions`, which owns `Continue`.
- **Statistical balance:** answer `balance`'s notes about goal rates, home advantage and upsets.
- **Formations and roles** beyond GK/DF/MF/FW, when selection needs them (roadmap phase 4 for `tick`).
- **Explain lineup readiness:** player eligibility is done (`World.SquadEligibility`, shared with `SubmitLineup`, tested for an injury after the preview). Left: structured reasons a whole lineup is rejected (duplicate selection, missing goalkeeper, bench too long) as typed errors rather than messages, so `ui` can render them; suspensions from `competitions` will add an `Eligibility` value.

### AI/player rule parity audit

- **P2 — Equal management capabilities (PAR-08):** extend the existing AI in-match/delegated-lineup items to cover live and background fixtures through the same legal commands and decision opportunities. AI currently always starts Balanced and never substitutes; a human can do both. Replay deterministic AI choices independently of user stepping frequency and verify minutes, fatigue, injuries and saved delegation. See [audit](../ai-manager-parity.md#par-08-only-the-human-currently-uses-in-match-management).
- **Done — engine choice independent of controller (PAR-09):** one engine per career, saved, for every fixture; see [progress](../progress.md#match-one-engine-per-career-live-frames-done). A future cheaper tier for background matches needs saved eligibility and `balance`'s evidence first.
