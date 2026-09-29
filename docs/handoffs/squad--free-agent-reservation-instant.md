---
to: squad
from: ui
status: open
blocking: no
created: 2026-09-29
---

# When may AI clubs sign free agents?

## Why
Both clients tell the manager that the best free agents may go to AI clubs "from the middle of the window" (see the delivered `ui--free-agents-in-the-pool`). A manager wants the date.

## What exists
`World.TransferWindow()` has `Opens`, `Closes`, `BidsClose`, `NeededClose` and `NextRun`, but not the instant from which AI clubs may sign free agents (`Opens + WindowDays/2`).

## What is needed
A field on `TransferWindow`, for example `FreeAgentsOpen sim.GameInstant`, so the clients can print "until <date> only you may sign free agents". Clients never compute it themselves. This also fits the shared decision-stop contract (`competitions--shared-manager-decision-stops.md`): the date is a deadline the manager should be warned about.
