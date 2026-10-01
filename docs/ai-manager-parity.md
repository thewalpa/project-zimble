# AI manager and player rule audit

Reviewed 2026-09-29 against `80ded24` (the latest pulled main, including seller protections, the free-agent grace period, injuries, promotion and nationalities). This is a code and documentation audit, not a new balance sweep. Existing measurements in [balance.md](balance.md) are versioned historical results, not measurements of this revision.

The target is [the architecture's AI contract](architecture.md#ai-and-information-access): human and AI managers use the same commands, football validation and club knowledge. They can make different decisions. A cautious transfer budget or a seller refusing a bid is a policy; extra generated players or a different legal squad limit is a rule difference. Convenience pauses need not consume game time. Optional assistance should be explicit, saved and independently testable.

Priorities below are proposed backlog order, not changes to the lanes' current work. **P1** closes present rule differences; **P2** closes capability and timing gaps; **P3** prevents planned features introducing new differences. Each lane's backlog links here for evidence and acceptance criteria.

## PAR-01: youth replenishment depends on who manages the club

**P1 — squad; data supplies the generation/content contract.** In [`playerYear`](../internal/app/lifecycle.go), all clubs receive a youth replacement for each retiree. Only clubs with `c.ID != w.userClub` also receive youth for existing vacancies below each position's `Quota.Count`. These players are generated and contracted automatically at the normal youth wage; they have no recruitment fee. An AI club that sold a player and failed to replace him gets a replacement the human club would not get. This is an AI advantage, not merely delegated recruitment.

**Avoid it:** define one academy intake rule for every club, independent of controller. If legal-squad protection remains necessary, use the same explicit emergency recruitment workflow, costs and eligibility for all clubs. Keep discretionary recruitment separate from generating new people; changing intake requires agreement with data on youth/content versions and population effects.

**Acceptance:** from otherwise identical seeded snapshots with a vacancy above the legal minimum, swap only the club's controller and run the player-year cohort. Intake entitlement, positions, contracts and fees must agree. Cover retirements separately and verify save/load, retries and population stability. The branch can be inspected directly; `TestPlayerYear` currently covers replacement of retirees, not this controller swap.

## PAR-02: the free-agent market grants the player exclusive access

**P1 — squad; competitions and ui support a replacement decision flow.** [`contractYear` and `holdBack`](../internal/app/contracts.go) allocate free agents to AI roster targets but only the human club's minimums, then leave up to four of the best above-minimum AI signings unsigned. [`freeAgentGrace` and `aiActions`](../internal/app/transfers.go) prohibit AI free-agent signings until halfway through the 28-day default window; `fillSquads` later fills AI vacancies at closing. `SignPlayer` lets the human sign immediately, outside the window too when no round is pending. The hold-back and grace also run in AI-only careers. `TestFreeAgentPoolAtTheWindowsOpen` deliberately protects the reserve; the earlier empty-pool complaint has already been addressed and should not be filed again.

**Avoid it:** give every club a common recruitment interval and decision opportunity before a batch of offers is resolved. Pause the human interface without giving it extra simulated days. Route optional assistant recruitment through the same workflow, allowing the manager to choose a target squad shape. If retaining the reserve/grace as an accessibility option, name and persist it as assistance rather than treating it as the neutral ruleset. Do not simply restore instantaneous AI consumption of the whole pool.

**Acceptance:** equivalent offers at the same instant compete under the same ordering and acceptance rules for either controller; no candidate is reserved because an actor is human. Test opening, mid-window, closing and out-of-window free-agent opportunities, with and without assistance, including a manager signing the reserved players. Coordinate with the existing free-agent sweep request.

## PAR-03: AI squad policy is embedded in legal capacity validation

**P1 — squad.** [`hasRoom`](../internal/app/contracts.go), called by both `SignPlayer` and transfer completion, branches on `userClub`. The human can fill any position up to `SquadLimit` (25 by default). An AI upgrade is refused while any position exceeds its roster target; conversely an AI positional vacancy returns true without checking total squad size. AI `market.sign` and annual employment plans do not use the same admission checks as the public signing command. The latter observations are validation gaps; this audit does not claim ordinary generated AI squads already exceed 25.

**Avoid it:** make roster maximums, minimums and availability-based eligibility shared football rules. Move desired position counts and the preference to sell surplus before upgrading into the AI planner. Use the common capacity validator for purchases, free signings and automated replenishment, with staged changes included.

**Acceptance:** a 21-player squad with one surplus accepts the same otherwise legal signing for either controller. A full 25-player squad with a positional vacancy rejects another signing for both. Exercise each entry point, atomic failure and restore validation, not just `hasRoom` in isolation.

## PAR-04: transfer consent is not revalidated on both completion paths

**P1 — squad.** [`ai.Joins`](../internal/ai/transfers.go) models a star refusing to join a weaker club. [`market.candidates`](../internal/app/transfers.go) checks it when AI bids are chosen; `transferRun` checks it again through `ai.AcceptBid` when an AI seller answers. A human seller's `RespondToOffer(Accept: true)` calls `market.complete`, which does not check it. With up to three days to answer, changed squads can invalidate the player's willingness without preventing completion. Current `TestAStarRefusesAWeakerClub` covers the human buyer and initial AI bids, not a human seller answering after circumstances change.

**Avoid it:** separate player willingness from the seller's price, settling-in and replacement preferences. Revalidate shared player consent against the staged current squads in `market.complete` for every seller; retain seller preferences in the decision policy. Define whether consent is binding when agreed or conditional until completion, and apply that choice to both controllers. Do not leave one path implicitly binding and the other conditional.

**Acceptance:** create a valid AI bid for a listed human-club star, change a club's squad average so `Joins` becomes false, then accept before the deadline. Repeat with an AI seller. Both paths must produce the same refusal/collapse policy without moving employment or money; include same-cohort squad changes and save/load. This is a code-path finding; the new scenario is proposed regression coverage, not a test already run.

## PAR-05: market participation and spending differ beyond football rules

**P2 — squad.** [`aiActions`, `candidates` and `MakeTransferOffer`](../internal/app/transfers.go) give the AI one outstanding bid, exclude targets with any other open bid, allow at most one discretionary upgrade per window, and restrict subsequent vacancy purchases to listed players. Humans can place several bids, compete for already-bid targets and buy several upgrades. AI only bids for human players who are listed, while human and AI buyers may approach unlisted AI players. [`ai.TransferBudget`](../internal/ai/transfers.go) reserves 26 weeks of wages; the human command checks only cash for the fee. Neither renewal nor free signing currently enforces a common forward wage-affordability limit, and wages are charged to both controllers.

**Avoid it:** keep conservative spending, prices, settling-in and willingness to sell as visible AI preferences, not special legal privileges. Give the AI a planner capable of repeated upgrades, competing bids, releases with the same payoff, and out-of-window free-agent recruitment. Use one availability/listing policy for all sellers; an optional human 'do not disturb' setting should be explicit. Decide with the existing board-budget work whether wage reserves are shared governance constraints or discretionary caution. Never add an AI-only insolvency exemption to solve the thin-market backlog.

**Acceptance:** replay the same proposed offers and releases through either actor's application workflow and compare legality, costs and obligations. Then measure AI policy differences separately. Cover several purchases, competing bids, unlisted targets, zero/negative funds and outstanding commitments. Different chosen actions and seller asking prices need not become identical.

## PAR-06: AI renewals have a special deadline execution path

**P2 — squad and competitions.** [`contractYear`](../internal/app/contracts.go) renews qualifying AI players inside the expiry cohort itself; the human must have called `RenewContract` before that cohort or the contract expires. A human renewal immediately replaces the wage for subsequent weekly runs, while the AI keeps the old wage until expiry. The human can also wait, so this is a timing and automation difference, not proof of an AI wage subsidy. The two paths independently build employment changes instead of sharing the final-year, offer and effective-date workflow.

**Avoid it:** schedule AI renewal decisions during the same final-year opportunity available to the human, and let optional human delegation submit those same intents. If extensions should begin after the existing term, model that effective date for everyone; otherwise make the immediate wage change equally visible. Process due decisions before expiries under a documented common ordering.

**Acceptance:** identical renewal terms submitted at the same decision boundary have the same acceptance, expiry and wage postings for either actor. Test the last legal instant, a wage run at the deadline, a save immediately before expiry, declined renewals and emergency minimum-squad recruitment. Preserve atomic task commits and idempotent commands.

## PAR-07: decision opportunities live in clients, response clocks depend on actor

**P2 — competitions; squad and ui coordinate.** [`Continue`/`handleCohort`](../internal/app/continue.go) stop for fixture rounds, but not contract expiry, window opening or incoming bids. [`cmd/play`'s `advance`](../cmd/play/main.go) and [`cmd/web`'s `advance`](../cmd/web/server.go) compensate with their own expiry warning and transfer-run stepping. A direct `World.Continue` call across a deadline can therefore expire an unanswered human offer while AI clubs answer automatically. This is not a claim that the normal clients lack warnings. [`market.bid`/`managerDeadline`](../internal/app/transfers.go) also gives AI sellers the next run and human sellers up to `ResponseDays` (three by default), allowing different timing of completion and use of sale proceeds.

**Avoid it:** provide a shared application decision-stop/next-decision query contract and make all clients and headless managed careers use it. Separate a UI pause from elapsed game time. Define a common offer-response/completion schedule, or explicitly document variable response time as a manager decision available to both actors. Acknowledged stops must resume deterministically without repeating forever.

**Acceptance:** one long managed Continue and equivalent small steps encounter the same decisions before their deadlines, including after restore. Compare renewal, window opening, rival bids and response deadlines at the same instant. If background auto-resolution is enabled, it must not bypass these stops. AI-only careers must still progress without a UI.

## PAR-08: only the human currently uses in-match management

**P2 — match; ui exposes delegation.** [`ai.SelectTeam`](../internal/ai/selection.go) always chooses Balanced and makes no in-match decisions. [`simulateBatch`](../internal/app/resolve.go) advances sessions without submitting AI commands; [`MatchDecision`](../internal/app/live.go) accepts human substitutions and mentality changes. AI opponents and AI-only matches therefore retain starters while the human can substitute, affecting fatigue and injury exposure as well as tactics. AI selections are freshly chosen each match, while a human submission carries over until replaced; `SuggestLineup` is available manually, but sustained delegation after submission is absent. These are capability/automation gaps, not evidence that AI players receive special match physics.

**Avoid it:** extend the existing AI in-match and delegate-lineups backlog items. Run deterministic AI choices through the same legal match commands and decision boundaries in live, replayed and background matches. Make ongoing lineup delegation a saved human option using the same selector and availability facts. AI decisions must not depend on how often the user advances or watches the match.

**Acceptance:** both sides obey the same substitution limits and eligibility, AI substitutes receive correct minutes/condition/injury exposure, and equivalent decision histories produce identical outcomes live, fast-forwarded and after restore. Cover the carried lineup, injury replacement and delegation transitions.

## PAR-09: planned engine split could make watching change football rules

**P3 — match, with balance and data.** The [match roadmap](lanes/match.md#tick-engine-roadmap) proposes `tick` for the human's live match and `simple` for background games. Currently [`world.go`](../internal/app/world.go) and [`save.go`](../internal/app/save.go) instantiate `simple` for every career fixture: there is no current engine split. The existing [synthetic comparison](balance.md#match-engines-tick-against-simple) shows material engine differences, so the proposed split must not be described as only presentation.

**Avoid it:** choose a career's football model independently of whether a club is human or a fixture is watched. Prefer one engine for interacting competitions, with optional presentation frames. If simulation tiers become necessary, define and save their eligibility at a stable boundary and require parity measurements before using them in the same competition.

**Resolved by match (2026-09-30):** one engine per career, chosen at creation (`Config.Engine`: `simple` by default, or `tick`) and pinned by the save; it plays every fixture, watched or not. No tier split exists. `TestTickLiveMatchEqualsDirectResolution` and `TestLiveFrames` check that watching and reading frames leave the result unchanged; `TestTickCareerSavesAndContinues` checks that a restored career keeps its engine and continues identically. See [progress](progress.md#match-one-engine-per-career-live-frames-done).

**Acceptance:** watch/skip must not select different rules for an otherwise identical fixture and decisions. Before permitting a mixed tier, compare points, goals, upsets, penalties, workload and injuries over matched seasons; engine identity/version must survive replay and restore. Do not claim statistical equivalence implies identical per-fixture output across engines.

## PAR-10: shared knowledge and durable actor provenance are future safeguards

**P3 — data; squad and match consume the contracts.** AI recruitment reads true profiles via `freeAgentPool`/`market.candidates`, but human [`Squad`](../internal/app/summary.go), [`FreeAgents`](../internal/app/contracts.go) and [`PlayerProfile`](../internal/app/views.go) queries also expose exact ratings and demands today. There is no demonstrated hidden-information advantage yet. Adding scouting only to human screens would create one. Human commands are tied to `userClub`/`checkCommand` while AI tasks stage module plans, despite the architecture's shared-command goal; sharing low-level `employment.Plan` is not sufficient to ensure the same football checks.

**Avoid it:** before adding uncertainty, define club-scoped observations for both UI and AI inputs, with separate authoritative match/development inputs. Support actor/club-aware application intents and shared validation (squad/match own their rules). Data should review command/task provenance, event facts and save validation when those workflows are unified. Do not make autonomous AI tasks impersonate the human club or change it temporarily.

**Data foundation (2026-10-01):** `World.ObservePlayers(club, requested)` now supplies detached, canonically ordered `PlayerObservation` rows tagged with observer, revision and query instant. The policy still reveals exact information to every club. Existing human player rows share its underlying projection; tests cover controller parity, read-only queries, separation from match inputs/results and lifecycle/restore. Adoption by match, squad and UI is requested in their `--club-observations` handoffs. No uncertainty or saved knowledge state was added, and this does not resolve the remaining actor/workflow or consumer-adoption work.

**Acceptance:** the same club and knowledge state reveal the same recruitment facts to both controllers; hidden truth cannot reach either. Shared intents retain actor/club identity, atomicity, deterministic task ordering, retry semantics and restore checks. Version any new authoritative assistance, delegation or observation state.

## Rules already shared and limits of the audit

Both controllers currently use the same career match engine and match rules, condition drain/recovery, injury exposure and emergency availability fallback, aging/retirement model, wage ledger, gate receipts and competition schedule/results. Transfer completion already shares fee conservation, seller minimums, window bounds and one-move-per-window checks. The injury fallback that permits injured players when a team cannot field an XI is explicit and shared; it should not be filed as an AI-only exemption.

An AI seller asking more for a star, refusing to resell a recent signing, or preserving replacement time is a manager preference that a human can also exercise by refusing an offer. Player consent, squad capacity and public deadlines need common validation. Prioritize PAR-01 through PAR-04, then improve shared timing and AI choices. New injury, attribute and 32-club sweep requests remain separate balance work; this audit does not claim their measurements are complete.
