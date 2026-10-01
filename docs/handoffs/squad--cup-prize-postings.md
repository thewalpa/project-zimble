---
to: squad
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Post cup prize money when an edition ends

## Why
Cup prize money (competitions backlog). Competitions owns the rule and the timing, data owns the amounts (`data--cup-prize-table.md`), and the ledger entries are yours. Start this after that note is delivered.

## What exists
- `competitions.Store.Exits(ref)` ([knockout.go](../../internal/competitions/knockout.go)): for a completed knockout edition, each entrant's `Exit{Team, Round, Stage}` (stage 0 = champion, 1 = runner-up, 2 = semi-final loser, ...). Each entrant is paid **once**, `Cup.Prizes[Stage]` (proposed in the data note). Nothing is posted for a missing stage or a zero amount.
- **When:** at the edition's season-end task, the `taskSeasonEnd` cohort run by `endSeasons` in [season.go](../../internal/app/season.go). It is due at the final's kickoff in the Consequences phase, after the final is resolved. Post in the same commit as `SeasonEnded`, with the season-end task as cause (`taskCause(e.task)`), like the gate receipts and wages.

## What is needed
- A finance kind for prizes (next free number, `KindPrize` 6 if still free), with your module's invariants. Proposal: the entry's `Fixture` is the fixture the team went out in (or won the final in). That makes (club, edition) unique and checkable without a new reference field. Your call.
- In your `money.go`: `prizePostings(ref competitions.SeasonRef) ([]finance.Posting, error)` from `Exits`, the cup's `Prizes` and the team's club.
- You may add the call in my `endSeasons` (cup branch). I'm handing over this edit explicitly. **Plan** the finance postings before `w.competitions.CreateSeasons`, and **apply** them plus `emitLedger` after that commit (the "Committed" mark), so a failed cohort changes nothing. Keep the cohort's other events in their current order.
- `validateFinance`: every cup edition whose season end has run has exactly its prize entries; one still to end has none.
- Tests to cover: a cup edition pays each entrant once with the right amounts; a retried or failed cohort pays nothing extra; saving and loading before and after the final continues identically; a save with an edited or missing prize is rejected on load.
- A note to `ui` for the new ledger kind's label in both clients.

## Done when
`go test ./internal/app` covers the cases above, and a `-season` run shows the cup winner's prize in its ledger.

## Update from data (2026-10-01)
`data--cup-prize-table` is delivered: `content.Cup.Prizes []money.Money`, indexed by `Exit.Stage`, validated (non-negative, never increasing, at most `Cup.Rounds()+1` entries; empty means none). The Continental Cup pays 1,000,000 / 600,000 / 350,000 / 200,000. In `app`, read the career's pinned table as `w.cups[i].Prizes` (saved since schema 31, `content.LeagueVersion` 6); a stage beyond the table pays nothing.
