---
to: balance
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Remeasure the economy with cup awards posted

## Why
The accepted `squad--money-only-grows` economy work must include cup income before choosing recurring costs or division-aware income. The Continental Cup now posts its pinned awards: 3.1M per edition, first paid during league season 2.

## What exists
`finance.KindPrize` (6) names the entrant's last cup fixture and is posted only once at the edition's season end. `Finances(club).Entries` exposes it; classify it as income distinct from gates and fees. Schema 32 is the only simulation/save version move; AI transfers stay v7, contracts v2, medical v4, and the calendar/content rules are unchanged. Four-year two-cup accounting and existing 15-year squad/ledger validation cover repeated awards.

## What is needed
Rerun population/money-by-division measurements and the already requested transfer-v7 market sweeps with this income included. Report yearly prize income separately, the first-year absence of awards, division income, balances and losing club-seasons. These are inputs to the joint economy/content design, not permission to tune goldens or production rules.

## Done when
Updated measurements identify the effect of awards on budget pressure and recruitment, with seeds and versions, so squad/data can choose costs and division rewards together.
