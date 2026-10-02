---
to: ui
from: competitions
status: open
blocking: no
created: 2026-10-02
---

# Pause for a season review before the season ends

## Why
"Play the rest of the season" currently runs straight through the final whistle: the league season ends at its last kickoff (same instant, after the last round is resolved), the next one exists, and the table is history. A review screen needs a pause that is still before the rollover and before any contract deadline.

## What exists
Opt-in, so nothing that calls `World.Continue` changes (`internal/app/review.go`):

- `World.ContinueWith(until, app.ContinueOptions{StopAtSeasonReview: true})` is `Continue` plus one stop. It returns `app.SeasonReviewReady` instead of ending the user's league season once that season's last round has results. The clock stays at the final kickoff; the stop grants no simulated time, and it repeats unchanged (no new revision) until acknowledged. `Resolved` carries the batches played on the way, like the other results.
- `SeasonReviewReady{At, Revision, Season, OpenEditions, NextContractYearEnd, Resolved}`. `Season` is the league season to review (its table, `Ranking`, champion and history are the existing queries). `OpenEditions` are other competitions the user's team is still entered in with rounds to play (a cup edition): the football year is not over while any is open, and they continue after the league season ends. `NextContractYearEnd` is strictly after `At`, so a review never offers a renewal that has already expired; renewals, signings and transfers all stay possible during and after the review.
- `World.AcknowledgeSeasonReview(app.AcknowledgeSeasonReview{ID, ExpectedRevision, Season})` records "seen"; the next `ContinueWith`/`Continue` ends the season. A retry returns the recorded result. `app.ErrNoSeasonReview` for any season without a review awaiting (mid-season, another season, acknowledged, ended).
- `World.SeasonReview() (SeasonReviewReady, bool, error)` is the read-only state: a save made at the stop shows the review again once loaded, so the screen can be rebuilt from the world alone. The acknowledgement emits `events.KindSeasonReviewed`; the inbox ignores it.
- A career without a user club never has a review; `ContinueWith` with the option just runs on.

## What is needed
In `cmd/play` and `cmd/web`, call `ContinueWith` with `StopAtSeasonReview` wherever the player advances (including "play the rest of the season" and `advance`), show a review screen on `SeasonReviewReady` (final table and the user's finish, the cup situation from `OpenEditions`, the contract and squad overview with `NextContractYearEnd`), and acknowledge it when the player moves on. Acknowledge automatically in unattended flows (`cmd/simulate -season`) or leave them on `Continue`. `SeasonReviewReady` is a new `ContinueResult`: type switches with a default branch need a case. Add it to `World.AutomaticResults` if you want its `Resolved` batches printed.

## Done when
A managed season played with "the rest of the season" stops once at its end with the table visible, `Continue` after the stop without acknowledging stops again at the same instant, and acknowledging then continuing starts the next season; the same after saving and loading at the stop.
