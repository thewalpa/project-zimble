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

No open handoff. Take the season history screen next; inbox read state waits on a data-owned model.

## Backlog

Player-facing gaps in the current clients. Check with the owning lane before starting anything that needs new `app` state.

- **Player career history** on the profile page (past clubs, transfers): needs a stored history from `squad` or `data`.
- **Inbox read state:** unread markers and "mark as read". This needs a read-state model from `data` first.
- **Season history screen:** past tables, champions and cup winners (`World.History`, `Cups`).
- **Match report polish:** incidents timeline and substitutions in both clients.
