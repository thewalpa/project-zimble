# Player development and potential: proposal, 2026-10-03

Status: proposed by squad from the owner's brief, revised the same day with
the owner's decisions (see [Decisions](#decisions)). Nothing here is
implemented. `players.Develop` at `DevelopmentVersion` 1 still applies: every
player follows one age curve plus yearly noise.

## The brief

- **No potential number and no talent score.** No single hidden value caps a
  player or ranks his future.
- **Potential is judged from attributes.** Managers, the AI and later scouts
  judge a young player by what he can do now. High technique at 17 suggests a
  good future. A teenager who can only run will struggle to learn technique.
- **A hidden talent curve.** Each player gets a few hidden talent values, one
  per career phase. Together they draw a curve over age that speeds up, slows
  or reverses his development in that phase.
- **Archetypes with weights.** Talent curves are drawn from archetypes (late
  bloomer, stagnates after 20, rare players who last into their late thirties
  …), weighted so that the population looks realistic.
- **Normalized to the general curve.** The average player still develops like
  today. Individual players end up genuinely better or worse.

## Today

`players.Growth(age, attribute)` is the general curve, in whole points a year:

| Age | up to 18 | 19–20 | 21–22 | 23–24 | 25–28 | 29–30 | 31–32 | 33+ |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Points a year | +4 | +3 | +2 | +1 | 0 | −1 | −2 | −3 |

There are two adjustments:

- From 29, pace, stamina and acceleration decline one point a year faster.
- From 29 to 32, strength and positioning decline one point a year slower.

`players.Develop` also adds a yearly form of −2..2 to every attribute together,
and a separate variation of −1..1 to each attribute.

Youth join at 16–17 with each position's ranges lowered by 13 points
(`content.Youth`). Generation (v9) gives initial players a detached youth
profile and runs `Develop` over the years up to their age. Players retire with
a chance from 33 and always at 37, or at 31 if they have no club.

So every player has the same expected future. Only the intake draw and yearly
luck separate them. Nothing about a 17-year-old can be inferred beyond his
current rating, and the AI's `agePermille` values every youngster the same way.

## The model in one picture

```
   hidden truth (saved, shown only in cheat mode)        visible
  ┌──────────────────────────────────────────────┐   ┌───────────────────┐
  │ talent curve: 6 knots over age, drawn from    │   │ attributes        │
  │ a weighted timing archetype                   │   │ (the player's own │
  └───────────────┬──────────────────────────────┘   │  profile = what   │
                  │                                   │  he learns easily)│
                  ▼  each year                        └──┬─────────┬──────┘
   general curve + talent(age) × window(group, age)       │         │
   × learning(group, from his attributes) when growing ◀──┘         ▼
   → yearly change                                       potential score
                                                         (5-point steps, from
                                                          attributes and age)
```

There is one hidden thing: **timing**, the talent curve from the brief.

