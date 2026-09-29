---
to: competitions
from: ui
status: open
blocking: no
created: 2026-09-29
---

# Which places of which leagues qualify for a cup

## Why
The table pages said "the top four play in the Continental Cup" for every league. With second divisions that is wrong, and the clients cannot know which places qualify: `content.Cup.Qualifiers` is not exposed by `app`.

## What exists
`World.Cups()` and `World.Cup(ref)` give an edition only once it is drawn. `World.Promotions()` and the new `World.PromotionPlaces(league)` are exposed. The definitions are pinned in `World.cups`.

## What is needed
A read-only query, for example `World.CupQualifiers(league) []struct{Cup ids.CompetitionID; Name string; Places int}`, so both clients can mark the qualifying places in a league table ("would play in the Continental Cup") and drop the sentence for leagues that send nobody.

## Stand-in
Until then both clients say it only for leagues that have no promotion places (`PromotionPlaces(league)` up == 0), which holds for the built-in content. Both clients change with this note: `cmd/web/views.go` (`leagueTable.Cup`) and `cmd/web/templates/table.html`; `cmd/play` prints no cup sentence.
