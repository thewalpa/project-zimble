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

Reviewed the interleaved cup calendar, August-to-May default and age-adjusted generation. Calendar-driven squad tests retain legality, accounting, recovery and staged-consent checks; weekly medical calibration retains its weekly-rest floor, with a separate congestion bound. Next: the passive-manager market investigation alongside the thin-market work. Cup prize postings and long-run budget pressure are accepted below. PAR-02 and PAR-06 await shared decision timing with competitions.

## Backlog

- **Passive-manager market (accepted `squad--passive-manager-market`):** isolate vacancy recruitment choices with a larger free-agent pool and the 24 short active-manager windows; compare need ordering and wage-reserve budgets before changing policy. Unsold fringe listings alone are not a recruitment guarantee.
- **Recalibrate condition for the football year:** the default now has three-week league gaps and four-day cup turnaround (`content.LeagueVersion` 7, `ScheduleVersion` 4). Data measured 2% tired starters and 0.33 players out per club at kickoff before generation v9; refresh measurements with age-developed starting stamina, then retune drain, recovery and incidence together. Keep the weekly medical v4 regression scenario and its rested/congested bounds distinct from the default calendar.
- **Injuries, next steps:** injury incidents from the engine (`Injuries` capability, forced substitutions) replacing the exposure roll; injury types and recurrence; AI transfers and renewals that allow for injured players (a long layoff at a thin position is now common enough to matter); a medical view of the squad. The v1 rules and the v4 calibration are delivered. If balance's rotation comparison shows a club that never rotates is not punished, or an injury crisis spirals, revisit the drain slope and `InjuryFatigueStep` together.
- **A thin market leaves a broke club short:** a club whose transfer budget is zero (balance below its wage reserve) cannot buy a listed surplus player and loses the race for the last free agent at a position to lower-numbered clubs (`aiActions` runs in club ID order), so it can end a window short while another club holds an unsold listed surplus. The next player year refills it with youth, as it does any club below its roster count. Consider letting a vacancy club buy a listed player below its budget, or ordering the free-agent race by need; never an AI-only insolvency exemption (PAR-05). `TestSquadsStayLegalAndBalancedOverTheYears` tolerates two missing players. Reproduce: `userWorld(7, 3)`, year 7, club 14 (one DF and one FW short, balance below its wage reserve).
- **Overall with the new attributes:** once data lands the five match attributes, decide with `balance` whether they enter `keyAttributes` (it moves wages, valuations and selection).
- **Cup prize postings (accepted `squad--cup-prize-postings`):** post the pinned `Cup.Prizes[Exit.Stage]` once per entrant at the edition's season-end task. Add the finance kind, atomic planning/apply, ledger events, restore checks and UI label handoff together. The data dependency is delivered; competitions authorizes its season-end call site.
- **Long-run budget pressure (accepted `squad--money-only-grows`):** recurring costs and division-aware income need a joint economy/content design with data, accounting for cup prizes first. Target meaningful spending pressure and a promotion reward without an AI insolvency exemption; ask balance to verify the agreed ranges.
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
