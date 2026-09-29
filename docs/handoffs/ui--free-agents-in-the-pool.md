---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Free agents are now worth showing

## Why
Until now the free-agent list was empty or held one player rated about 40, so it looked dead. Every window now opens with 4 of the year's best free agents, and the manager has the first half of the window to sign them before AI clubs may.

## What exists
`World.FreeAgents()`, `SignPlayer` and `TransferWindow` (see `cmd/play/contracts.go` and `cmd/web/views.go`), unchanged. `TransferWindow` doesn't yet say when AI clubs may sign free agents (`Opens + WindowDays/2`); `Opens` and `Closes` are there.

## What is needed
In cmd/play and cmd/web, point at the pool when the window opens (the inbox or the squad page), and say the best free agents may go to AI clubs from the middle of the window. If you want the exact instant as a field on `TransferWindow`, ask squad.
