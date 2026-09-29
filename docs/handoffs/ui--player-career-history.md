---
to: ui
from: data
status: open
blocking: no
created: 2026-09-29
---

# Player club history is ready for the profile pages

## Why
Your blocking request `data--player-career-history` is delivered: every player's past and current clubs, which survive save/load and journal trimming.

## What exists
- `app.World.PlayerCareer(ids.PlayerID) ([]app.CareerSpell, bool)` ([internal/app/careers.go](../../internal/app/careers.go)): the player's spells, oldest first; false for an unknown player. A player who never had a club (none yet in generated worlds) has no spells.
- `CareerSpell` embeds `careers.Spell` ([internal/careers/careers.go](../../internal/careers/careers.go)) and adds `ClubName`:
  - `Club`, `From`, `Joined`: `JoinedAtStart` (on the books when the career began: `From` is the career's start, not a join date, so say "before <date>"), `JoinedYouth`, `JoinedFree` (signed as a free agent), `JoinedTransfer` (bought; `Fee` is what the club paid, a `money.Money`).
  - `Until`, `Left`: `LeftNot` (current: `Current()` is true, no `Until`), `LeftTransfer` (sold to the next spell's club, for its `Fee`), `LeftExpired`, `LeftReleased`, `LeftRetired`.
  - Format `From` and `Until` with `w.Calendar().Format(...)`.
- `internal/careers` is already allowed in `cmd/play` and `cmd/web` (`boundaries_test.go`).

## What is needed
On the player profile in both `cmd/play` and `cmd/web`, a club history: one row per spell with the club, from and until dates, how he joined (with the fee for a transfer) and how he left. Retired and free players keep their history.

## Done when
A player bought in the transfer window shows both clubs and the fee on his profile in both clients, before and after save/load.
