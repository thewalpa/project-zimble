---
to: squad
from: data
status: open
blocking: yes
created: 2026-09-28
---

# Five new player attributes: development and Overall

## Why
The `tick` engine needs Dribbling, Heading, Strength, Acceleration and Positioning ([data--match-attributes.md](data--match-attributes.md) has the request and data's answer). `players` is yours, so data will only make this change once you agree. Blocking for data's delivery, not for anything you are building.

## What exists
- `players.Attribute` has six values, 0–5 ([internal/players/players.go](../../internal/players/players.go)). `Attributes` is a `[NumAttributes]Rating` array.
- `players.Growth` and `players.Develop` ([internal/players/development.go](../../internal/players/development.go)): one growth curve, with Pace and Stamina declining a point faster from 29. Each attribute makes a -1..1 draw, in attribute order.
- `keyAttributes` sets `Overall`, and `Overall` sets wages (`content.Economy.Wage`), AI valuations and selection.

## What is needed
Answers to three questions. Data then makes one commit that adds the constants to `players.go` together with the content ranges, generation and save changes, because a resized `Attributes` array doesn't compile or validate in pieces.

1. **Constants.** Is it fine to add `Dribbling = 6`, `Heading = 7`, `Strength = 8`, `Acceleration = 9` and `Positioning = 10`, with names `dribbling`, `heading`, `strength`, `acceleration` and `positioning`?
2. **Growth.** Which ageing rules apply? Our proposal: Acceleration declines like Pace. Strength declines one point a year more slowly from 29, at least until 32. Dribbling, Heading and Positioning follow the base curve. `Develop` loops over attributes in order, so the new draws come after the existing six, and those six develop exactly as they do now. Should `DevelopmentVersion` go up? Bumping it makes older saves `ErrIncompatibleSave`, but the schema bump refuses them anyway.
3. **Overall.** We propose keeping `keyAttributes` unchanged for now. Wages, valuations and AI selection then don't move, and seeded careers stay the same. Adding the new attributes to `Overall` can come later, with `balance`.

If you would rather make the `players.go` part yourself, say so. It would have to land in the same commit as data's generation change, so we would pair on one branch.

## Done when
This note says `accepted` and answers 1–3 in an `## Answer` section. Data then closes it with the delivering commit.
