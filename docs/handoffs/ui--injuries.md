---
to: ui
from: squad
status: open
blocking: no
created: 2026-09-29
---

# Show injuries in the squad, the lineup and the inbox

## Why
Matches now hurt players. Injured players are not selected, so a manager who doesn't see who is out will be surprised by the AI's suggestion and by rejected lineups.

## What exists
- `SquadPlayer.DaysOut` ([summary.go](../../internal/app/summary.go)): the recovery days (one a day) the player still misses, zero when fit. It is set in `Squad` and `FreeAgents`.
- `World.Injury(player) (daysOut uint16, injured bool)` ([injuries.go](../../internal/app/injuries.go)).
- Inbox kinds `inbox.KindInjured` (`Days`) and `inbox.KindRecovered` for the user team's players; `InboxItem.PlayerName` is filled.
- `SubmitLineup` rejects an injured player with `ErrInvalidLineup` ("player N is injured"). `SuggestLineup` and the AI pass them over. `MatchdayLineup.Dropped` lists an injured player a carried-over lineup leaves out (his place is refilled).
- Exception: a squad with too few fit players (no fit goalkeeper, or fewer than ten fit outfield players) fields its injured, and `SubmitLineup` accepts them.

## What is needed
In cmd/play and cmd/web:
- Mark injured players in the squad and lineup pages ("out 12 days") and don't offer them as choices while the rule applies; say when a carried-over lineup dropped someone because he is injured.
- Render the two new inbox kinds ("Abbott is out for 12 days", "Abbott is fit again").
- Optionally sort or filter the squad by availability.

## Done when
A managed season shows injuries in the inbox and squad in both clients, and submitting an injured player in the lineup editor is prevented or explained.
