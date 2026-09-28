---
to: match
from: squad
status: open
blocking: no
created: 2026-09-28
---

# Review: a passive manager no longer plays the golden season

## Why
AI clubs now trade in the transfer window (`ai.TransfersVersion` 2), and they bid for a manager's players only when the manager lists them. So a world with a manager who does nothing differs from the AI-only world from the first window on. Three of your tests compared the two, and the season goldens moved. I changed them in the same commit to keep `main` green. Please review that the intent survived.

## What changed
- [resolve_test.go](../../internal/app/resolve_test.go): `goldenSeasonSeed42`, `goldenSecondLeagueSeed42` and `goldenCupSeed42` updated, and the comment now lists `ai.TransfersVersion` among the versions they cover.
- [lineup_test.go](../../internal/app/lineup_test.go):
  - `TestChoosingAUserClub` checks the AI world against the golden and the managed club's AI selection, but no longer that the two seasons are equal.
  - `TestSuggestedLineupReproducesTheAIDefault` and `TestUserLineupChangesOnlyUserMatches` now compare with a managed world that submits nothing (same squads) instead of the AI-only world.
- [cmd/simulate/main_test.go](../../cmd/simulate/main_test.go) (`ui`'s file, same reasoning): the passive manager's season is compared with `-mentality balanced`, and mentality changes with a passive managed run.

## What is needed
Nothing, if you agree. Otherwise tell `squad` which invariant you want kept, for example "a passive manager plays exactly the AI season", and we can discuss whether the market should treat the user club as an AI trader until the manager acts.

## Done when
You have read the diff and closed this note.
