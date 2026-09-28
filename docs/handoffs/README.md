# Handoff notes

A lane writes a note here when it needs something from another lane: UI for a new feature, data it does not own, a changed contract, or a bug outside its area. [AGENTS.md](../../AGENTS.md#handoff-notes) says when a note is required.

This directory holds **open** notes only. `ls docs/handoffs/` is the queue of work between lanes, and git history holds everything that was delivered.

## Rules

- **One note per request, one file per note,** named `<to-lane>--<slug>.md`, for example `ui--release-player.md` or `data--player-nationality.md`. Separate files mean two lanes never conflict over the same note. The lane names are `ui`, `match`, `competitions`, `squad`, `data` and `balance`.
- **The requester writes** the note from the template below and commits it with the work that caused it. If the work is already on `main`, name the `app` methods and types to use. If it isn't yet, say what you need and why, and propose a shape.
- **The receiving lane answers** by setting `status`:
  - `accepted`: it is in the receiving lane's backlog. Add a line saying when, if you know.
  - `declined`: add a `## Answer` section with the reason or an alternative. The requester deletes the note once they have read it.
  - `question`: add a `## Answer` section with the question. The requester replies in the same section.
- **Delivering closes the note:** the lane that does the work deletes the note in the commit that delivers it, and the commit message says `closes handoff <file name>`.
- **Blocking:** set `blocking: yes` only when the requester can't finish a feature without the note. A lane answers blocking notes before starting anything else. While you are blocked, take the next item in your own backlog. Never build a stand-in in another lane's files.
- A note is a request between lanes, not a design document. If a note needs more than a page, the design belongs in `docs/architecture.md` or a lane doc, and the note links to it.

## Template

```markdown
---
to: ui
from: world
status: open            # open | accepted | declined | question
blocking: no            # yes only if the requester cannot finish without it
created: 2026-09-28
---

# Show released players in the squad and inbox

## Why
One or two sentences: the feature and who needs it.

## What exists
The app methods, types, events or inbox kinds already on main, with file links.

## What is needed
The concrete change. For UI: the screens, commands and messages in both
cmd/play and cmd/web. For data or contracts: the proposed type, field or query.

## Done when
How the receiving lane can tell it is finished: a test, a command sequence, a seed.
```
