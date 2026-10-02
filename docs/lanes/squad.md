# Lane: squad

The clubs' people and money between matches: contracts, wages and the ledger, the transfer window, player condition and recovery, development, retirement and youth intake, and the AI decisions about renewals, signings and bids.

## Owns

- Modules: `internal/employment`, `finance`, `transfers`, `medical`, `players`.
- AI: `internal/ai/contracts.go`, `internal/ai/transfers.go`.
- App workflows: `internal/app/contracts.go` (the contract year), `lifecycle.go` (the player year), `money.go` (wages, gate receipts, the ledger), `transfers.go` (the window and completions).
- Versions: `ai.ContractsVersion`, `ai.TransfersVersion`, `medical.Version`, `players.DevelopmentVersion`, with their goldens. `worldgen.YouthVersion` is `data`'s. Youth generation changes go through a note.

## Rules for this lane

- **A feature includes its whole footprint:** module changes, the `app` workflow, events, snapshot fields, restore validation and the schema version. Adding these to the [hub files](../../AGENTS.md#hub-files) is part of your work. Don't leave it to `data`.
- **Every player-visible feature gets a `ui--*.md` note** in the commit that lands it. Name the commands, queries, inbox kinds and any new `Continue` stop. Agree a new stop with `competitions`, which owns `Continue`.
- **Cross-module commits** go through `app`: validate and plan everything, then apply every module in one step that cannot fail. Transfers (`market.commit`) are the model to follow.
- **Money is conserved.** Balances are derived from ledger entries, never assigned. Every movement between clubs is a pair of entries that sums to zero, and every entry names what justifies it (a fixture, an offer, a contract).
- **Long-run proofs.** Rules that change the population or money over time need multi-season tests: squads stay legal and full, money is conserved, and no task is left behind. `TestSquadsStayLegalAndBalancedOverTheYears` is the model. Ask `balance` for a longer sweep when a rule is tuned.

## Depends on

- **`data`:** player identities, generated attributes and youth intake content, events and inbox messages.
- **`match`:** condition drain and minutes played arrive through `resolve.go`. Anything that changes who can play (injuries) needs a `match` note for selection eligibility.
- **`competitions`:** the calendar, season transitions and `Continue` stops.
- **`ui`:** squads, contracts, the market, finances and every decision the manager makes.
- **`balance`:** its notes about markets that stall, balances that drift, or squads that thin out.

## Now

Delivered the [passive-manager investigation](../squad-market-investigation.md) and daily recruitment role fallback (`ai.TransfersVersion` 7). Clubs try vacancies by descending need and role order until one bid or signing can be staged. Clock, reserve, listing, consent, capacity, save/retry and multi-season regressions pass; balance is asked to rerun all three market sweeps before further supply/reserve decisions.

Refreshed [medical measurements](../squad-medical-calibration.md) for generation v9 and the football year. Retain medical v4: three-week breaks restore starters while four-day turnarounds still reduce condition. Separate default-calendar and weekly-rest regressions preserve availability and fatigue under congestion. Balance is asked to refresh the matched rotation comparison before any further tuning.

Delivered cup prize postings: `finance.KindPrize` 6 pays each entrant its pinned stage award once at the edition's season end, atomically with `SeasonEnded`. Restore checks the amount, club, exit fixture and final kickoff; schema 32 explicitly refuses prior saves. Final-boundary reloads, cohort overflow/retry and four-year simultaneous-cup accounting are covered. UI, competitions and data receive review handoffs; balance is asked to remeasure the economy and transfer-v7 sweeps with prize income included.

Proposed the [joint economy design](../squad-economy-design.md): division-aware gates and explicit weekly operating costs, with rates tied to the agreed strength gap and post-prize measurements. Data receives `data--division-economy-design.md` to agree the content shape and parameters before implementation. PAR-02 and PAR-06 await shared decision timing with competitions. The medical squad view is the next independent backlog unit.

## Backlog

- **Injuries, next steps:** injury incidents from the engine (`Injuries` capability, forced substitutions) replacing the exposure roll; injury types and recurrence; AI transfers and renewals that allow for injured players (a long layoff at a thin position is now common enough to matter); a medical view of the squad. The v1 rules and the v4 calibration are delivered. If balance's rotation comparison shows a club that never rotates is not punished, or an injury crisis spirals, revisit the drain slope and `InjuryFatigueStep` together.
- **A thin market leaves a broke club short:** a club whose transfer budget is zero (balance below its wage reserve) cannot buy a listed surplus player and loses the race for the last free agent at a position to lower-numbered clubs (`aiActions` runs in club ID order), so it can end a window short while another club holds an unsold listed surplus. The next player year refills it with youth, as it does any club below its roster count. Consider letting a vacancy club buy a listed player below its budget, or ordering the free-agent race by need; never an AI-only insolvency exemption (PAR-05). `TestSquadsStayLegalAndBalancedOverTheYears` tolerates two missing players. Historical reproduction: `userWorld(7, 3)`, year 7, club 14 (one DF and one FW short, balance below its wage reserve). Version-6 investigation: passive seed 7, club 19/year 3 and club 20/year 10; seed 2026, club 13/year 10 (each one FW short with zero reserve budget but cash-affordable listings). All 40 missing recruiting-manager roles lack free agents at the close; 31 also lack a same-position listing. Daily role fallback is delivered in TransfersVersion 7; remeasure positional supply and reserve policy after balance reruns the sweeps.
- **Overall with the new attributes:** once data lands the five match attributes, decide with `balance` whether they enter `keyAttributes` (it moves wages, valuations and selection).
- **Long-run budget pressure (accepted `squad--money-only-grows`):** recurring costs and division-aware income need a joint economy/content design with data, including the delivered cup awards and balance's requested refresh. Target meaningful spending pressure and a promotion reward without an AI insolvency exemption; ask balance to verify the agreed ranges.
- **AI money:** AI clubs use their balance in renewals and signings, sell to raise money, and list declining players. Balances diverge over decades because AI clubs spend at most one upgrade a year, and money has no sink: late in a career rich clubs meet any selling price, so stars move more (5-6 of the 16 best per window by year 30).
- **Generation/development coupling:** generation v9 consumes `players.Develop`; coordinate any `DevelopmentVersion` change with data's worldgen version and dependent goldens. Review youth-floor growth and division-aware intake jointly with data before moving development behavior.
- **Player potential** and development driven by minutes played. `MatchOutcome.Participants` already supplies minutes; agree with `match` and `data` how seasonal usage is retained and consumed without counting a retried result twice.
- **Board feedback** and a transfer budget (club governance).
- **Negotiation:** counter-offers, players refusing terms, and bids below a listed player's asking price.
- **Listings across windows:** let the manager keep a player listed from one window to the next.

- **Contract planning view:** expose each squad player's expiry, renewal eligibility and current demand, with the squad positions at risk if contracts lapse. Dates and costs come from app/domain queries; `ui` can use them in the decision overview and season review. The view must distinguish a forecast from a guaranteed agreement and remain read-only.
- **Committed finance forecast:** show the next contract period's scheduled wage obligations and confirmed income separately from uncertain receipts, with a breakdown by player or source. Use overflow-checked money and known calendar dates; do not count speculative transfers as funds. Coordinate with `competitions` on prize commitments and `ui` on presentation; verify forecasts against ledger postings in an unchanged scenario.

### AI/player rule parity audit

Evidence, reproductions and acceptance criteria are in [the 2026-09-29 audit](../ai-manager-parity.md). These proposals extend the money, negotiation and thin-market work above.

- **P1 — A competitive free-agent opportunity (PAR-02):** replace `holdBack` and the half-window AI signing ban with common decision windows, coordinated with competitions/ui. Keep any assistance explicit and saved; preserve a usable human recruitment opportunity.
- **P2 — Common transaction workflow and fuller AI choices (PAR-05):** give either actor the same offer/sign/release legality and costs; make one-upgrade, one-open-bid, listed-only and wage-reserve restrictions AI policies or explicit shared rules. Allow AI competition for targets and repeat recruitment where justified; extend the existing AI-money work rather than giving broke clubs exemptions.
- **P2 — Renewal timing (PAR-06):** schedule AI renewals before expiry through the same terms/effective-wage validation as human renewals, with optional delegation. Agree decision/expiry ordering and transfer response clocks with competitions (PAR-07); hand the resulting app contract to ui.
