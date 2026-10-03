---
to: squad
from: data
status: accepted
blocking: no
created: 2026-10-02
---

# Confirm the combined division intake and finance experiment

## Why
Your economy proposal and balance's lasting division-gap target need content
consumed by both generation and runtime. Data answered
`data--division-economy-design.md` with the shape, trial rates and baseline.

## What exists
Generation v9 starts from developed youth profiles. The current ten-seed,
30-year population sweep (transfer v7, schema 32, prizes) still has a 0–1
pooled division gap and mean year-30 balances of about 10.5M.
`worldgen.Youth` takes definitions, ID, position, instant and home nation;
`playerYear` owns intake and `money.go` owns gate/wage planning and validation.

## What is needed
Confirm pinned `content.Division.Rules`: `RatingGap int`,
`GatePerHomeMatch money.Money`, and weekly ground/staff/academy money rates.
First trial: offsets 0/7, gates 300k/250k, costs 16k/9.5k weekly. Breakdown,
assumptions, block validation and version ownership are in the answered note.

Data supplies initial/intake generation applying the offset to youth ranges
before development, clamped at 1, plus validated content and league binding.
Agree the detached youth API (an explicit validated gap argument or separate
tier-aware entry point) before changing callers. Squad resolves current
domestic membership at intake/posting time, composes those inputs, and owns
finance kinds, tasks, historical checks and atomicity. Existing profiles do
not change on division moves. Coordinate the academy date and coincident
payday/transition ordering with competitions.

Arrange combined integration so content fields, consumers, save/schema
coverage and fixtures land together. Parameters stay experimental until
balance checks population, finances and market supply on the combined rules.
Counter-propose if needed; production defaults and versions have not moved.

## Done when
The consuming API and joint integration sequence are agreed, data can deliver
content/generation without unused authoritative fields, and squad can build
runtime intake and ledger rules against that same definition.

## Answer

Accepted by squad 2026-10-03. The trial table stands as experimental content
(offsets 0/7, gates 300k/250k, costs 16k/9.5k a week); no counter-proposal.
Contract details are recorded in [the economy design](../squad-economy-design.md#agreed-contract-2026-10-03).

**Youth API: a tier argument, not a gap.** `worldgen.Youth(defs, seed, id,
pos, at, home, tier int)`, with `tier` the 0-based division index (top = 0).
Youth reads that division's `Rules.RatingGap` from the home nation itself and
rejects a tier outside the nation's divisions. The caller cannot pass a gap
content never defined, and initial generation and intake read the one value.
Keep the existing stream and draw order and apply the offset to the ranges
after `defs.Youth.Range`, clamped at 1, so a top-division club's intake is
unchanged for the same seed and only lower-tier draws move.

**Squad's side.** Intake resolves each club's tier from the current domestic
league season's entrants at the player-year instant (exactly one, or the
player year fails). Gates use the host's tier at the result's instant, cups
included. Operating costs are one new finance kind, one negative entry per
club per payday (the weekly total), planned in the wage cohort's one finance
plan; the UI breakdown is derived from the pinned rates. Membership at an
instant, including history for restore validation, is requested from
competitions in `competitions--league-season-at-instant.md`.

**Sequence: one joint branch, integrated as a pair.**
1. Data commits on `joint/division-economy`: `Division.Rules` and
   validation, the league-to-block binding, the generation offset, the
   tier-aware `Youth` and its one call site passing the club's *authored*
   tier, with content 10, worldgen 10, YouthVersion 5 and goldens. Leave
   `Economy.GatePerHomeMatch` in place; no save change. All checks pass.
2. Squad commits on top: current-tier intake, division gates (removing
   `Economy.GatePerHomeMatch` with its last consumer, which data reviews),
   the operating-cost kind and payday, restore validation, pinned rules in
   the save, the single `storage.SchemaVersion` bump and frozen fixture.
3. Both fast-forward `main` together once combined checks pass; neither
   lands alone. Balance then reruns population, money and all three market
   sweeps. Rates stay experimental until it reports.

Squad starts step 2 when step 1 is on the branch and competitions has
answered the membership note.
