---
to: match
from: ui
status: open
blocking: no
created: 2026-09-28
---

# Preserve manager's lineup selection across matchdays

## Why
When the manager customizes and submits a lineup for a matchday, that lineup is currently only saved for that specific fixture. On the next matchday, the manager's lineup is lost: `SubmittedLineup` returns false, `SuggestLineup` generates a new AI lineup from scratch, and if the manager continues or plays the match from the home screen, `ResolveRounds` plays with `SelectedByAI`. Players expect their saved lineup to persist across matchdays as their standing selection (if possible) unless they explicitly change it or players become unavailable.

## What exists
- `selection.Store` in `internal/selection` stores submitted lineups per `(Fixture, Team)`. It retains all submitted entries in `Snapshot()`, which is saved in `app.Snapshot.Lineups` across saves.
- `World.SubmittedLineup(fixture ids.FixtureID) (selection.Lineup, bool)` in `internal/app/lineup.go` only checks for the exact fixture ID.
- `World.SuggestLineup(fixture ids.FixtureID) (selection.Lineup, error)` in `internal/app/lineup.go` always runs `ai.SelectTeam`, generating a new lineup and discarding past manager choices.
- `World.ResolveRounds(cmd ResolveRounds)` in `internal/app/resolve.go` falls back to `w.selectTeam(team, rules)` with `SelectedByAI` if no lineup was explicitly submitted for the pending fixture.
- In `cmd/web/views.go` and `cmd/play/main.go`, both clients call `SubmittedLineup(fixture)` and fall back to `SuggestLineup(fixture)`.

## What is needed
In `match` lane (`internal/app/{lineup,resolve}.go` and `internal/selection`):
1. **Lineup carryover in `app`**: When a matchday has no newly submitted lineup for the pending fixture, `app` should carry over the manager's last submitted lineup if it is still valid for the current squad and competition rules (all players still employed, exactly 1 GK, 11 starters, bench within `MaxBench`).
2. **`SuggestLineup` / default lineup**:
   - When suggesting or presenting the lineup for the next fixture, return the preserved lineup (or an adjusted version if players left/retired) rather than generating an AI lineup from scratch.
   - If a player in the preserved lineup is no longer in the squad (transferred/released/retired), handle the missing slot gracefully (e.g. fill from available players or report invalidity).
3. **`ResolveRounds` carryover**:
   - If the manager does not re-submit a lineup for the new matchday (e.g. clicking "Play match" or "Continue" directly), `ResolveRounds` should field the preserved lineup with `SelectedByManager` rather than falling back to `SelectedByAI`.
4. **Exported query for clients**:
   - Provide a way for clients to distinguish whether the lineup shown for a pending fixture is:
     - Newly submitted for this fixture.
     - Carried over from the previous matchday.
     - An assistant AI suggestion.
   - So both `cmd/web` and `cmd/play` can display the correct state description (e.g., "Your saved lineup", "Carried over from last match", or "The assistant's suggestion").

## Done when
1. In a multi-matchday sequence (e.g. Round 1 to Round 2):
   - The manager submits a custom lineup in Round 1 (e.g. with specific starters, bench, and mentality).
   - After Round 1 resolves and Round 2 becomes pending:
     - Navigating to `/lineup` in `cmd/web` or typing `lineup` in `cmd/play` shows the lineup from Round 1 rather than a new AI suggestion.
     - Clicking "Play match" without re-submitting in Round 2 resolves Round 2 using the manager's lineup, with `SelectedByManager` in the match report.
2. If a player from the preserved lineup leaves the club (sold/released) between matchdays, the invalid slot is handled gracefully (either prompted in UI or fallback to AI with notification).
