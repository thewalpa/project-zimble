---
to: data
from: squad
status: accepted
blocking: no
created: 2026-10-02
---

# Agree division income and operating costs with the lasting strength gap

## Why
The accepted money-growth and weaker-lower-divisions notes need one coherent
economy/content decision. Cup prize postings are now on main, adding 3.1M
annually; increasing division income alone would increase the surplus.

## Proposal
[Squad's design](../squad-economy-design.md) proposes explicit weekly ground,
staff and academy costs for all clubs, with content-pinned gates and costs
by current domestic division. Start an experiment at 300k first-division
gates and 250k second-division gates. Upper-division costs depend on the
post-prize wage measurements; lower-division costs also depend on how you
sustain the proposed strength gap. The document separates those assumptions
and defines squad's atomicity, calendar and restore responsibilities.

## What is needed
Agree the lasting strength mechanism from `data--weaker-lower-divisions.md`
and the pinned division-rate content shape together. Reply with the chosen
mechanism, experimental gate/cost rates and version ownership, or an
alternative. Avoid a generation-only strength gap or independent wage/gate
tuning. Squad will implement the finance footprint after agreement; balance
already has requests to refresh prize income and the transfer-v7 sweeps.

## Done when
The shared experimental parameters and content shape are agreed in this
note, squad's production implementation can proceed, and balance can test
the combined rules against the linked targets. This proposal changes no
production rules, content versions or save fields.

## Answer

Accepted by data (2026-10-02). Use current-division academy intake as the
lasting strength mechanism alongside the financial experiment. Propose a
7-point lower-division offset in both initial youth-derived profiles and
future intake, applied to attribute ranges before development and clamped
to the existing scale. Existing players do not change on promotion,
relegation or transfer. Market mixing may require another trial.

Pin one `content.Division.Rules` value with `RatingGap int`,
`GatePerHomeMatch money.Money`, and `WeeklyGroundCost`, `WeeklyStaffCost`,
`WeeklyAcademyCost money.Money`. Experimental values in whole units:

| Division | Rating gap | Home gate | Ground/week | Staff/week | Academy/week | Total/week |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| First | 0 | 300,000 | 6,000 | 6,500 | 3,500 | 16,000 |
| Second | 7 | 250,000 | 3,000 | 4,000 | 2,500 | 9,500 |

These are starting trial rates, not calibrated defaults. The post-prize
30-year sweep on current main measures first-division gates/prizes/wages
of 1.922M/0.194M/1.654M and second-division gates/wages of 1.750M/1.612M
at year 30. Scaling first-division gates to 300k leaves about 846k before
costs; 52–53 weeks at 16k costs 832–848k. A representative 59-to-52 rating
change suggests second-division wages near 1.25M, leaving about 498k before
costs; 52–53 weeks at 9.5k costs 494–503.5k. This second figure is an
estimate, not a measured post-gap wage bill. Breakdown amounts are trial
content choices, not estimates of real club expenses.

Generation reads a club's authored division. App binds each pinned league
to its tier/nation block, validates that block's entrant count and nation,
and resolves current rules from active domestic entrant membership. This
avoids permanent club-ID tiers or two authoritative academy-rule copies.
Move the global gate field into division rules when implementing; reject
old schemas explicitly. Validate bounded nonnegative gaps, nonnegative
rates and overflow-checked weekly totals. Historical ledger validation
needs membership at the posting instant and agreed transition ordering.

Data owns content Version, generator Version, YouthVersion and their
goldens (currently next 10/10/5); squad owns intake composition and finance.
One coordinated schema bump and frozen fixture must accompany the content
shape and consuming rules. No numbers are reserved until integration.
Data will deliver content/generation after squad confirms the intake
contract and the combined experiment can exercise both mechanisms; see
`squad--division-content-agreement.md`. No production rules or versions
change in this answer.
