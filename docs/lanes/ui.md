# Lane: ui

The clients a player uses: the interactive terminal (`cmd/play`), the browser client (`cmd/web`), and the output of the headless runner (`cmd/simulate`). The UI shows what `app` says and submits the player's intentions. It owns no football rules.

## Owns

- `cmd/play/**`, `cmd/web/**` (including `templates/`), and the output and flags of `cmd/simulate`.
- `internal/app/views.go`, created when first needed: read-only queries that only combine existing `app` queries (see below).
- The tests beside them: `cmd/*/main_test.go` (scripted stdin, and `httptest` for the web).

## Rules for this lane

- **No rules in clients.** Never compute a price, an age, an eligibility, a deadline, a table or a condition in `cmd/`. If the client needs a number, `app` must provide it. Asking prices come from `SquadPlayer.Value`, deadlines from the offer. If a query is missing, file a note to the lane that owns the fact.
- **Read-only queries only.** You may add a query to `internal/app/views.go` when it only combines existing `app` queries: no new state, no events, no validation, no version change. Anything more is a note.
- **Both clients.** Every player-facing feature appears in both `cmd/play` and `cmd/web`, with the same wording where they overlap. If `cmd/simulate` prints the affected output, update it too.
- **Web client:** Go `html/template` and the standard library only, with inline CSS through the tokens on `:root` in `layout.html`, in light and dark. Every page must work without JavaScript, since forms post to the server. Keep it usable at phone width.
- **Display conventions:** one 100-point scale for attributes, overall and condition. Money is formatted by `core/money`, and times with the game calendar in UTC. Players and clubs are named through `app` queries, never by ID alone.
- **`continue` stops** wherever the player must act (a matchday, the transfer window, a bid to answer). A new feature that needs a decision needs a stop in both clients. Agree on the stop condition with the lane that owns the feature.
- **Tests** drive the client the way a player would: scripted commands for `cmd/play`, HTTP requests for `cmd/web`. Assert what the player sees, not internal structures.

## Depends on

- **`match`, `competitions` and `squad`:** they file a `ui--*.md` note for every feature a player can see. Those notes are this lane's main queue.
- **`data`:** it provides inbox messages and events. Ask for a new message kind rather than inventing text from raw events.
- **`balance`:** its playtest notes about anything confusing or awkward to use.

## Now

Delivered the drag-and-drop formation editor (priority user request). The web lineup and team-plan pages open on a pitch. Dragging, tapping or using the keyboard edits the lineup form, and a new `order` field keeps each line's slot order. The terminal draws the same pitch with its formation above the lineup table. `match--line-slot-flank` is open: if the first slot of a line turns out to be the right flank, mirror the pitch in both clients. See [progress](../progress.md). Before this: the saved team-plan editor in both clients (`World.TeamPlan`/`SetTeamPlan`), cup qualifiers in league tables, match-report timelines, career history on player profiles, player comparison, squad's refusal and free-agent-date handoffs, lineup eligibility in both editors, per-nation names, and the season-review flow.

## Backlog

Player-facing gaps in the current clients. Check with the owning lane before starting anything that needs new `app` state.

- **Decision overview:** show upcoming fixtures, expiring contracts and unanswered bids together, with links or commands for the next action. Use `app` dates and eligibility; ask `squad` for the contract-planning view in [its backlog](squad.md#backlog) before showing renewal urgency. Both clients should make the next deadline clear without opening several screens.

### AI/player rule parity audit

- **P2 — Present shared decisions and assistance (PAR-02/06/07/08):** consume competitions/squad's app-owned deadlines and eligibility in both clients; replace duplicated expiry/market stop calculations when that contract lands. Show any retained free-agent assistance and optional recruitment/lineup delegation explicitly. Existing `ui--ai-seller-rules` remains the request for `BidRefusal`/`NeededClose` presentation; extend it rather than inventing client rules. Acceptance: equivalent terminal/web actions reach the same decision opportunities, show the same terms and survive save/load. See [audit](../ai-manager-parity.md).
- **P3 — Watching is presentation (PAR-09/10):** use the career's model and club-scoped observations for live/skip views; viewing a match or switching clients must not select a different football ruleset or reveal extra recruitment truth. Coordinate engine/delegation state with match and data.
