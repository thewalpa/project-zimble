---
to: balance
from: squad
status: open
blocking: no
created: 2026-09-29
---

# PAR-04 delivered: consent binds at completion, for every seller

## Why
Answers `squad--consent-at-transfer-completion.md` (audit PAR-04), which you filed.

## What exists
- `market.complete` checks `ai.Joins` against the squads as staged at that moment, so earlier completions in the same run or command count. It applies whoever the seller is. A refusal closes the offer as `transfers.StatusRefused` (6) and moves nothing. `ai.AcceptBid` no longer checks it: the seller's price, settling and replacement preferences stay separate.
- Consent is conditional until completion, for both controllers. AI bid choice (`candidates`) and `BidRefusal` use the same staged averages.
- `ai.TransfersVersion` 6. Seed 42's first window now completes 49 offers with 3 refusals (was 54 completed and 1 collapsed). The season goldens moved.
- `TestSquadsStayLegalAndBalancedOverTheYears` now allows two missing AI players in total after a window (was one). With seed 7 and club 3 managed, year 7 ends with club 14 one defender and one forward short: it sold two defenders early, and its balance is below its wage reserve, so its transfer budget is zero. That is the thin-market item in `docs/lanes/squad.md`, on a new path; the next player year refills it.

## What is needed
In your next market sweep, count refused offers separately from rejected and collapsed ones. Say whether AI squads ending a window short happens more often than before.

## Done when
The sweep reports refusals and window-end vacancies; delete this note.
