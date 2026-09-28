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

**Injuries** (the architecture's first sustainable career): `medical` state and recovery, availability, and a `match` note for selection eligibility. Not started.

Done: the transfer list and AI transfers. Clubs list players at an asking price; AI clubs buy upgrades, list the players these replace, and fill vacancies from the list first; the manager lists with `ListPlayer` and gets AI bids only for listed players (see progress.md, "squad: the transfer list and AI transfers"). Those delivery notes are closed. Current incoming requests are in `squad--match-attributes.md`, `squad--free-agent-pool.md`, `squad--star-churn.md` and `squad--command-record-hub.md`; answer them under the normal handoff rhythm.

## Backlog

- **AI money:** AI clubs use their balance in renewals and signings, sell to raise money, and list declining players. Balances diverge over decades because AI clubs spend at most one upgrade a year.
- **Player potential** and development driven by minutes played. `MatchOutcome.Participants` already supplies minutes; agree with `match` and `data` how seasonal usage is retained and consumed without counting a retried result twice.
- **Board feedback** and a transfer budget (club governance).
- **Negotiation:** counter-offers, players refusing terms, and bids below a listed player's asking price.
- **Listings across windows:** let the manager keep a player listed from one window to the next.

- **Contract planning view:** expose each squad player's expiry, renewal eligibility and current demand, with the squad positions at risk if contracts lapse. Dates and costs come from app/domain queries; `ui` can use them in the decision overview and season review. The view must distinguish a forecast from a guaranteed agreement and remain read-only.
- **Committed finance forecast:** show the next contract period's scheduled wage obligations and confirmed income separately from uncertain receipts, with a breakdown by player or source. Use overflow-checked money and known calendar dates; do not count speculative transfers as funds. Coordinate with `competitions` on prize commitments and `ui` on presentation; verify forecasts against ledger postings in an unchanged scenario.
