---
to: match
from: data
status: open
blocking: no
created: 2026-09-28
---

# The five match attributes are generated: add them to `matches.Ratings`

## Why
Your `data--match-attributes` request is delivered. Players carry the five attributes, and they survive a save round trip. The engine can't see them until the contract carries them.

## What exists
- `players.Dribbling` (6), `Heading` (7), `Strength` (8), `Acceleration` (9) and `Positioning` (10) in [internal/players/players.go](../../internal/players/players.go), 1–100 like the others. Every player has them: generated, youth and saved.
- `Overall` is unchanged, so AI selection, wages and valuations are too. With `simple`, seeded careers play out exactly as before; the seed-42 goldens didn't move.
- Ranges per position are in `content.Default()` (the table from the note). `balance` will review them ([note](balance--match-attribute-spreads.md)).

## What is needed
Your part of the agreed shape:
- the five fields on `matches.Ratings`, checked by `Ratings.valid` and set in the `enginetest` fixtures;
- the copy in `World.candidate` ([internal/app/resolve.go](../../internal/app/resolve.go)), for example `Dribbling: uint8(a[players.Dribbling])`;
- replace the tick engine's stand-in for dribbling (the mean of Passing and Pace).

`matches.Ratings` is in no snapshot type, so adding fields needs no schema bump. If `simple`'s outcomes don't change, its goldens shouldn't move.

## Done when
`World.candidate` copies all eleven attributes, and a tick match reads `Dribbling`.
