# Agents

Several agents work on this repository at the same time, each in its own **lane**. This file explains how they share it. Read it after [CLAUDE.md](CLAUDE.md), which holds the project rules. Those rules apply to every lane and take precedence over this file.

## Lanes

| Lane | Charter | Owns |
| --- | --- | --- |
| `ui` | The clients the player sees: [docs/lanes/ui.md](docs/lanes/ui.md) | `cmd/play`, `cmd/web`, the presentation side of `cmd/simulate` |
| `match` | The match engine and everything on matchday: [docs/lanes/match.md](docs/lanes/match.md) | `internal/matches/**`, `internal/selection`, `internal/ai/selection.go`, `internal/app/{lineup,live,resolve}.go` |
| `competitions` | Game time, `Continue`, leagues, cups and season transitions: [docs/lanes/competitions.md](docs/lanes/competitions.md) | `internal/competitions`, `internal/core/sim`, `internal/app/{continue,season,schedule}.go` |
| `squad` | Contracts, transfers, money, condition, development and retirement: [docs/lanes/squad.md](docs/lanes/squad.md) | `internal/{employment,finance,transfers,medical,players}`, `internal/ai/{contracts,transfers}.go`, `internal/app/{contracts,lifecycle,money,transfers}.go` |
| `data` | Content, generation, identities, saves, events and read models: [docs/lanes/data.md](docs/lanes/data.md) | `internal/{content,worldgen,registry,storage,events,inbox}`, `internal/core/{ids,money,random}`, `internal/app/{world,save,journal,summary}.go`, `internal/app/boundaries_test.go` |
| `balance` | Long seeded simulations, statistics and playtesting; no production code: [docs/lanes/balance.md](docs/lanes/balance.md) | every `balance_test.go`, `docs/balance.md` |

Each lane owns the tests beside its files. A file not listed belongs to the lane that owns its package.

Owning a path means you are responsible for its design and its tests, and that other lanes ask you before changing its behavior. It does not mean nobody else may touch it: see [Hub files](#hub-files).

`competitions` and `squad` share the career between matches. `competitions` decides when things happen and who plays whom. `squad` decides who is at which club, on what terms, in what shape, and with what money. `balance` builds nothing the player sees: it measures what the other five build and reports back through notes.

### Changing the team

The split follows the module ownership in [docs/architecture.md](docs/architecture.md). Not every lane needs an agent all the time. A lane with an empty queue can stay idle, and its notes wait for it. To add a lane, create `docs/lanes/<lane>.md` on the model of the existing ones, move paths into its row above, and move matching backlog items into it. To merge two lanes, do the reverse.

## Handoff notes

When your work needs something from another lane, write it a note. Don't do their work for them, and don't wait silently. Notes live in [docs/handoffs/](docs/handoffs/README.md), one file per request, named `<to-lane>--<slug>.md`. The README there has the template and the full rules.

You must file a note when:

- **You add something a player can see or do:** a command, a query, a new event or inbox kind, or state worth showing. Tell `ui` which `app` methods and types to use. A feature is not done until its note is filed.
- **You need data you don't own:** a new query, field, content definition, generated attribute, or event from another lane.
- **You change a contract others consume:** a changed `matches` input or outcome, a changed event, or a new rule on an existing command. Tell every lane that reads it.
- **You find a bug in another lane's area.** Describe it and how to reproduce it (seed, commands). Fix it yourself only if it blocks you and the fix is small, and say so in the note.

At the start of every session, run `ls docs/handoffs/` and read every note addressed to your lane. Answer each one before picking your own next task: accept it into your backlog, deliver it, or decline it with a reason.

## Hub files

Some files are shared by every feature. The project rules require a change to add its events, snapshot fields and validation in the same commit, so any lane may edit these files **additively**, as part of its own feature:

| File | What lanes add |
| --- | --- |
| `internal/app/world.go` | a module store on `World`, loading, a `validateX` call in `Validate` |
| `internal/app/save.go` | snapshot fields, restore and version checks |
| `internal/app/journal.go` | event fact checks |
| `internal/app/continue.go` | a task kind in the cohort dispatch |
| `internal/app/boundaries_test.go` | a new package's allowed imports |
| `internal/events/events.go`, `internal/inbox/inbox.go` | event kinds and their messages |
| `internal/core/ids/ids.go` | an ID type |
| `docs/progress.md` | a section for completed work |

The owner of a hub file is its steward. The steward reviews what others added (see [Session rhythm](#session-rhythm)) and may refactor it, but must not change its behavior without a handoff note to the lanes that rely on it.

### Numbered values

Enum values are durable, and two lanes can pick the same next number at the same time. These are allocated by whoever merges first:

- task kinds (`sim.TaskKind` constants in `internal/app/*.go`)
- event kinds (`internal/events/events.go`), inbox kinds (`internal/inbox/inbox.go`), finance entry kinds (`internal/finance/finance.go`)
- `storage.SchemaVersion`, and every version constant listed in CLAUDE.md

If a rebase shows that another lane already took your number, renumber **your** value to the next free one and bump the schema version again if you need one. Never keep two values with the same number, and never renumber a value that is already on `main`.

### Goldens

A golden hash is owned by the lane that owns its version constant. `balance` never updates one. Update a golden only when your change is meant to alter that output, and bump the version in the same commit. If a golden breaks after a rebase and you didn't mean to change that output, don't update it. Find which merged change moved it and write a note to that lane.

## Git workflow

Each lane works in its own worktree and branch. The main checkout stays on `main`, clean, and is used only to integrate.

```sh
# once per lane, from the main checkout
git worktree add ../zimble-<lane> -b lane/<lane>
```

- **Start of session:** `git rebase main`, then run the three checks from CLAUDE.md. If `main` is broken, write a note to the lane that broke it before you start.
- **Commit small:** one feature or one answered note per commit. Work that fails the checks is never committed.
- **Integrate:** once a unit of work passes all three checks, rebase onto `main`, run the checks again, then fast-forward `main`:

  ```sh
  git rebase main && gofmt -l . && go vet ./... && go test ./...
  git -C /root/src/project-zimble merge --ff-only lane/<lane>
  ```

  If the fast-forward fails, someone merged first: rebase and check again. Never force-push `main`, never merge `main` with a merge commit, and don't integrate if the user asked you to wait for review.
- **Conflicts** in hub files are usually two additions side by side: keep both. Conflicts in another lane's own files mean you went outside your lane. Stop and write a note.

## Session rhythm

1. Read CLAUDE.md, this file and your lane's doc.
2. Run `git rebase main` and the three checks.
3. Run `ls docs/handoffs/` and answer every note addressed to you.
4. Look at what others changed in your area since your last session: `git log --oneline -20 -- <your paths>`. For hub files you steward, read the diffs.
5. Take the top item of your lane's backlog, or an accepted note. Build it with its tests.
6. File notes for anything other lanes must do: the UI for your feature, data you need, a contract you changed.
7. Update `docs/progress.md` (what you completed and why) and your lane's doc (backlog and "Now"). Delete any notes you delivered.
8. Run the three checks, commit and integrate.

## Starting an agent

Start each agent in its lane's worktree with a prompt like:

> You are the `<lane>` lane of project-zimble. Read CLAUDE.md, AGENTS.md and docs/lanes/<lane>.md, then follow the session rhythm in AGENTS.md.
