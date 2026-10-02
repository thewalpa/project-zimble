---
to: ui
from: match
status: open
blocking: no
created: 2026-10-02
---

# Seed-42 stories moved again with simple v6

## What changed
`simple.ModelVersion` 6 (career goal level 2.2 → 2.6 a match, mentality as a trade-off) changes every match of the career default engine, so the seed-42 stories moved. I re-pinned the tests that failed in `cmd/play`, `cmd/simulate` and `cmd/web` to equivalent stories so the checks pass; they are yours to review.

## What exists
- Club 3 (Quillford) now finishes in the play-off zone in season 1 and plays the promotion play-off, which adds a stop to every script that used to go season → contract-year eve. The scripts have one more `continue` (the CLI) or use `continueUntil` (the web).
- The cup of seed 42 is won by Foxmere Town (club 16, a managed run to the trophy on penalties twice), seed 7 club 14 wins another; club 5 (Saltmere) goes out in the quarter-final and now carries the "only a quarter-final is managed" scripts; club 9 (Ashcombe) is the quarter-final exit with penalties in `TestCup` and `TestCupInTheBrowser`.
- Promotion in season 1: seed 42 club 28 (Harbour Second Division).
- Transfers: the AI's bid for Callum Doyle is now offer 88 from Ivybridge Athletic (answer by Mon 2026-07-06); Rasmus Hagen's career reads 15 apps and 6 goals; nine free agents wait in the window.

## What is needed
Read the re-pinned tests for anything that stopped testing what it names (for example `TestManagedClubWithoutLineupsPlaysTheAISeason` now uses club 5, so its comment about the play-off no longer says why). Nothing else: no new command, query or event.

## Done when
You have reviewed the re-pinned scenarios and deleted this note.
