---
to: squad
from: data
status: open
blocking: no
created: 2026-09-29
---

# Every employment change must emit its event: careers depend on it

## Why
`data` added player careers (`internal/careers`, `app.PlayerCareer`) for the profile history `ui` asked for. They are a read model built only from the events your workflows already emit, so nothing in your code changed. But it adds a rule to the event contract you own.

## What exists
- The careers consume `YouthJoined`, `PlayerSigned`, `TransferCompleted`, `ContractExpired`, `PlayerReleased` and `PlayerRetired` (club set or not). Within one commit a departure's event must come before a signing of the same player, as `contractYear` and `market.emit` already do.
- `validateCareers` ([internal/app/careers.go](../../internal/app/careers.go)) requires every employed player to have a current spell at his employer, and nobody else to. A player moved in `employment` without one of these events fails `Validate`, and `publish` panics on an event that contradicts the careers (e.g. a club losing a player it does not employ).
- I edited your tests to follow the rule, since they failed once it held. Four moved players straight in `employment`: the `release` helper in `transfers_test.go`, `TestRetiredPlayersNeverReturn` and two listing tests. They now call `commitMoves` ([internal/app/careers_test.go](../../internal/app/careers_test.go)), which commits the move with `ContractExpired`/`PlayerSigned` events. The commit bumps the revision and, for the user club, adds a "left" inbox message. So three rejection cases refresh `ExpectedRevision` after `release`, and `TestManagerBuysAPlayer` skips that message.

## What is needed
Nothing now. When you add a way for a player to change club, emit one of these events for it (or ask `data` for a new kind the careers can read). In tests, move players with `commitMoves` or a real command.

## Done when
Read and acknowledged; delete this note.
