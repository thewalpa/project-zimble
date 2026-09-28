---
to: data
from: match
status: accepted
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

## Answer
Accepted, all five, in one change. Squad has to agree to its part first (see [squad--match-attributes.md](squad--match-attributes.md)). After that the change goes to the top of data's backlog, right after inbox read state. That is well before the aerial step in your phase 4. We don't want "composite" ratings: a mean of the existing six adds no new information, and that is the stand-in you want to replace.

**Shape.**
- `players.Attribute` gets `Dribbling = 6`, `Heading = 7`, `Strength = 8`, `Acceleration = 9` and `Positioning = 10`. They go after `Stamina`, and the existing values stay as they are. One `Positioning` covers both off-ball runs and marking; the player's position decides which one it means.
- `players.Attributes` is a `[NumAttributes]Rating` array. That makes the constants, content ranges, generation and save one atomic commit: with the constants alone, content ranges would be zero and generated players invalid. Data makes that commit, including squad's rules for `players`.
- **No reshuffle of the existing draws.** The five are drawn after the birth date, at the end of each player's worldgen stream and each youth stream. Names, the existing six attributes, contracts and birth dates stay the same for every seed. `worldgen.Version` goes to the next free number (6: version 5 went to the second divisions) and `streamVersion` doesn't change. `YouthVersion` goes to 2, `content.Version` goes to the next free number (7) and the fingerprint golden moves. `Overall` stays on its current key attributes (squad's call), so wages, valuations and AI selection don't change. Your `simple` engine ignores the new fields, so seeded careers should play out exactly as before. A test will check this for seed 42.
- **Saves:** `storage.SchemaVersion` gets the next free number (17 after the second divisions). Older saves are refused with `ErrUnsupportedSave`. We can't restore values the save never held, and restore must not regenerate them from the seed.
- **Content ranges** to start from (GK / DF / MF / FW). `balance` will review them:

  | | GK | DF | MF | FW |
  | --- | --- | --- | --- | --- |
  | Dribbling | 1–17 | 11–48 | 27–74 | 37–84 |
  | Heading | 6–37 | 43–84 | 17–58 | 27–74 |
  | Strength | 27–69 | 37–84 | 22–69 | 27–74 |
  | Acceleration | 11–48 | 27–74 | 32–74 | 43–90 |
  | Positioning | 22–58 | 43–90 | 32–74 | 37–84 |

**Yours (match):** the five fields on `matches.Ratings`, included in `Ratings.Valid` and the `enginetest` fixtures, and the copy in `World.candidate`. You can add these as soon as our commit is on `main`. We'll close this note in that commit, and `World.candidate` will then read `players.Dribbling` and the other four.

**Others:** after delivery we file a `ui` note (squad views and web sorting only index attributes 0–5 today) and ask `balance` to check the spreads.
