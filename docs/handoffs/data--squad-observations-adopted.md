---
to: data
from: squad
status: open
blocking: no
created: 2026-10-01
---

# Review squad's observation adoption and staged overlay

## Why
Squad delivered `squad--club-observations`: manager policy now uses the deciding club's exact observations. This documents the cohort overlay before uncertainty is introduced.

## What exists
`contracts.go` projects `ObservePlayers(club, activePlayers)` into detached policy inputs. Annual renewal/kept averages and each club's pool use that view; planned departures add ReleasedBy to every observed pool. `ai.SigningsForClubs` takes per-club detached pools and reserves a selected player globally by ID. Contract suggestions read the observer's Demand. Seller valuation and unlisted pricing use seller knowledge.

`transfers.go` caches initial observations per club for one market. Staged employers/counts supply membership for surplus/upgrade comparisons; remaining free-agent IDs supply shared availability. Candidate ratings come from the buyer, prices from the seller. Consent and capacity retain authoritative staged facts. Seller pricing retains its original observed squad average, as it did before. Policies never use cached Club/Team as the current staged assignment.

The scheduler commits Now after a cohort handler, so `recruitmentPlayers` adjusts only Age from public birth dates to the decision instant; other observation facts remain the current store projection. No saved cache, new fields, schema/version/golden changes or hidden scouting policy.

## What is needed
Review this overlay when evolving observations. An observation-at-decision-instant API would remove the local public-age projection; require it before introducing time-sensitive knowledge. Existing unscoped compatibility views remain unchanged, with explicit human observer work already requested in `ui--club-observations`. No UI output or new command contract is added here.

## Done when
The overlay and task-instant projection are accepted for today's exact-information policy, or a concrete extension is requested before scouting work.
