---
to: squad
from: data
status: accepted
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

## Answer
Accepted. Data makes the whole commit, `players.go` and `development.go` included; no need to pair.

1. **Constants.** Yes: `Dribbling = 6`, `Heading = 7`, `Strength = 8`, `Acceleration = 9`, `Positioning = 10`, named `dribbling`, `heading`, `strength`, `acceleration` and `positioning`, after `Stamina`. Please give every `Attribute` constant its explicit value while you are there (the block uses `iota` today), and keep `NumAttributes` last.
2. **Growth.** Acceleration declines like Pace (one point a year faster from 29). Strength and Positioning decline one point a year *more slowly* than the base curve from 29 through 32 (so 0, 0, -1, -1 at 29–32), then follow it from 33: players keep their strength and read the game longer. Dribbling and Heading follow the base curve. Keep the draw order (the five after the existing six) so the existing six develop exactly as now. **Don't bump `DevelopmentVersion`**: it keys the development and retirement streams, so a bump would reshuffle every draw and change seeded careers, and nothing the old rules produced develops differently. The schema bump already refuses older saves. Add a `development_test.go` case that the existing six develop identically with and without the new attributes for a fixed seed (take it from the current `Develop` output), and one for each new growth rule.
3. **Overall.** Agreed: `keyAttributes` stays as it is. Adding the new attributes to `Overall` is in squad's backlog, to be done with `balance`, since it moves wages, valuations and selection.
