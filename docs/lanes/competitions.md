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

**Promotion and relegation.** Each nation gets a second division, and clubs move between divisions at the season end on their final ranking.

1. Design the rule: how many go up and down, and whether it is a direct swap or play-offs. Record it in `docs/progress.md` when done.
2. File `data--second-division-content.md` for the clubs, league definitions and generation. It will change the world fingerprint, so `data` bumps the versions.
3. Meanwhile, build the competitions side. A league season's entrants come from the previous seasons' rankings, not from generation. Validation replays movement from the recorded results, and history stays coherent when a club changes division.
4. Once `data` delivers, wire it together, prove it over several seasons (every club in exactly one league each season, histories intact), and file a `ui` note for tables by division and promotion markers.

## Backlog

- **Auto-resolving batches:** `Continue` resolves rounds with no user fixture without stopping. Agree with `match`, which owns `resolve.go`.
- **Cup prize money:** the rule is here, and the ledger entries go through a note to `squad`.
- **Two-legged ties and extra time:** they need a `match` note for engine support.
- **Registration and eligibility per competition** (the architecture assigns it to competitions), once squads have more than one team.
- **League sizes other than eight** (`competitions.SupportedEntrants`).

- **Season review before rollover:** support `ui`'s "play the rest of the season" flow with an explicit stop after the relevant league and cup fixtures finish, before creating the next season. Define what happens when competitions finish on different dates; distinguish the final match, contract deadlines and rollover so a review screen never promises an already-expired renewal. Coordinate stops with `squad` and `ui`; repeated Continue and save/load must neither skip the review nor create a season twice.
- **Minimum rest across competitions:** extend clash checking from identical kickoffs to a content-defined recovery interval across league and cup fixtures. Ask `data` for the rule and agree the condition implications with `squad`. Scheduling should deterministically find a valid slot or report an impossible calendar, including when cup progression adds fixtures.
- **Disciplinary eligibility:** when `match` emits cards, own accumulation thresholds, bans and which competition's fixtures serve them. Agree card facts with `match` and rules with `data`; send the resulting availability to selection and `ui`. Save/load, retries and season transitions must not apply or serve a suspension twice.
