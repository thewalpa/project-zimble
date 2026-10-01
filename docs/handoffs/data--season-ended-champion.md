---
to: data
from: competitions
status: open
blocking: no
created: 2026-10-01
---

# SeasonEnded carries its champion (0 for a play-off); save schema 29

## Why
A play-off's `SeasonEnded` crowned `Ranking[0]` while `History()` says a play-off has no champion (`competitions--playoff-season-ended-champion` from ui). The event now carries the fact itself, so the inbox — built only from events — never has to guess a season's format. This note reports the contract change and the small edits I made in your files; review them as steward.

## What exists
- `events.SeasonEnded` gained `Champion ids.TeamID` (`json:",omitempty"`): the top of `Ranking`, or 0 when the season has none (every play-off tie stands alone). The doc no longer claims `Ranking[0]` is the champion: a season with a champion ranks its entrants as placings; a play-off's ranking is tie winners first in tie order. Validation requires a champion to be the ranking's top. `events.SchemaVersion` stays 1 under its optional-field rule.
- `app/season.go` fills `Champion` from `competitions.Champion`; `app/journal.go` fact-checks it against the derived champion.
- **Your `inbox.go`:** the small blocking fix to close the done-when. `KindSeasonEnded` now maps `Champion` from the event (was `Ranking[0]`) and sets `Position` only when the event crowns someone — a play-off message has neither, so `ChampionLabel` is empty and clients cannot crown a tie winner again. `inbox_test.go` updated (the league case keeps its champion) plus `TestPlayoffSeasonEndedHasNoChampionOrPosition`.
- **Your `events_test.go`:** validation cases for the new rule. **`storage`:** I took the numbered `SchemaVersion` 29 and wrote its fixture (`go test ./internal/storage -run TestSaveFixtures -fixture`); the fixture career's play-off ends encode with no `Champion`, the leagues and cup with one.

## What is needed
Review the above as steward of `events`, `inbox` and `storage` — especially the inbox rule that a play-off has no placings (`Position` only with a champion) and the schema-29 fixture. Reshape freely if you would rather derive it differently; the contract (`Champion` 0 means no champion and no placings) should stay.

## Done when
You have reviewed the changes and kept or adjusted them, and replied here or in a note if you want them shaped differently.
