---
to: data
from: match
status: open
blocking: no
created: 2026-09-28
---

# Player attributes for the tick match engine

## Why
The new `tick` engine (`internal/matches/tick`) moves every player and the ball. Its first model gets by with the six ratings players have today, but the next phase needs skills the six don't cover: ball control, aerial play, physical duels and positioning. The lane rules say to ask for attributes rather than derive them inside an engine. Today the engine stands in for dribbling with the mean of Passing and Pace.

## What exists
- `players.Attributes` (squad lane) and `matches.Ratings` carry Goalkeeping, Defending, Passing, Finishing, Pace and Stamina, all on the 1–100 scale.
- `internal/app/resolve.go` (`World.candidate`) copies them into `matches.Ratings`.

## What is needed
Nothing yet. We'd like to agree on the shape before match-lane phase 4 (see [docs/lanes/match.md](../lanes/match.md#tick-engine-roadmap)). Our proposal: five more attributes on the same 1–100 scale, generated per position like the existing ones.

| Attribute | Used for |
| --- | --- |
| Dribbling | keeping the ball against a tackle, first touch |
| Heading | winning and directing headers |
| Strength | shoulder-to-shoulder duels, holding off a defender |
| Acceleration | the first steps of a sprint (Pace stays top speed) |
| Positioning | off-ball runs and marking choices |

This touches `players` (squad lane: development and ageing curves), content and worldgen (your generation, `worldgen.Version`), the save schema, and `matches.Ratings` (ours: additive, and `simple` would ignore the new fields). Please answer with `question` if you'd rather scope it differently, for example fewer attributes, or derived "composite" ratings owned by `players`. Let us know if `squad` should be asked too.

## Done when
A generated world's players carry the new attributes, they survive a save round trip, and `World.candidate` can copy them into `matches.Ratings`.
