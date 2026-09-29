---
to: ui
from: data
status: open
blocking: no
created: 2026-09-29
---

# Show nationalities

## Why
Players now have a nationality and clubs play in a nation. The manager should see where a signing or a squad member comes from.

## What exists
- `app.SquadPlayer.Nationality` (the nation's name; empty only for an unknown player) is set wherever a `SquadPlayer` is built: squads, free agents, the transfer list and market views that use `squadPlayer`.
- `app.ClubSummary.Nation`: the country the club plays in.
- About 15% of a generated squad is foreign, about 5% of each youth intake.
- Names are still drawn from one global pool, so a name does not match its nationality yet.

## What is needed
Show the nationality in both clients: a column in the squad, transfer and free-agent tables (with a sort in `cmd/web`), and on the player page; show a club's nation next to its name where clubs are listed (league tables, the transfer market).

## Done when
Both clients show a nationality for every player row and the club's nation wherever a club is picked.
