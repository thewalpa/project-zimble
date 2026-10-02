# UI of choice

Which technology should the player-facing game be built in, before the current web client grows too large to change course? This document records the research and the ui lane's recommendation. It closes the "What is the UI of choice" item in [lanes/ui.md](lanes/ui.md). The owner decided on 2026-10-02; see [Decisions](#decisions).

Researched on 2026-10-02 against `main` at `4589ff6`.

## Recommendation

**Keep the browser as the UI. Restructure it before it grows further, and don't adopt a game engine for the management screens.** Concretely:

1. **Share the presentation layer between clients now.** The clients already duplicate wording. That duplication, not HTML, is the complexity to fix first.
2. **Redesign the web client on the same stack**: page components, a real visual system and an app shell. The server renders all HTML; JavaScript makes it dynamic (partial updates, the live match, the pitch editor).
3. **Package it as a desktop app later** with a webview shell (Wails or similar) around the same Go server, if and when the game is distributed. That needs no rewrite.
4. **Revisit an engine (Godot) only for the match viewer**, as a separate presentation fed by the existing `matches.Frame` stream. Do this when the match engine produces more than a browser canvas can draw well. The management screens stay in HTML.
5. **Reduce `cmd/play` to a developer console**, so that new visual features (pitch editor, replay, charts) no longer need a terminal equivalent.

The reasoning follows.

## What the UI has to do

A football manager is mostly a data application with one animated screen.

- **Management screens** are almost all of the game: squad lists, player profiles, comparison, tables, fixtures, cups and play-offs, inbox, finances, transfers and contracts, history, lineup and team plan. Today that is 15 career pages and 16 form actions in [cmd/web/server.go](../cmd/web/server.go). They draw on about 70 `World` queries and commands, which hold every rule. The work is dense tables, sorting, filtering, forms, cross-links and readable text, at desktop and phone width.
- **The match** is one screen. The tick engine produces positional frames at 5 per second (`tick.TicksPerSecond`): a ball and 22 points in centimetres (`matches.Frame`). The browser shows them today as an SVG replay sampled once per second. Live control is stop, substitute and change mentality.
- **Fixed constraints** from [CLAUDE.md](../CLAUDE.md) and [lanes/ui.md](lanes/ui.md) apply to any client:
  - There are no rules in clients.
  - Money is formatted by `core/money`.
  - Times use the game calendar in UTC.
  - Commands carry a `CommandID` and the expected revision.
  - The core is a headless Go library with one writer.

  A client in another language therefore receives display-ready data and sends back intentions. It can't format a price or check an eligibility itself.

## Where the current complexity comes from

The complaint is that the simple web version is becoming too complex. Measured on `4589ff6`:

| Part | Size | What it is |
| --- | --- | --- |
| `cmd/web/views.go` + `transfers.go` + `live.go` + `sort.go` | ~2,900 lines of Go | Turning `app` queries into page data: names, match labels, message text, marks, sorting |
| `cmd/web/templates/*.html` | ~1,450 lines | Markup and CSS; one stylesheet in `layout.html` |
| `cmd/play/*.go` | ~3,500 lines | The same compositions again, printed as text |
| Client tests | ~2,700 lines | Scripted terminal sessions and HTTP requests |

Most of the weight is **view composition**, not rendering. The same composition exists twice:

- Inbox wording: `messageText` (web) and `printMessage` (terminal).
- Match names: `matchName` and `itemMatchName`, in both clients.
- Result letters: `outcome`, in both clients.
- Shoot-out text: `penalties`, in both clients.
- Career spells: `playerJoined` (web) and `careerJoined` (terminal).

Every new feature is written twice, tested twice and worded twice. Replacing HTML with any other renderer keeps all of this composition and adds a third copy or an API in front of it. The templates themselves are small and the CSS is already tokenized for light and dark. The visual plainness is a design problem, not a technology limit.

## Options

Each option is judged on the same questions. How do the management screens and the match viewer fare? What has to sit between the client and the Go core? What does it cost while lanes keep adding features? Can it ship to players?

| Option | Management screens | Match viewer | Bridge to the Go core | Cost while features keep coming | Shipping |
| --- | --- | --- | --- | --- | --- |
| **A. Server-rendered web, redesigned** (current stack) | Strong: HTML tables, forms, links, text and accessibility come free | Good for 2D: SVG today, canvas with interpolation next | None: same process, same Go types | Lowest: one language, existing tests keep working | Local server plus browser now; desktop via a webview shell |
| **B. Web front end (TypeScript SPA) over a JSON API** | Strong, with richer interaction | As A | A typed JSON API for about 70 queries and 16 commands, plus display strings from Go | High: two languages, a build toolchain, an API contract to version, and tests on both sides | As A |
| **C. Desktop webview shell** (Wails v2 stable, v3 in beta) | Same as A or B: it wraps them | Same | None beyond A or B | Small, once | Native window, installers, Steam |
| **D. Godot 4** (4.7 stable) | Weak to fair: Control nodes and Containers can do it, but sortable dense tables, rich text and forms are hand-built; GDScript or C# | Strong, 2D or 3D | Either a local JSON/WebSocket API (as B), or cgo GDExtension bindings for Go (godot-go and graphics.gd, both experimental) | Highest: every screen rewritten in a second language and kept in step with each lane's features | Excellent: desktop, mobile, web export |
| **E. Native Go UI** (Gio, Fyne; Ebitengine for 2D) | Fair: one language, but small widget sets and no table or rich-text layer to match HTML | Good (Ebitengine) | None | Medium: a rewrite of the screens, but in Go | Single binary, no browser |
| **F. Unity** | As D | Strongest | As D, but C# only | As D | As D |

