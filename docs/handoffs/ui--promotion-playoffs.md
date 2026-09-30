---
to: ui
from: competitions
status: open
blocking: no
created: 2026-09-30
---

# Promotion play-offs: new views, decided-only movement wording, and naming gaps

## Why
Promotion play-offs are in: each movement link's boundary places play single-match
ties (level → penalties) between the leagues' seasons and the next, and only then
do the winners go up and the losers stay down. Both clients stepped through the
new stops and results fine after small test-driver fixes (below), but the
play-off fixtures themselves still render as anonymous "round 1" ties and the
legends still say the boundary places "were relegated" when they are now play-off
places.

## What exists
- **Views** in [schedule.go](../../internal/app/schedule.go): `Playoffs() []PlayoffEdition`
  (latest edition of each play-off, competition ID order) and
  `Playoff(ref) (PlayoffEdition, bool)`. `PlayoffEdition` is bracket-shaped like
  `CupEdition` (`Entrants` in `competitions.PlayoffPairings` order, one
  `CupRound`) but deliberately has **no `Champion`** — the winners move up, the
  losers stay down; there is no cup winner. `Schedules()` and `Tables()` do not
  include play-offs.
- **`FixtureInfo.Playoff bool`** marks a play-off fixture (`FixtureInfo.Cup` is
  false for one). `RoundName` currently falls through to `"round N"` for a
  play-off because its format is `competitions.FormatTies`, not `FormatKnockout`.
- **`World.InPlayoff(ref, position) bool`** and
  **`World.SeasonMove(ref, position) (promoted, relegated bool)`** in
  [views.go](../../internal/app/views.go). `SeasonMove` is decided-only: it reads
  next season's entrants, so until the play-offs decide (or for a place that lost
  its tie the other way) both are false. The season-end inbox message is
  rendered at view time, so the same message gains "promoted to the division
  above" / "relegated to the division below" once the ties are played — that is
  intended.
- Play-off matches resolve like cup matches: a level tie goes to penalties
  (`matchRules` in [resolve.go](../../internal/app/resolve.go) builds
  `matches.Rules{Knockout: true}` from the link's lower league definition).
- I made the small test/driver fixes this needed in your files and committed
  them with the milestone; the production UI is untouched. Terminal: the cup and
  history strings, transfer-market bid IDs, continue retiming after the shorter
  cup run, and `TestSeasonEndSaysRelegation` retargeted to a promoted club and
  renamed `TestSeasonEndSaysMovement` (seed 42 has no club whose play-off loss
  relegates it). Web: the same retiming and result swaps; the same rename to
  `TestSeasonEndSaysMovement` (club 20, plain-number wording
  `you finished 2: promoted to the division above.`); `TestCupInTheBrowser`'s
  "Next match" check now lives at the quarter-final's post-match pause (the home
  page's "Next match" panel needs a scheduled fixture and no live matchday; the
  cup is drawn at the play-off end, so right after `season` the page is
  off-season). `cmd/simulate`'s `schedule=v3` fixture string is updated.

## What is needed
- **Name play-off fixtures.** Matchday banners, the home page, fixtures rows and
  inbox result/round labels should say "Promotion Play-off … v …" instead of
  "round 1 v …"; `FixtureInfo.Playoff` and `PlayoffEdition.Name` give the
  pieces. Both clients.
- **Show the ties.** A play-off page or section from `Playoffs()`/`Playoff(ref)`
  (bracket panel like the cup's, without a champion), linked from the history
  listing and from the two divisions' tables. `Schedules()` will not carry them.
- **SeasonEnded wording for a play-off.** The generic league message says
  "ended: champion X"; a play-off has no champion. Say the ties decided the
  places (e.g. "Promotion Play-off 1 decided: …"), and keep it out of the
  champion/history-winner columns (`history` rows currently show
  "Promotion Play-off 1 none" — a correct placeholder).
- **Legends.** "▼ the bottom 2 were relegated" / "▲ the top 2 were promoted"
  describe play-off places now, not direct swaps. Suggest: mark the boundary
  rows as play-off places ("in the play-off", `InPlayoff`) and reserve
  promoted/relegated for what `SeasonMove` reports after the ties. Play's
  `TestPromotionMarksAndHistory` still pins the old wording, so change both
  together.
- **Web vs terminal movement wording** is your call to unify: web renders
  `you finished %d` (plain number), play uses ordinals ("you finished 2nd").
  The clause text matches otherwise.

## Done when
Both clients show a play-off edition's ties with its name, the season-end and
history text does not crown a play-off champion, and the boundary rows'
legends/marks distinguish "play-off place" from decided movement — with the
clients' promotion tests updated to match.
