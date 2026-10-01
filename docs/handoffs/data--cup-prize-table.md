---
to: data
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# A prize table on each cup definition

## Why
Cup prize money (competitions backlog). Competitions now owns the rule for how far each entrant got. The amounts are content, so they belong on your `content.Cup`. Squad posts the money (see `squad--cup-prize-postings.md`, which waits for this note).

## What exists
- `competitions.Store.Exits(ref) ([]Exit, bool)` ([knockout.go](../../internal/competitions/knockout.go)): for a completed knockout edition, every entrant's `Exit{Team, Round, Stage}` in ranking order. **Stage** counts back from the final: 0 = champion, 1 = runner-up, 2 = semi-final loser, 3 = quarter-final loser, and so on. A stage means the same thing in a bracket of any size.
- The rule: each entrant of a completed edition is paid **once**, the prize for the stage it reached. Payments do not accumulate per round won. A stage with no prize, or a zero prize, pays nothing.

## What is needed
- `Cup.Prizes []money.Money`, indexed by stage (`Prizes[0]` for the champion). Proposed validation in `Cup.Validate`: every amount ≥ 0; `len(Prizes) ≤ rounds + 1` (rounds = log2 of `Entrants()`); non-increasing, so a deeper run never pays less. An empty table is valid and means no prize money.
- Built-in values for the Continental Cup (8 entrants, 3 rounds). Proposal, scaled to the 250,000 gate and about 1.7 million yearly wages: 1,000,000 / 600,000 / 350,000 / 200,000. Choose your own.
- Whatever the pinned definitions in saves need: a `storage.SchemaVersion` bump and fixture if `content.Cup` is saved, and `content.LeagueVersion` if you treat it as covered.

## Done when
`content.Builtin()`'s cup has a validated prize table, invalid tables are rejected in `content` tests, and the squad note can read `Cup.Prizes`.