Notes on each:

- **A** keeps the property that has paid off so far: a client is a thin layer over `app` queries, tested by driving it the way a player does. The limits are interaction (full page reloads, no live animation without script) and looks. Both can be addressed without leaving the stack. Allow small, embedded scripts as progressive enhancement, as the lineup drag-and-drop and the live replay already do. Every form still works without JavaScript.
- **B** pays off when the UI team is large, or when several front ends share one API. Neither holds here. It would move display wording into TypeScript, which conflicts with "money formatted by `core/money`". Otherwise Go must send preformatted strings, and then the SPA mostly renders server-made view models, which is what A already does.
- **C** is not an alternative to A but its distribution step. The Go server and its pages run unchanged inside a native window. Wails v3 is in beta with a stable desktop API, and v2 is the stable line.
- **D** is the strongest option for a rich match presentation and for shipping on many platforms. For the 95% of the game that is data screens, it trades HTML's tables, text and forms for hand-built Control trees. It also needs a bridge for every query and command. Football Manager is the reference case: FM26 rebuilt its UI in Unity, and reviewers singled out the interface and navigation as its weakest part. A game engine is a good match renderer and an expensive spreadsheet.
- **E** avoids a second language. Its widget libraries are thinner than HTML for exactly the screens this game is made of, and it gives up phone-width access through a browser.
- **F** has D's trade-offs, plus licensing and a heavier toolchain, with nothing to offset them for a 2D data game.

## Plan

Each step can be stopped after and leaves a working game.

1. **Shared presentation (ui).** Move what both clients compose into one place: message text, match names, result letters, career spells, table marks. Each client then lays the result out its own way. Small read-only combinations can live in `internal/app/views.go` today. Wording does not fit `app` well. It belongs in a presentation package under `cmd/`, e.g. `cmd/internal/present`, imported by `cmd/play`, `cmd/web` and `cmd/simulate`. The package has its own entry in `boundaries_test.go`. Acceptance: each phrase that both clients print is defined once, and both clients' tests pass unchanged.
2. **Web redesign (ui).**
   - Split `views.go` by page.
   - Turn repeated markup (player rows, fixture rows, team badges, pitch) into named templates.
   - Add an app shell: a persistent club header with date, next match and Continue; navigation grouped as Club, Competitions, Market and Inbox.
   - Add a type scale and spacing tokens beside the colour tokens.
   - Polish tables: sticky headers, compact density, column choice on wide screens.

   Acceptance: same routes and forms, existing HTTP tests pass, phone width still works.
3. **Dynamic UI (ui, with match).** The owner named the static feel of the current UI as its worst problem.
   - Replace full page reloads with partial updates: a form posts as today and the server answers with the HTML fragment that changed (the squad table, the lineup, the inbox), rendered by the same templates.
   - Animate the live match on a canvas, interpolating the 5 Hz frames instead of sampling once a second. The template renders the frames into the page; nothing fetches them from an API.
   - Add keyboard shortcuts for Continue and the main pages, and instant client-side sorting and filtering of tables the page already holds.

   Scripts are embedded in the binary, with no build step. The server answers with HTML, never JSON.
4. **Desktop packaging (ui), when distribution is on the roadmap.** Wrap `cmd/web` in a webview shell. Saves stay files beside the binary. Nothing in `app` changes.
5. **Match viewer in an engine (match + ui), only if a trigger below fires.** A separate Godot viewer would need `matches.Frame` and match events as data, over a local WebSocket from the same server. That is the one place this plan would need a non-HTML interface, and the owner ruled those out for the UI, so it needs their agreement when a trigger fires. Management stays in HTML. Frames are already a presentation-only contract, and the match engine never changes the world, so the viewer can't break any rules.

## What would change this recommendation

- **The match presentation becomes the product's centre:** animated players, camera, 3D. A browser canvas then stops being enough, so take step 5 and keep the rest.
- **Mobile or console stores become a target**, needing native packaging beyond a webview: re-evaluate Godot for the whole client, budgeting a JSON API and a full rewrite of the screens.
- **A dedicated front-end developer joins:** option B becomes affordable. It is still worth it only if the API serves more than one client.

## Decisions

The owner decided on 2026-10-02:

1. **The web stays the UI; an engine only for the match viewer, later.** Accepted.
2. **`cmd/play` becomes a developer console.** Accepted. It keeps its commands for scripted tests and debugging, and keeps every decision the player must make, so that a scripted career can still make them. New visual features no longer need a terminal version. [lanes/ui.md](lanes/ui.md) carries the new rule.
3. **A shared presentation package under `cmd/`** (step 1). Accepted.
4. **JavaScript.** Allowed for a dynamic UI, more broadly than proposed: pages no longer have to work without it. The application stays server-rendered at its core. The server renders every page and fragment as HTML with `html/template`, every change is a form post to the server, and there is no JSON API.

## Sources

- Godot download archive, 4.7 stable line: https://godotengine.org/download/archive/
- GDExtension bindings for Go: https://github.com/godot-go/godot-go (experimental), https://github.com/quaadgras/graphics.gd
- Wails v3 beta announcement and stability notes: https://v3.wails.io/blog/wails-v3-beta/
- Ebitengine: https://ebitengine.org/
- htmx releases: https://github.com/bigskysoftware/htmx/releases
- Football Manager 26 on Unity and the reception of its interface: https://en.wikipedia.org/wiki/Football_Manager_26, https://www.operationsports.com/if-fm26s-ui-is-going-to-stay-this-way-at-least-let-us-mod-it
