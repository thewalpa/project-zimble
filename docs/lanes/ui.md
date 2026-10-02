# Lane: ui

The clients a player uses: the interactive terminal (`cmd/play`), the browser client (`cmd/web`), and the output of the headless runner (`cmd/simulate`). The UI shows what `app` says and submits the player's intentions. It owns no football rules.

## Owns

- `cmd/play/**`, `cmd/web/**` (including `templates/`), and the output and flags of `cmd/simulate`.
- `internal/app/views.go`, created when first needed: read-only queries that only combine existing `app` queries (see below).
- The tests beside them: `cmd/*/main_test.go` (scripted stdin, and `httptest` for the web).

## Rules for this lane

- **No rules in clients.** Never compute a price, an age, an eligibility, a deadline, a table or a condition in `cmd/`. If the client needs a number, `app` must provide it. Asking prices come from `SquadPlayer.Value`, deadlines from the offer. If a query is missing, file a note to the lane that owns the fact.
- **Read-only queries only.** You may add a query to `internal/app/views.go` when it only combines existing `app` queries: no new state, no events, no validation, no version change. Anything more is a note.
- **The web is the game; the terminal is a developer console** ([decided](../ui-choice.md#decisions)). Every player-facing feature appears in `cmd/web`. `cmd/play` keeps a command for every decision the player must make (so scripted careers and tests can make it) and the reads needed to check a career. It gets no visual features (pitch editor, replay, charts). Wording that more than one client prints is defined once in `cmd/internal/present`. If `cmd/simulate` prints the affected output, update it too.
- **Web client: server-rendered, dynamic in the browser.** Go `html/template` renders every page and every partial update; the server answers with HTML, never JSON, and there is no JSON API. Every change is a form post that the server handles. JavaScript is welcome to make the UI dynamic (partial updates, drag and drop, animation, the live match, client-side sorting of what the page holds); it is embedded in the binary with no build step. The Go side uses the standard library only. CSS is inline through the tokens on `:root` in `layout.html`, in light and dark. Keep it usable at phone width.
- **Display conventions:** one 100-point scale for attributes, overall and condition. Money is formatted by `core/money`, and times with the game calendar in UTC. Players and clubs are named through `app` queries, never by ID alone.
- **`continue` stops** wherever the player must act (a matchday, the transfer window, a bid to answer). A new feature that needs a decision needs a stop in both clients, so scripted careers in the console reach it too. Agree on the stop condition with the lane that owns the feature.
- **Tests** drive the client the way a player would: scripted commands for `cmd/play`, HTTP requests for `cmd/web` (a partial update is a request too). Assert what the player sees, not internal structures.

## Depends on

- **`match`, `competitions` and `squad`:** they file a `ui--*.md` note for every feature a player can see. Those notes are this lane's main queue.
- **`data`:** it provides inbox messages and events. Ask for a new message kind rather than inventing text from raw events.
- **`balance`:** its playtest notes about anything confusing or awkward to use.

## Now

Delivered the shared presentation package `cmd/internal/present`: inbox messages, match names, result letters, shoot-out text, dates and career spells are now worded once for all three clients, and the web's date columns sort by game time. Next: the web redesign (UI of choice, below), unless the tired-starter cue lands first. Before this: the owner decided the UI direction ([ui-choice.md](../ui-choice.md#decisions)): the web is the game, `cmd/play` is a developer console, and JavaScript may make the server-rendered web client dynamic (HTML only, no JSON API). Before this: Wrote the UI-of-choice research. Before this: Delivered career appearances and goals in both player profiles: apps and goals per club spell, and a total (shared `app.CareerTotals`) when a player has more than one. Reviewed the August-to-May calendar and midweek-cup fixture updates; the "no cup drawn yet" text in both clients now says the cup is played midweek during the following season. Next: the UI-of-choice research document (see Future decisions), unless the tired-starter cue lands first. Before this: club observations in both clients: shared read-only `ObservedSquad`, `ObservedFreeAgents`, `ObservedPlayerProfile` and `ObservedTransferList` views use the human club for squad/lineup, profiles/comparison, market/listings, reports and live lineups. The terminal club heading also reads its squad aggregate from `ObservedClubs`. Existing price, payoff and eligibility queries still own their terms; current exact information and saves are unchanged. Auto-resolved batches are now shown in both clients: dated batch counts and scores on Continue and season runs, with a browser result list and explicit score-only reports. Before this: `cmd/simulate -content`: seed-free content inspection, diagnostic errors and exclusive flags. Reviewed the seeded-test changes for simple v5; formatting is clean. Delivered promotion play-offs in the clients: play-off fixtures named with their divisions, a play-offs page and `playoffs` command, play-off places versus decided movement in every table (`app.TableMark`), and season messages without a champion. Before this: the tick engine in the clients: `-engine` for new careers (all three, engine shown with the career), a live match in the browser (`/live`: play to a stop, substitutions, mentality) with an SVG pitch replay of `LiveFrames`, and match statistics (`app.StatLines`) in reports and the live match in both clients. Before this: the player overall in the read-only formation views (report and probable lineups, both clients). Before this: explicit recovery of a previous save in both clients (terminal prompt and `recover`, web chooser/home buttons and `POST /recover`; never automatic). Before this: both sides' played formations in the match report and another club's probable lineup (web squad page, `club` in the terminal), the manager's played formation, the decision overview: `World.Agenda()` feeds an "Ahead" panel on the web home page and the `agenda` command in the terminal (the matchday, bids to answer, next matches, expiring contracts, own bids, the window). See [progress](../progress.md). Before this: the drag-and-drop formation editor (the pitch is not mirrored: `match` confirmed slot order runs left to right), the saved team-plan editor in both clients, cup qualifiers in league tables, match-report timelines, career history on player profiles, player comparison, squad's refusal and free-agent-date handoffs, lineup eligibility in both editors, per-nation names, and the season-review flow.

## Backlog

Player-facing gaps in the current clients. Check with the owning lane before starting anything that needs new `app` state.

- **Tired starters in a carried-over or planned lineup (accepted):** mark them on both lineup screens and offer `SuggestLineup` beside them. Waits for `match`'s app-owned cue ([match--tired-starters](../handoffs/match--tired-starters.md)). See [handoff](../handoffs/ui--tired-carried-lineup.md).
- **Decision overview, next step:** replace the home notes and `status` lines that the agenda now duplicates, and show renewal urgency (positions at risk) once `squad` delivers its [contract-planning view](squad.md#backlog). Show the deadline on the season-review stop when `competitions` delivers it.
- **Formation View, rest:** the formation is shown for both sides of a report and for other clubs' squads, and the live match has a pitch replay on engines with frames. Left: a formation view of the live match with substitutions applied on `simple`, if `match` wants one.
- **Live match, next steps:** text commentary from frames in the terminal (waits for `match`'s phase 3); a per-match statistics line in `cmd/simulate` if balance wants one; live statistics and score line instead of the end result when watching a live game; simple engine games should start at minute 0 and not at half time

### UI of choice

Decided 2026-10-02 ([ui-choice.md](../ui-choice.md#decisions)). In order:

- **Web redesign on the same stack:** `views.go` split by page, named templates for repeated rows, an app shell (club header, grouped navigation), type and spacing tokens, denser tables.
- **Dynamic UI:** partial updates (forms post, the server answers with the changed HTML fragment), the live match on a canvas interpolating the 5 Hz frames rendered into the page, keyboard shortcuts, client-side sorting.
- **Later:** desktop packaging with a webview shell; a Godot match viewer only if a trigger in the document fires (it would need the owner's agreement for a data stream).

### AI/player rule parity audit

- **P2 — Present shared decisions and assistance (PAR-02/06/07/08):** consume competitions/squad's app-owned deadlines and eligibility in both clients; replace duplicated expiry/market stop calculations when that contract lands. Show any retained free-agent assistance and optional recruitment/lineup delegation explicitly. Existing `ui--ai-seller-rules` remains the request for `BidRefusal`/`NeededClose` presentation; extend it rather than inventing client rules. Acceptance: equivalent terminal/web actions reach the same decision opportunities, show the same terms and survive save/load. See [audit](../ai-manager-parity.md).
- **P3 — Watching is presentation (PAR-09/10):** use the career's model and club-scoped observations for live/skip views; viewing a match or switching clients must not select a different football ruleset or reveal extra recruitment truth. Coordinate engine/delegation state with match and data.
