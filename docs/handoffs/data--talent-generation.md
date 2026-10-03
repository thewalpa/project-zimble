---
to: data
from: squad
status: open
blocking: no
created: 2026-10-03
---

# Draw talent at generation and mark position floors in content

## Why
Step 2 of [player development and potential](../squad-development-design.md#8-implementation-plan).
Step 1, the rules, is on main and changes no output; step 2 needs generation
to draw each player's hidden talent and content to say which attributes a
position never uses.

## What exists
In `internal/players/talent.go`: `Talent` (archetype plus six knots, zero is
neutral), `DrawTalent(rng)`, `Norm`/`NewNorms` (each position's attribute
middles and floor flags) and `DevelopWith(seed, profile, talent, norms, age,
year)`. `Develop` is the neutral case and still pins v1 exactly.

## What is needed
1. **Floors in content.** Mark per position the attributes it never uses
   (outfield goalkeeping; a goalkeeper's finishing and dribbling) so `app`
   can build `players.Norms` from the generated ranges' middles. Propose the
   field; squad builds the `Norms` in `app`.
2. **Talent at generation.** Draw talent for every generated player and in
   `worldgen.Youth` from a stream of its own keyed by player ID
   (`"worldgen/talent"`), so no existing draw moves; run initial histories
   through `DevelopWith` with that talent and the norms. Return it beside
   the profile.
3. **Timing.** Land this after `joint/division-economy`, which also changes
   `Youth`, or fold both into one worldgen bump if that is simpler for you.
   Squad's matching commit adds the store field, snapshot, restore checks,
   schema and fixture, lifecycle wiring and long-career retirement
   (`DevelopmentVersion` 2); the two commits integrate together.

## Done when
Squad and data agree the content field and the `Youth`/snapshot return shape
here, and step 2 can be built as one paired integration.
