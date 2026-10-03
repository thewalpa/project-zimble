# Economy proposal, 2026-10-02

Status: agreed with data on 2026-10-03 as an experiment (see the agreed
contract below); awaiting the joint implementation and balance's sweeps. This is an implementation design, not calibrated
new rules. The current economy and AI affordability rules still apply.

## Problem and target

The accepted [money-growth handoff](handoffs/squad--money-only-grows.md)
measures persistent universal surplus. The earlier 30-year baseline has
1.922M first-division gate income and 1.750M second-division income per club,
against roughly 1.6–1.7M wages. Transfers move cash between clubs and cannot
remove the world's aggregate surplus. The delivered cup awards add 3.1M per
year across eight first-division entrants: 193,750 averaged across the 16
first-division clubs, but concentrated in the qualifiers.

Adopt balance's targets for an experiment: median balances within ±50% of
the opening balance after 30 years, the richest below about five times its
opening balance, first-division income at least 20% higher, and 5–15% of
club-seasons losing money without widespread insolvency. Measure the actual
ledger result, including cup prizes, fees and release payoffs, rather than
using income minus a single opening wage bill.

## Proposed mechanism

Add explicit ground, staff and academy operating costs, paid weekly by every
club through the same scheduled finance workflow as wages. Pin rates by
league division in content. Pair them with division-specific gate receipts:
the host's current domestic division sets gate and operating rates, including
cup matches. A promoted club gets its new rates when it joins the newly
created league season; it does not keep a permanent club-ID income tier.

Use the active season's entrant membership to resolve the club's division.
Require exactly one domestic division per club. At a transition, the season
transition's ordering decides whether a coincident payday uses old or new
membership; document and test that ordering with competitions. Never use
map iteration, the historical fixture's competition ID alone, or an archived
season as the club's present economic tier.

For data's first experimental content set, propose 300k per home match in
the first division and 250k in the second. At the historical cup exposure
that would mean about 2.306M gate plus 0.194M prize per first-division club,
against 1.750M second-division income (about 43% more). This arithmetic is a
starting estimate: cup qualification and home draws vary across careers.

An unchanged first-division 1.6–1.7M wage bill would leave 0.8–0.9M a year
for operating costs. Do not assign the same cost to the second division:
at roughly 1.6M wages, its margin is only 0.15M. If data's lasting strength
gap instead lowers a representative rating from 60 to 53, the squared wage
curve scales wages by 2,809/3,600; a 1.6M bill would be about 1.25M, leaving
about 0.5M. These are alternative wage assumptions, not measured outcomes.
Choose the second-division cost only after the strength mechanism and
post-prize wage measurements are agreed. Divide an experimental annual
amount into a pinned weekly rate; count the actual 52 or 53 paydays in the
calendar when evaluating it.

Do not balance the budget by increasing hidden AI spending or exempting an
AI club from transaction costs. The existing wage reserve remains a policy
to reconsider separately after balance's TransfersVersion-7 sweeps. Fixed
costs may expose a need for explicit shared debt or board rules; if the
sweeps show structural insolvency, revisit the rates or design those rules
rather than silently forgiving a club's deficit.

## Ownership and delivery

Data chooses and validates the pinned content representation for division
rates and the lasting strength mechanism. Squad owns the ledger kind,
weekly postings, gate lookup and cross-module finance validation. A single
payday plans wages and operating costs before scheduling or applying; an
overflow must leave balances, events and the queue unchanged. Each cost is
justified by the scheduled task and its pinned economic tier, and every
transfer retains paired entries summing to zero.

The implementation must include its events, restore facts, schema bump and
frozen fixture in the same feature commit. Content/version changes belong
with the agreed data change; squad must not edit a content-owned golden to
hide an unintended generation change. Historical cost validation must
identify the division at the posting instant, including promotion and
relegation; if existing season history cannot unambiguously prove that
fact, save an explicit validated tier reference rather than inferring one.

Before integration, verify promotion/relegation rates, cup-host income,
simultaneous payday/transition ordering, overflow atomicity, retry and
save/load identity, and multi-season ledger accounting with legal squads.
Hand the new finance label and breakdown to UI. Ask balance to rerun the
population/economy and all three market sweeps on the combined agreed
versions, reporting wages, operating costs, gates, prizes and transaction
cash separately by the division occupied during each measured season.

## Agreed contract, 2026-10-03

Agreed in `data--division-economy-design.md` and `squad--division-content-agreement.md`.

- **Content.** `content.Division.Rules`: `RatingGap int`, `GatePerHomeMatch`,
  `WeeklyGroundCost`, `WeeklyStaffCost`, `WeeklyAcademyCost money.Money`.
  Trial: first division 0 / 300k / 6,000 + 6,500 + 3,500; second division
  7 / 250k / 3,000 + 4,000 + 2,500. `Economy.GatePerHomeMatch` goes away
  with its last consumer.
- **Strength gap.** A lower division's youth ranges drop by its `RatingGap`
  (clamped at 1) at generation and at every intake; existing players keep
  their profiles when their club moves. `worldgen.Youth` takes the 0-based
  `tier` and reads the gap from content; the stream and draw order are kept,
  so a top-division club's intake is unchanged for the same seed.
- **Membership.** A club's tier is its league's season current at the
  instant (competitions' query, requested in
  `competitions--league-season-at-instant.md`). Paydays and the player year
  at a transition instant see the old membership (Preparation and Expiries
  run before Consequences). Gates use the host's tier at the result instant,
  cup matches included.
- **Ledger.** One new finance kind for operating costs: one negative entry
  per club per payday, the weekly total, planned with the wages in one
  finance plan. Restore checks each entry's amount against the club's tier at
  its instant; the UI breakdown is derived from the pinned rates.
- **Integration.** Data's content/generation commit and squad's runtime
  commit share `joint/division-economy` and reach `main` together with one
  schema bump and frozen fixture; balance sweeps after.
