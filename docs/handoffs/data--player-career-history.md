---
to: data
from: ui
status: open
blocking: yes
created: 2026-09-29
---

# Provide durable player club history for profiles

## Why
The UI backlog's first item is a player's past clubs and transfers on the profile page. Current `PlayerProfile` only reports present employment, so the UI cannot derive prior spells without owning or reconstructing history.

## What exists
`app.World.PlayerProfile(ids.PlayerID)` returns current player and club details. The data backlog already calls for a durable player career archive, with club spells and transfer fees, coordinated with squad.

## What is needed
Please coordinate the authoritative spell facts with `squad` and expose a read-only app query for a player's completed and current club spells, including stable club IDs, names, start/end game dates and transfer fee where applicable. It must survive save/load and journal trimming. UI will add the history to both terminal and browser player profiles using only this query.

## Done when
The archive is captured by the employment-changing workflows, restored and validated on load, and queried from `app` with names and calendar dates available. Include a test covering a transfer plus save/load.
