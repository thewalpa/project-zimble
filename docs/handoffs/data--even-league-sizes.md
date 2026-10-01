---
to: data
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# Leagues may have any even size; content should say so

## Why
Competitions no longer limits a league to eight clubs (competitions backlog). Your content still accepts sizes that competitions rejects, and a few content rules assume nothing about the football year's length.

## What exists
- `competitions` accepts any **even** number of league entrants from 2 to `competitions.MaxLeagueEntrants` (128: the double round-robin's 2(n-1) rounds fit in a `Round`), and rejects an odd size, which would need byes ([competitions.go](../../internal/competitions/competitions.go)). Eight-club schedules are unchanged.
- `TestLeaguesOfOtherSizesPlayConsecutiveSeasons` ([season_test.go](../../internal/app/season_test.go)) runs two seasons with divisions of 10 and 6 (same world, same fingerprint): play-offs, cup and a save in the middle.

## What is needed
- `content.League.Validate`: reject an odd `Entrants` (and anything above `MaxLeagueEntrants`, if you'd rather not import the bound, 128). Today an odd league passes content and fails only at world creation with the competitions error. `League.Rounds()` is right only for even sizes.
- Nothing else is required. When you add divisions of other sizes, two things are unchecked at load. First, `Division.Clubs` must sum to the leagues' `Entrants` in block order (load already checks the total). Second, the football year: with weekly rounds, a league above about 22 clubs plus the cup runs past the contract-year end, which only `TestSeasonsStayInsideTheContractYear` checks, and only for the built-in content. If you want that enforced at load, say so here and I'll take it (it involves squad's contract-year and window dates).

## Done when
An odd league size is rejected by `content` validation, with a test.
