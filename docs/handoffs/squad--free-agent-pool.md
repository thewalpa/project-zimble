---
to: squad
from: balance
status: accepted
blocking: no
created: 2026-09-28
---

# The free-agent pool is always empty for the manager

## Why
In 30-year sweeps the pool is empty when every window opens and when it closes in all 10 AI-only seeds. With a passive manager at club 3 (4 seeds), it holds 2–4 players at the open in years 2–10, which are his own expired contracts. AI clubs sign them during the window, and at most one player is left at the close, rated 36–43 against squad averages of about 59. So a manager looking for a free agent finds nobody worth signing, and `SignPlayer` is effectively unused in play. The numbers are in [docs/balance.md, "Free agents"](../balance.md#free-agents).

## What exists
- AI signings at the contract-year end (`ai.Signings`, called from the contract-year task in [contracts.go](../../internal/app/contracts.go)) and AI signings from the pool in the window's daily runs ([transfers.go](../../internal/app/transfers.go)).
- The sweep: `ZIMBLE_BALANCE=1 go test ./internal/app -run TestBalance -v -count=1`. See the `fa-open`, `fa-close` and `best-fa` columns.

## What is needed
Is this intended? If not, some players should reach the pool and stay there long enough for the manager to act. Some possible ways:
- AI clubs sign free agents only for vacancies below their minimum quota before the window, and fill the rest later in the window;
- AI clubs let more expiring contracts lapse;
- a few days at the start of the window before AI clubs may sign free agents.

If it is intended, decline this note with the reason, and I'll record it as the design.

## Done when
A suggestion: at the window's open, the AI-only sweep shows a pool of 3 or more players in most years, with the best of them within about 10 points of the average squad. Squads still end the window full.

## Answer
Accepted: not intended. It is second in squad's backlog, after star churn, and will be designed with it, since both are about when AI clubs sign and sell. The second divisions' clubs share the pool ([the data note](squad--market-spans-32-clubs.md), now closed, pointed this out); squad doesn't need a division rating gap for this.
