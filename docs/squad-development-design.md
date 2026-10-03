# Player development and potential: proposal, 2026-10-03

Status: proposed by squad from the owner's brief, with open decisions at the
[end](#open-decisions). Nothing here is implemented. `players.Develop` at
`DevelopmentVersion` 1 still applies: every player follows one age curve plus
yearly noise.

## The brief

- **No potential number and no talent score.** No single hidden value caps a
  player or ranks his future.
- **Potential is judged from attributes.** Managers, the AI and later scouts
  judge a young player by what he can do now. High technique at 17 suggests a
  good future. A teenager who can only run will struggle to learn technique.
- **A hidden talent curve.** Each player gets a few (4–6) hidden talent values,
  one per career phase. Together they draw a curve over age that speeds up,
  slows or reverses his development in that phase.
- **Archetypes with weights.** Talent curves are drawn from archetypes (late
  bloomer, player who stagnates after 20, and so on), weighted so that the
  population looks realistic.
- **Normalized to the general curve.** A player's talent curve is combined with
  the general development curve, so the population as a whole still develops
  realistically.

## Today

`players.Growth(age, attribute)` is the general curve, in whole points a year:
+4 up to 18, +3 to 20, +2 to 22, +1 to 24, 0 to 28, then −1, −2, −3. Pace,
stamina and acceleration decline a point faster from 29. Strength and
positioning decline a point slower from 29 to 32. `players.Develop` adds:

- a yearly form of −2..2 that moves every attribute together;
- a per-attribute variation of −1..1.

Youth join at 16–17 with each position's ranges lowered by 13 points
(`content.Youth`). Generation (v9) gives initial players a detached youth
profile and runs `Develop` over the years up to their age.

Every player therefore has the same expected future. Only the intake draw and
yearly luck separate them. Nothing can be inferred about a 17-year-old beyond
his current rating, and the AI's `agePermille` values every youngster the
same way.

## The model in one picture

```
                 hidden truth (saved, never shown)             visible
  ┌────────────────────────────────────────────────┐   ┌──────────────────┐
  │ timing: talent curve, 5 knots over age          │   │ attributes       │
  │   drawn from a timing archetype (weighted)      │──▶│ (what everyone   │
  │ profile: aptitude per attribute group           │   │  sees and judges)│
  │   drawn from a profile archetype (weighted)     │   └────────┬─────────┘
  └──────────────┬─────────────────────────────────┘            │
                 │ each year                                    ▼
                 ▼                                   outlook (projection from
  general curve + talent(age) × window(group, age)    attributes and age only,
  × aptitude(group) when growing → yearly change      with an uncertainty band)
```

The hidden truth answers two questions:

- **Timing:** when does this player grow, stall or fade? This is the talent
  curve from the brief.
- **Profile:** what does he learn easily? This is a per-group aptitude.

Profile is what makes attributes evidence of the future. The same aptitude
that shapes a youngster's starting attributes keeps shaping his growth, so a
gifted technician is visible at 17 and keeps improving his technique. Timing is
mostly invisible at intake. It shows up as a player gets ahead of, or falls
behind, his age, which is where late bloomers and early peakers come from.

## 1. Talent curve (timing)

### Knots

Each player has five talent values. Each one is the extra growth per year, in
tenths of a point, at a fixed age:

| Knot | Age | Phase |
| --- | --- | --- |
| K1 | 17 | youth |
| K2 | 20 | breakthrough |
| K3 | 23 | establishment |
| K4 | 27 | peak |
| K5 | 31 | ageing |

Between knots the curve is interpolated linearly in integer arithmetic. Before
K1 it holds K1's value, after K5 it holds K5's. So `T(age)` is a continuous
curve with age on the x-axis, which is the drawing the brief asks for. Values
are bounded to −40..40 (−4..+4 points a year).

### Timing archetypes

These are a starting proposal for balance to tune. Weights are in permille.
The knot values are already normalized (see [Normalization](#3-normalization)).

| Archetype | Weight | K1 17 | K2 20 | K3 23 | K4 27 | K5 31 | Story |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Steady | 340 | 0 | +1 | +2 | 0 | 0 | follows the general curve |
| Prodigy | 40 | +25 | +21 | +12 | 0 | 0 | fast and early, stays high |
| Early peaker | 110 | +15 | +6 | −18 | −10 | 0 | good at 19–20, stagnates after 20, then slips |
| Late bloomer | 100 | −10 | −9 | +17 | +15 | +5 | ordinary at 21, at his best at 28–30 |
| Limited | 130 | −10 | −14 | −13 | −5 | 0 | never quite gets going |
| Grafter | 80 | 0 | +11 | +12 | +5 | 0 | keeps improving into his mid-twenties |
| Evergreen | 100 | 0 | +1 | +2 | +5 | +15 | ages slowly, plays at 35 |
| Early fader | 100 | 0 | +1 | −3 | −10 | −15 | declines from his mid-twenties |

Within an archetype, each knot gets its own variation, a sum of two −4..4
draws (−8..8, centred). No two players of one archetype share a curve, and
archetypes blend into each other at their edges.

Below is the expected level of one attribute for each archetype's mean curve,
joining at 16 at 47 (the middle youth range). It leaves out aptitude, form and
noise, and comes from the scratch simulation used to check this proposal:

| Archetype | 16 | 18 | 20 | 22 | 24 | 26 | 28 | 30 | 32 | 34 | Peak |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| General curve | 47 | 55 | 61 | 65 | 67 | 67 | 67 | 65 | 61 | 55 | 24–28 (67) |
| Steady | 47 | 55 | 61 | 66 | 68 | 68 | 68 | 66 | 62 | 56 | 26 (68) |
| Prodigy | 47 | 60 | 70 | 77 | 82 | 82 | 82 | 80 | 76 | 70 | 26 (82) |
| Early peaker | 47 | 58 | 65 | 68 | 67 | 64 | 62 | 60 | 56 | 50 | 22 (68) |
| Late bloomer | 47 | 53 | 57 | 62 | 67 | 70 | 73 | 73 | 70 | 65 | 28–30 (73) |
| Limited | 47 | 53 | 56 | 58 | 57 | 56 | 55 | 52 | 48 | 42 | 22 (58) |
| Grafter | 47 | 55 | 63 | 70 | 74 | 75 | 76 | 75 | 71 | 65 | 28 (76) |
| Evergreen | 47 | 55 | 61 | 66 | 68 | 69 | 70 | 70 | 69 | 66 | 30 (70) |
| Early fader | 47 | 55 | 61 | 65 | 66 | 65 | 63 | 58 | 51 | 42 | 24 (66) |

The early peaker is ahead of the steady player at 20 and behind him at 26. The
late bloomer is behind at 20 and ahead at 28. At 20 the late bloomer and the
limited player look alike, so a club that writes off one also writes off the
other. These overlaps are what make youth recruitment a judgement rather than
a lookup.

Archetypes are a generation device only. A player stores his curve, not an
archetype name. The name may be kept for measurement (see [Truth and
knowledge](#5-truth-and-knowledge)).

## 2. Aptitude (profile)

### Attribute groups

| Group | Attributes | Learning window for talent |
| --- | --- | --- |
| Technical | passing, dribbling, finishing, heading, goalkeeping | full to 21, then 800 / 600 / 400‰ at 22 / 23 / 24, 250‰ from 25 to 28 |
| Physical | pace, acceleration, stamina, strength | full to 25, 600‰ from 26 to 28 |
| Reading | defending, positioning | full at every age |

From 29 every group takes the talent curve in full. Past 29 the curve is about
ageing, not learning, and an evergreen should mostly keep his legs.

The window scales only the talent term, not the general curve. A late bloomer's
burst at 24–28 therefore goes mostly into physique and reading the game ("he
filled out and learned to read the game"). It barely goes into technique: a
runner who is still raw at 23 stays technically raw.

### Profile archetypes

Aptitude is a permille multiplier on positive yearly gains in a group
(technical / physical / reading):

| Profile | Weight | Technical | Physical | Reading |
| --- | ---: | ---: | ---: | ---: |
| Balanced | 400 | 1000 | 1000 | 1000 |
| Technician | 150 | 1300 | 850 | 1000 |
| Athlete ("can only run") | 150 | 800 | 1300 | 900 |
| Reader | 150 | 950 | 900 | 1150 |
| Gifted | 50 | 1200 | 1200 | 1200 |
| Raw | 100 | 850 | 850 | 850 |

Each value gets its own variation of −50..50‰. Weighted means are 1002.5‰,
which normalization brings to 1000.

Timing and profile are drawn independently, so a late-blooming athlete and a
technician prodigy can both exist.

### Aptitude shows in youth

At intake (and in generation's detached youth profile) each attribute is
shifted by `(aptitude − 1000) / 40` points, within 1..100. A technician starts
about +7 in technique and −4 in physique. An athlete starts −5 in technique and
+7 in physique. Then development keeps the gap widening. From 16 to 24 the
general curve adds about 20 points, so a 1300‰ technician gains about 26 in
technique and an 800‰ athlete about 16. That is how "high technique in youth
suggests a good future" becomes true in the simulation, not only in the
evaluator's head.

The intake range (about 20 points wide per attribute) is noise around the
signal. Technique at 17 is evidence of aptitude, not proof.

## 3. Normalization

The brief asks for the talent curve to be normalized with the general
development curve. The proposal does this at the population level:

- **Timing:** each knot's weighted mean over the archetypes (by weight) is
  subtracted from every archetype's value at that knot. The table above is
  already normalized: the raw means were +0.35, −0.8, −1.95, −0.35 and +0.5
  tenths, rounded and subtracted. Within-archetype variation is centred.
  The expected talent of a random player is therefore 0 at every age.
- **Profile:** the same subtraction per group makes the weighted mean
  aptitude 1000‰. Aptitude is drawn independently of timing and only scales
  gains, so in expectation it neither adds nor removes growth.

The **average player follows the general curve exactly** (up to rounding and
clamping at 1 and 100). This matters because balance and data have just
verified that all 44 position/attribute means stay within 2 of generation over
30 years (data's youth-floor stability check, [progress](progress.md#data-youth-floor-stability-verified-done)). This design keeps that equilibrium
and widens the spread around it. Individual players' lifetime totals differ:
a prodigy really ends up better than a limited player.

A test asserts the normalization from the tables (integer means of 0 at every
knot and 1000 per group). A population test simulates a large drawn cohort and
checks that the mean change at every age stays within half a point of
`Growth`.

## 4. The yearly step

For a player at `age` in calendar `year`, for each attribute `a` in group `g`:

```
gain   = 10 × Growth(age, a)  +  T(age) × Window(g, age) / 1000      // tenths
if gain > 0: gain = gain × Aptitude(g) / 1000                         // learning
change = round(gain / 10) + form + variation(−1..1)
value  = clamp(value + change, 1, 100)
```

- **Rounding** of the tenths is probabilistic: round up with a probability of
  the remainder in tenths, drawn from the player's development stream. The
  expected value is exact and no fractional state needs saving. The
  development stream stays keyed by seed, player and year, so the result still
  does not depend on order.
- **Decline:** aptitude does not soften decline. Only the talent curve does,
  in the K4–K5 phase.
- **Decrease in youth is allowed**, as the brief asks. An early peaker at 23
  has a gain of about −0.8 a year in physique and reading, and about 0 in
  technique (its window is at 600‰), before form and noise.
- Form and per-attribute variation stay as they are. Form already provides
  yearly ups and downs that the talent curve does not try to model.

### Neutral talent reproduces today

Talent with all knots 0 and every aptitude 1000 gives whole-point gains, so no
rounding draw is needed. It reproduces `DevelopmentVersion` 1 exactly. That
gives a safe first step (see [Implementation plan](#7-implementation-plan)),
and a test can pin it.

### Retirement

Out of scope for the first version, but natural next: the K5 value could scale
`RetirementPermille` (an evergreen retires later, an early fader sooner). That
version change belongs to squad. The first version leaves retirement alone, so
that balance can measure development by itself.

## 5. Truth and knowledge

`docs/architecture.md` already separates the two: "The simulation can know a
player's actual attributes and potential while a club sees scouting estimates
… Do not accidentally give AI recruitment perfect hidden attributes."

- **Truth:** the curve knots and aptitudes, plus the archetype indices for
  measurement. They are stored by `players` beside the profile, not inside
  `players.Profile`. Code that reads profiles (selection, match engines, club
  observations, both clients) therefore cannot reach them by accident. Only
  development reads them.
- **Never visible:** no app query, observation, event, inbox message or client
  shows talent or archetype. Balance reads them through test-only helpers. A
  boundary-style test asserts that no exported `app` type carries them.
- **Knowledge:** the outlook (below) is computed only from what a club observes
  (position, attributes, age). Today every club observes exact attributes
  (`World.ObservePlayers`). Once scouts arrive, they will observe attributes
  with error, and the same outlook then becomes uncertain in a second way.

## 6. The outlook: judging potential from attributes

The outlook is a pure rule in `players`:
`Outlook(norms, position, attributes, age) Outlook`. It takes no talent
argument. The signature itself guarantees it cannot peek.

1. **Age norm.** For each group, compare the player's group level with the
   expected level of an average player of his position and age. That norm is
   derived from the content's youth ranges plus the cumulative general curve.
   `app` builds the `Norms` value from content once (players cannot import
   content).
2. **Inferred aptitude.** The group surplus or deficit, converted with the
   visible-shift ratio (×40‰ a point) and *shrunk toward 1000*, because intake
   noise and timing also move the level. Shrinkage is stronger when the gap
   could be noise (young age, few attributes in the group).
3. **Assumed timing.** The remaining timing is unknown, so the projection
   assumes the neutral curve. Being ahead of the norm is ambiguous: it could
   be a prodigy, or an early peaker who is about to stop. The rule adds no
   bonus for it, and the band says so.
4. **Projection.** Develop the attributes with the general curve, the windows
   and the inferred aptitudes up to the age of 27 (or keep the current values
   if he is older). Then compute the position's overall. The band around it
   widens with the years remaining (for example ±2 per year to 27, at least
   ±2). The widths are calibrated by balance against the truth.
5. **Result:** `Outlook{Current, Likely, Low, High}` on the one 100-point
   scale, plus group remarks derived from the same surplus: "technique well
   ahead of his age", "physically raw", "reads the game like an older player",
   "little growth expected" (past the windows). These are a scout's sentences,
   not numbers from the truth.

### Calibration targets (proposal for balance)

- At 17–18, the rank correlation between `Likely` and the actual overall at 27
  is about 0.55–0.7. That is clearly better than an age-only guess and far from
  certain.
- At 21–22 it is about 0.8–0.9.
- The truth falls inside `Low..High` for about 80% of players.
- Late bloomers are underestimated at 20 and prodigies are found early. This
  is how the design is supposed to behave, not a bug in the estimate.

### Consumers

- **AI valuation** (`ai.Valuation`, squad): replace the flat `agePermille`
  youth premium with a value that blends `Likely` into the overall by the years
  to peak. Renewals and recruitment can then prefer prospects. This needs an
  `ai.TransfersVersion` and `ai.ContractsVersion` bump and multi-season tests.
- **Club observations** (data): `PlayerObservation` gains the outlook, since
  it is what a club knows. Scouts later change the observation, not the rule.
- **Clients** (ui): show the outlook range and remarks in the squad view, the
  player profile and the market. The archetype and the curve are never shown.
- **Wages** (`content` economy, data) stay on current overall in the first
  version.

## 7. Implementation plan

Each step passes the three checks on its own:

1. **players (squad): rules only.** `Talent` type, the two archetype tables
   with normalization, `DrawTalent(stream, position)`, and `DevelopWith(seed,
   profile, talent, age, year)` with `Develop` = neutral talent. Tests cover
   normalization, neutral equals v1 (pinned), determinism and order
   independence, bounds over a century for the extreme curves, each
   archetype's shape (the late bloomer gains more at 23–27 than at 17–21, the
   early peaker loses at 23–27), and a population mean within half a point of
   `Growth`. There is no version change.
2. **Store, saves and youth (squad + data).** The players store keeps one
   talent per player (additions carry it). It goes into the snapshot,
   validated on restore (knots in −40..40, aptitude in bounds, every active
   player has one) and never regenerated. This bumps `storage.SchemaVersion`
   with a fixture. Data draws talent in `worldgen.Youth` and generation's
   detached histories and applies the visible shift (`worldgen.Version`,
   `worldgen.YouthVersion`). Lifecycle passes talent to `DevelopWith`
   (`DevelopmentVersion` 2). Goldens move in the lanes that own them.
3. **Outlook (squad).** `players.Outlook` plus `Norms` from content in `app`,
   and a calibration test against the truth on a seeded population.
4. **Consumers.** AI valuation and renewals (squad, AI versions). Observation
   field (data note). Clients (ui note). A balance note asks to measure the
   targets below and the 30-year attribute stability.

### Realism targets for balance

- The general curve is unchanged on average: all 44 position/attribute means
  stay within 2 of generation over 30 years.
- Peak age: median 26–27, about 15% at 29 or older, about 15% at 23 or
  younger.
- About 15% of players gain 3 or fewer overall points from 20 to 27
  (stagnation). About 10% gain more from 23 to 27 than from 19 to 23 (late
  bloom).
- The p90 overall at year 30 rises a few points over today's 74, but no more
  than one player in 200 is above 90.

## Later, without changing the truth

- **Playing time** (squad backlog): minutes from `MatchCompleted` scale
  positive gains, like aptitude, for players under about 24. They need a
  season usage record that counts a retried result once. Agree it with match
  and data.
- **Scouts:** noisy observations and report dates per club, feeding the same
  outlook.
- **Trajectory:** an observed history of last year's attributes is strong
  evidence of timing (a jump at 22 hints at a late bloomer). It needs a read
  model of development events.
- **Injuries** that cut a phase short, training focus, monthly development
  (`docs/architecture.md`).

## Open decisions

The owner should decide these before step 2. The recommended choice comes
first:

1. **What "normalized" means.** (a) Population level: the average player
   follows the general curve and individuals end up genuinely better or worse.
   (b) Per player: talent only moves *when* a player grows and every lifetime
   total is the same. The proposal is (a), because the "limited" and "early
   fader" stories need a lower total.
2. **Aptitude groups as extra hidden values.** The brief names 4–6 timing
   values. The runner example also needs three per-group aptitudes, drawn from
   their own archetypes. Is that acceptable, or should aptitude be folded into
   the timing archetypes (fewer combinations)?
3. **Knots.** Five, at 17 / 20 / 23 / 27 / 31? A sixth at 34 would give the
   last seasons their own value. Today almost everyone retires by 36, so
   five seem enough.
4. **Late technique.** Should late bloomers be locked out of technical growth
   after 25 (window 250‰), or should a late bloomer also improve technically?
5. **Outlook display.** A range ("likely 71, 63–78") and remarks, or a coarser
   grade (stars or letters)? Both would come from the same rule.
6. **Ever revealing the truth.** Never (proposed), or a retirement
   retrospective in the career history ("a late bloomer")?
7. **The AI's information.** The AI gets exactly the manager's outlook
   (proposed, the parity audit's rule). A difficulty setting could later give
   it more or less, explicitly in the observation policy.
