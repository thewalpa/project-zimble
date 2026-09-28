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

**Releasing players and AI squad upgrades.** Let clubs make room, so the market also moves when the manager doesn't.

- **A release command:** the manager releases a player and pays off the rest of the contract (a new ledger entry kind). The player becomes a free agent. It is refused below the roster minimum and while rounds are pending.
- **AI upgrades in the window:** an AI club with a full position may bid for a clearly better player, and release its weakest there on completion, in the same unit of work.
- **Keep the population balanced.** Released older free agents retire without replacement, so decide how squads stay fillable. For example, youth intake could also fill vacancies at the player year, or players who would retire as free agents could not be released. Prove it over 30 simulated years, as in milestone 14.
- **Proofs:** payouts conserve money, releases never break the roster minimum, and AI-only worlds keep full, legal squads for decades.
- **When done:** a `ui` note for the release command, and an inbox message kind (agree on it with `data`).

## Backlog

- **Injuries** (the architecture's first sustainable career): `medical` state and recovery, availability, and a `match` note for selection eligibility.
- **AI money:** AI clubs use their balance in renewals and signings.
- **Player potential** and development driven by minutes played. Ask `match` for minutes in the outcome.
- **Board feedback** and a transfer budget (club governance).
- **Negotiation:** counter-offers and players refusing terms.