What a player learns easily is **not stored**. It is read from his attributes
every year. A player whose technique stands out compared with his position
learns technique faster, and one who is mostly legs learns it slower. Today
this profile comes from how players are generated (the intake draw within each
position's ranges). Generation archetypes such as technician or athlete are a
later discussion with `data`. They would change only what is generated, never
this rule.

Because the learning rule reads only visible attributes, the evaluator can
apply exactly the same rule. The only uncertainty left in judging a player is
his hidden timing, plus form and noise.

## 1. Talent curve (timing)

### Knots

Each player has six talent values. Each is the extra growth per year, in tenths
of a point, at a fixed age:

| Knot | Age | Phase |
| --- | --- | --- |
| K1 | 17 | youth |
| K2 | 20 | breakthrough |
| K3 | 23 | establishment |
| K4 | 27 | peak |
| K5 | 31 | ageing |
| K6 | 34 | late career |

Between knots the curve is interpolated linearly in integer arithmetic. Before
K1 it holds K1's value; after K6 it holds K6's. So `T(age)` is a continuous
curve with age on the x-axis. Values are bounded to −40..40 (−4..+4 points a
year).

### Timing archetypes

The weights below are a starting proposal for balance to tune, in permille.
The knot values are already normalized (see [Normalization](#3-normalization)).

| Archetype | Weight | K1 17 | K2 20 | K3 23 | K4 27 | K5 31 | K6 34 | Story |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Steady | 320 | 0 | +1 | +2 | 0 | −1 | 0 | follows the general curve |
| Prodigy | 40 | +25 | +21 | +12 | 0 | −1 | 0 | fast and early, stays high |
| Early peaker | 110 | +15 | +6 | −18 | −10 | −1 | 0 | good at 19–20, stagnates after 20, then slips |
| Late bloomer | 100 | −10 | −9 | +17 | +15 | +4 | 0 | ordinary at 21, best at 28–30 |
| Limited | 130 | −10 | −14 | −13 | −5 | −1 | 0 | never quite gets going |
| Grafter | 80 | 0 | +11 | +12 | +5 | −1 | 0 | keeps improving into his mid-twenties |
| Evergreen | 100 | 0 | +1 | +2 | +5 | +14 | +10 | ages slowly |
| Early fader | 100 | 0 | +1 | −3 | −10 | −16 | −15 | declines from his mid-twenties |
| Ageless | 20 | 0 | +1 | +2 | +10 | +24 | +30 | rare: still at his level at 38 (Modrić, Messi, Ronaldo) |

Within an archetype, each knot gets its own variation: a sum of two −4..4
draws (−8..8, centred). No two players of one archetype share a curve, and
archetypes blend at their edges. Ageless is rare on purpose. About one player
in fifty has its long-career shape, and most of them are ordinary players who
simply last. A Modrić needs ageless timing and a high level, which is far
rarer.

The table below shows the expected level of one attribute for each archetype's
mean curve, joining at 16 at 47 (the middle of the youth range). It leaves out
learning, form and noise, comes from the scratch simulation used to check this
proposal, and ignores retirement:

| Archetype | 16 | 18 | 20 | 22 | 24 | 26 | 28 | 30 | 32 | 34 | 36 | 38 | Peak |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| General curve | 47 | 55 | 61 | 65 | 67 | 67 | 67 | 65 | 61 | 55 | 49 | 43 | 24–28 (67) |
| Steady | 47 | 55 | 61 | 66 | 68 | 68 | 68 | 66 | 62 | 56 | 50 | 44 | 26 (68) |
| Prodigy | 47 | 60 | 70 | 77 | 82 | 82 | 82 | 80 | 76 | 70 | 64 | 58 | 26 (82) |
| Early peaker | 47 | 58 | 65 | 68 | 67 | 64 | 62 | 59 | 55 | 49 | 43 | 37 | 22 (68) |
| Late bloomer | 47 | 53 | 57 | 62 | 67 | 70 | 73 | 73 | 70 | 64 | 58 | 52 | 28 (73) |
| Limited | 47 | 53 | 56 | 58 | 57 | 56 | 55 | 52 | 48 | 42 | 36 | 30 | 22 (58) |
| Grafter | 47 | 55 | 63 | 70 | 74 | 75 | 76 | 74 | 70 | 64 | 58 | 52 | 28 (76) |
| Evergreen | 47 | 55 | 61 | 66 | 68 | 69 | 70 | 70 | 69 | 65 | 61 | 57 | 30 (70) |
| Early fader | 47 | 55 | 61 | 65 | 66 | 65 | 63 | 58 | 51 | 42 | 33 | 24 | 24 (66) |
| Ageless | 47 | 55 | 61 | 66 | 68 | 70 | 72 | 74 | 75 | 74 | 74 | 74 | 32 (75) |

Some patterns to read from it:

- **Early peaker against steady:** the early peaker is ahead at 20 and behind
  at 26.
- **Late bloomer:** behind at 20 and ahead at 28.
- **Late bloomer against limited:** at 20 they look alike. A club that writes
  off one also writes off the other.
- **Ageless:** from 34 his technique and reading hold level, while his
  physical attributes still lose about a point a year. That is a Modrić who
  reads the game better than he runs.

These overlaps are what make youth recruitment a judgement rather than a
lookup.

## 2. Learning from the player's own profile

### Attribute groups and windows

| Group | Attributes | Talent window |
| --- | --- | --- |
| Technical | passing, dribbling, finishing, heading, goalkeeping | full to 21, then 800 / 600 / 500‰ at 22 / 23 / 24, 400‰ from 25 to 28 |
| Physical | pace, acceleration, stamina, strength | full to 25, 600‰ from 26 to 28 |
| Reading | defending, positioning | full at every age |

From 29 every group takes the talent curve in full. Past 29 the curve is about
ageing, not learning, and a long career depends on keeping your legs.

The window scales only the talent term, never the general curve. Here is what
it does for a late bloomer who is still growing at 25–28:

- He keeps improving his technique a little, at 40% of his late burst.
- Most of the burst goes into physique and reading the game: "he filled out
  and learned to read the game".

A runner who is technically raw at 23 can still add a few points of technique,
but he does not become a technician.

### The learning factor

Each year, for each group `g`, the player's **relative surplus** is computed
from his current attributes:

```
surplus(g)  = mean over g's relevant attributes of (attribute − position norm)
relative(g) = surplus(g) − mean surplus over all his relevant attributes
learning(g) = clamp(1000 + 25 × relative(g), 700, 1300)          // permille
```

- **Position norm** is the middle of the position's generated range for the
  attribute, from content. `app` builds a detached `Norms` value once, since
  `players` cannot import content.
- **Relevant attributes** leave out the floors a position never uses:
  goalkeeping for outfield players, and finishing and dribbling for
  goalkeepers. `data` marks these in content as floors.
- **Relative** means the factor reflects the *shape* of his profile, not his
  level. A prodigy is not taxed twice for being good everywhere. Relative
  surpluses sum to about zero for every player, so the rule moves growth
  between groups rather than adding it.

Example: an athlete with technique 5 below his position's norm and physique 7
above, at a normal reading level. His factors are about 870‰ technical and
1170‰ physical. From 16 to 24 the general curve gives about 20 points. He gains
about 17 in technique and 23 in physique, so his gap widens by about 6 points
over his youth. A technician the other way round mirrors him.

Two limits keep the rule tame:

- The factor is clamped and recomputed every year from the current
  attributes, so there is no saved state and no runaway.
- It applies only to positive gains, which mostly happen before 25.

So: high technique at 17 *is* a good sign for technique at 25, both for the
simulation and for anyone who reads the player.

## 3. Normalization

The talent curve is normalized at the population level. This is the owner's
decision 1. Each knot's weighted mean over the archetypes is subtracted from
every archetype at that knot. The table above is already normalized: the raw
means were +0.35, −0.8, −1.95, −0.15, +1.0 and +0.1 tenths, rounded and
subtracted. Within-archetype variation is centred, so the expected talent of a
random player is zero at every age.

The learning factor is relative to the position norm and sums to about zero for
each player, so it neither adds nor removes growth on average.

The **average player therefore follows the general curve** (up to rounding and
clamping at 1 and 100). This matters because data's youth-floor stability
check ([progress](progress.md#data-youth-floor-stability-verified-done)) shows
all 44 position/attribute means within 2 of generation over 30 years. This
design keeps that equilibrium and widens the spread around it.

Three tests guard this:

- The tables have an integer mean of 0 at every knot.
- A large cohort drawn and simulated from 16 to 36 keeps its mean change at
  every age within half a point of `Growth`.
- Its group means per position stay within one point of the same cohort
  developed by `Growth` alone.

## 4. The yearly step

For a player at `age` in calendar `year`, for each attribute `a` in group `g`:

```
gain   = 10 × Growth(age, a)  +  T(age) × Window(g, age) / 1000      // tenths
if gain > 0: gain = gain × Learning(g) / 1000
change = round(gain / 10) + form + variation(−1..1)
value  = clamp(value + change, 1, 100)
```

- **Rounding** of tenths is probabilistic: round up with a probability of the
  remainder in tenths, drawn from the player's development stream. The expected
  value is exact and no fractional state is saved. The stream stays keyed by
  seed, player and year, so the result does not depend on order.
- **Decline:** the learning factor does not soften decline. Only the talent
  curve does, from K4 on.
- **Decrease in youth is allowed.** An early peaker at 23 has a gain of about
  −0.8 a year in physique and reading, and slightly below 0 in technique (a
  600‰ window), before form and noise.
- Form and per-attribute variation stay as they are.

### Neutral talent reproduces today

With zero knots and no norms (every learning factor 1000), all gains are whole
points and no rounding draw is needed. That reproduces `DevelopmentVersion` 1
exactly, a safe first step that a test can pin.

## 5. Retirement for long careers

The rare long careers in decision 3 need retirement to allow them. Today
everyone retires by 37.

- **Players with a K6 below +20:** today's rule is unchanged. That is almost
  everyone, so the general population ages as it does now.
- **Players with a K6 of +20 or more** (in practice ageless, and rarely the
  top of evergreen):
  - The retirement chance from 33 to 36 is halved.
  - They may play on to a new hard limit of 40, with 300 / 500 / 700‰ at
    37 / 38 / 39.

Free agents still retire from 31: a club has to want him.

Retirement keeps its own stream (`players/retirement`) and rides the same
`DevelopmentVersion` bump. `app.World.Validate` and the lifecycle checks that
assume no active player is 37 or older change with the new limit.

## 6. Truth, knowledge and cheat mode

`docs/architecture.md` already separates the two: "The simulation can know a
player's actual attributes and potential while a club sees scouting estimates
… Do not accidentally give AI recruitment perfect hidden attributes."

- **Truth:** each player's archetype (a durable enum value, for the cheat view
  and measurement) and his six knots. `players` stores them beside the profile,
  not inside `players.Profile`. Selection, match engines, club observations
  and the clients read profiles and so cannot reach them by accident. Only
  development and retirement read the truth.
- **Hidden in normal play:** no observation, event, inbox message or regular
  view shows talent, archetype or true peak. No look-back at retirement either
  (decision 6).
- **Cheat mode (decision 6):**
  - An explicit app query, `World.TalentReveal(player)`, returns the
    archetype, the six knots and the true expected peak (the outlook
    computation run with his real curve).
  - The clients show it only when started with a cheat flag (`-cheat` on
    `cmd/play` and `cmd/web`): the archetype name and a small curve on the
    player profile.
  - The flag is per run and is not saved.
  - A test asserts that no non-cheat page or command calls the query.
- **Knowledge:** the potential score below is computed only from what a club
  observes (position, attributes, age). Today every club observes exact
  attributes (`World.ObservePlayers`). Scouts will later observe them with
  error, and the same rule then becomes uncertain in a second way.

## 7. Potential score

The rule lives in `players`: `Outlook(norms, position, attributes, age)
Outlook`. It takes no talent argument, so its signature guarantees it cannot
peek.

1. **Projection.** Develop the attributes year by year to 27 with the general
   curve, the windows and the same learning rule, assuming neutral timing,
   average form and no noise. Then take the position's overall at its best
   along the way. For a player aged 27 or older, it is his current overall.
2. **Score.** The projected peak rounded to the nearest 5, within 5..100:
   "potential 70" (decision 5). Managers and the AI see the same score
   (decision 7). It is the whole published result, so nobody gets finer
   information than another.
3. **Remarks.** One or two short scout sentences from the same computation:
   - "technique well ahead of his position" or "physically raw" (relative
     surplus);
   - "still has years of growth" (age);
   - "little growth expected" (past the windows).

   These are words, not numbers from the truth.

The score is uncertain only because of timing:

- A late bloomer scores below what he becomes.
- An early peaker scores above.
- A prodigy at 18 already scores high, because he is ahead of his age.

That is the design: the score is an honest estimate from what can be seen.

### Calibration targets (proposal for balance)

- At 17–18, the rank correlation between the score and the actual best overall
  is about 0.55–0.7. That is clearly better than an age-only guess and far from
  certain.
- At 21–22 the correlation is about 0.8–0.9.
- At 17–18, the actual peak is within one step (±5) of the score for about 60%
  of players and within two steps for about 90%.

### Consumers

- **AI valuation** (`ai.Valuation`, squad): replace the flat `agePermille`
  youth premium with a value that blends the score into the overall by the
  years to peak. Renewals and recruitment can then prefer prospects. This
  needs an `ai.TransfersVersion` and `ai.ContractsVersion` bump and
  multi-season tests.
- **Club observations** (data): `PlayerObservation` gains the outlook, since
  it is what a club knows.
- **Clients** (ui): the score and remarks in the squad, profile and market
  views; the cheat view behind the flag.
- **Wages:** `content`'s economy (data) stays on the current overall in the
  first version.

## 8. Implementation plan

Each step passes the three checks on its own.

1. **players (squad): rules only.**
   - Code: the `Talent` type (archetype and six knots), the archetype table
     with normalization, `DrawTalent(stream)`, `Norms`, and
     `DevelopWith(seed, profile, talent, norms, age, year)`. `Develop` becomes
     the neutral case.
   - Tests:
     - normalization;
     - neutral equals v1 (pinned);
     - determinism and order independence;
     - bounds over a century for the extreme curves;
     - each archetype's shape (a late bloomer gains more at 23–27 than at
       17–21, an early peaker loses at 23–27, ageless holds technique and
       reading at 34–38);
     - the learning factor's direction and clamp;
     - the population mean against `Growth`.
   - Versions: none change.
2. **Store, saves, generation and retirement (squad + data).**
   - **players:** the store keeps one talent per player, with additions
     carrying it.
   - **Saves:** the talent goes into the snapshot. Restore validates it
     (knots within −40..40, a known archetype, every active player has one)
     and never regenerates it. This bumps `storage.SchemaVersion` and writes a
     save fixture.
   - **data:** draws talent in `worldgen.Youth` and for initial players, runs
     generation's histories through `DevelopWith`, and marks floor attributes
     in content. Bumps `worldgen.Version` and `worldgen.YouthVersion`.
   - **Lifecycle:** passes talent and norms to `DevelopWith` and applies the
     long-career retirement (`DevelopmentVersion` 2).
   - Goldens move in the lanes that own them.
3. **Potential score (squad).**
   - `players.Outlook`, with `Norms` built in `app`.
   - A calibration test against the truth on a seeded population.
   - `World.TalentReveal` for cheat mode.
4. **Consumers.**
   - AI valuation and renewals (squad, with AI version bumps).
   - The observation field (a note to data).
   - Score, remarks and the cheat view in both clients (a note to ui).
   - A note to balance to measure the targets below and the 30-year attribute
     stability.

### Realism targets for balance

- **Averages:** the general curve is unchanged on average. All 44
  position/attribute means stay within 2 of generation over 30 years.
- **Peak age:** median 26–27, about 15% at 29 or older, about 15% at 23 or
  younger.
- **Stagnation and late blooming:** about 15% of players gain 3 or fewer
  overall points from 20 to 27. About 10% gain more from 23 to 27 than from 19
  to 23.
- **Long careers:** about 1–2% of players are still active at 37 or older.
  Each season a handful of them are still regular starters in the whole world.
- **Spread:** the p90 overall at year 30 rises a few points over today's 74,
  but no more than one player in 200 is above 90.

## Later, without changing the truth

- **Generation archetypes** (with data): technician, athlete, reader and so on
  shape the intake draw. The learning rule then rewards them with no change
  here.
- **Playing time** (squad backlog): minutes from `MatchCompleted` scale
  positive gains, like the learning factor, for players under about 24. They
  need a season usage record that counts a retried result once. Agree it with
  match and data.
- **Difficulty** (decision 7): an explicit observation policy that gives the AI
  a better or worse view than the manager.
- **Scouts:** noisy observations and report dates per club, feeding the same
  score.
- **Trajectory:** last year's attributes as evidence of timing (a jump at 22
  hints at a late bloomer). It needs a read model of development events.
- **More of `docs/architecture.md`:** injuries that cut a phase short, training
  focus and monthly development.

## Decisions

Answered by the owner on 2026-10-03:

1. **Normalization at the population level.** The average player follows the
   general curve, and individuals end up genuinely better or worse.
2. **No stored aptitude.** What a player learns easily comes from his own
   generated attributes (section 2). Generation archetypes are a later topic.
3. **Six knots,** adding 34, so that rare players last very long (section 5).
4. **Late technique:** a player who can still grow significantly also grows a
   little technically. The technical window holds at 400‰ from 25 to 28.
5. **The potential score** is the expected peak in 5-point steps.
6. **No look-back at retirement,** but a debug/cheat mode shows the truth.
7. **The AI gets the manager's information.** A difficulty setting can come
   later.

Still open, for implementation rather than the owner:

- **Weights and knot values** are balance's to tune against the targets.
- **The learning slope and clamp** (25‰ a point, 700..1300) are tuned
  together with the windows.
- **The long-career threshold** (K6 ≥ +20) and the 37–39 retirement chances
  are measured by balance in step 2.
