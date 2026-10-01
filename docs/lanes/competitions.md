# Lane: competitions

The shape of the football year: game time and the task queue, `Continue`, league seasons and cup editions, fixtures and kickoff times, official results, season transitions, and what is derived from them (tables, rankings, champions, history).

## Owns

- `internal/competitions` and `internal/core/sim`.
- `internal/app/continue.go` (the clock, cohort dispatch and stops; also a [hub file](../../AGENTS.md#hub-files) that every lane adds task kinds to), `season.go`, `schedule.go`.
- Versions: `competitions.ScheduleVersion`, with its goldens. League and cup definitions (`content.LeagueVersion`) belong to `data`. Ask for new definitions with a note.

## Rules for this lane

- **Competition work never changes the world fingerprint.** Generated clubs, players and contracts are `data`'s. A new competition takes entrants from the world as it is.
- **Results are official once recorded.** Standings, rankings, champions and history are derived on demand, never stored. A knockout round's fixtures are created from the previous round's winners in the same commit that records them.
- **Time is explicit.** Use `sim.GameInstant` and the calendar, and never the wall clock. Every task is a plain record (kind and payload ID). A cohort is dispatched atomically, and a failed cohort stays queued with the clock unchanged. One `Continue` call must equal several smaller ones, and a save and load at any point must continue identically. Test both for every new task.
- **Stops are a contract with `ui`.** `Continue` returns where the player must act. A new stop condition, or a change to one, needs a `ui` note: both clients step through it.
- **Scheduling clashes:** a team in two competitions must never have two fixtures at the same instant. Check it whenever you add a competition or change timing.

## Depends on

- **`data`:** league, cup and nation definitions, and new clubs for new divisions.
- **`match`:** turns a round into results through `resolve.go`. Knockout rules (extra time, two legs) need engine support: ask with a note.
- **`squad`:** season-level money (prize money, gate receipts) is posted by `squad`'s finance workflows.
- **`ui`:** tables, fixtures, brackets, history and every `Continue` stop.

## Now

Cup prize money, the competitions half: `competitions.Store.Exits` (each entrant's stage in a completed knockout) is the rule, and the inbox's cup stage reads it. Waiting on `data--cup-prize-table` (amounts on `content.Cup.Prizes`), then `squad--cup-prize-postings` (ledger kind, postings in `endSeasons`, validation). When squad delivers, review their edit in `season.go`. League sizes: any even size is in, and `data--even-league-sizes` asks content to reject odd ones. Open from earlier: `ui--auto-resolving-batches` (accepted by ui). `competitions--shared-manager-decision-stops` stays accepted as the PAR-07 item below. Next: the football-year item at the top of the backlog.

## Backlog

- **A football year that fills the year (reported in play, first priority):** with built-in content the leagues play 14 weekly rounds from 9 Aug to 8 Nov 2025. The play-offs follow on 15 Nov and the Continental Cup quarter-final, semi-final and final on 22 Nov, 29 Nov and 6 Dec. After that nothing is played until 8 Aug 2026, about eight months later. Target: league seasons run from August to May, with the play-offs at the season's end. A cup edition is played during a league season (midweek rounds between league matchdays), still drawn from the previous season's final rankings, so edition N plays during season N+1. Decide what season 1 does without a previous ranking: no edition, or one drawn from a seeded order.
  - **Data:** the round spacing (a longer interval, a winter break or dated matchdays on `content.League`) and the cup's timing on `content.Cup`. Ask with a note.
  - **Competitions:** the cup's interleaving, the clash check (and the minimum-rest item below), and the season-end and cup-draw order in `endSeasons`.
  - **Must hold:** everything ends before the 1 July contract-year end and fits around the transfer window. Turn `TestSeasonsStayInsideTheContractYear` into a load-time check (also offered to `data` in `data--even-league-sizes`, since larger leagues can overrun the year).
  - **Versions and saves:** kickoff times change, so bump `competitions.ScheduleVersion` and update its goldens. Existing saves keep their pinned definitions and calendar.
  - **Other lanes:** tell `ui` that a cup tie can fall between league rounds, and tell `squad` and `balance` that the off-season's wage, condition and recovery rhythm will change.
- **Cup prize money (waiting on data and squad):** the rule is delivered (`Exits`). Review squad's `endSeasons` edit when it lands; league prize money by final position could follow the same pattern from `Ranking`.
- **Two-legged ties and extra time:** they need a `match` note for engine support.
- **Registration and eligibility per competition** (the architecture assigns it to competitions), once squads have more than one team.
- **Odd league sizes (byes):** even sizes are in (`competitions.MaxLeagueEntrants`). An odd size needs a bye slot per round and `content.League.Rounds()` changed with it, so coordinate with `data`. A content-driven reason should come first.

- **Season review before rollover:** support `ui`'s "play the rest of the season" flow with an explicit stop after the relevant league and cup fixtures finish, before creating the next season. Define what happens when competitions finish on different dates; distinguish the final match, contract deadlines and rollover so a review screen never promises an already-expired renewal. Coordinate stops with `squad` and `ui`; repeated Continue and save/load must neither skip the review nor create a season twice.
- **Minimum rest across competitions:** extend clash checking from identical kickoffs to a content-defined recovery interval across league and cup fixtures. Ask `data` for the rule and agree the condition implications with `squad`. Scheduling should deterministically find a valid slot or report an impossible calendar, including when cup progression adds fixtures.
- **Disciplinary eligibility:** when `match` emits cards, own accumulation thresholds, bans and which competition's fixtures serve them. Agree card facts with `match` and rules with `data`; send the resulting availability to selection and `ui`. Save/load, retries and season transitions must not apply or serve a suspension twice.

### AI/player rule parity audit

- **P2 — Shared decision opportunities (PAR-06/07):** extend the season-review work with an app-owned decision-stop or next-decision contract for renewals, incoming offers and window opening. `Continue` currently stops only for fixtures while both clients implement financial-deadline stops themselves. Agree with squad how both actors submit before expiry and share response/completion timing; UI pauses should not grant extra simulated days. Test long versus short stepping, simultaneous deadlines, acknowledged stops, background auto-resolution and restore. See [audit evidence and acceptance](../ai-manager-parity.md#par-07-decision-opportunities-live-in-clients-response-clocks-depend-on-actor).
- **P1 dependency — Competitive free-agent decisions (PAR-02):** support squad's replacement for player-only pool reservation/grace with a common decision interval before offer resolution, without auto-consuming the pool before the human can act. Keep AI-only careers unattended and managed careers actionable through the same scheduler contract.
