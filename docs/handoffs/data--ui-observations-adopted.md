---
to: data
from: ui
status: open
blocking: no
created: 2026-10-01
---

# Review UI observation composition before changing knowledge policy

## Why

Both clients now consume the human club's observations for individual player facts. This records the composition boundary for a later knowledge policy, alongside squad's adoption note.

## What exists

`internal/app/views.go` adds read-only `ObservedSquad(observer, club)`, `ObservedFreeAgents(observer)`, `ObservedPlayerProfile(observer, player)` and `ObservedTransferList(observer)`. They combine existing queries with `ObservePlayers`; every player fact is copied from observations, while `Value`, `Payoff` and `Listed` stay on negotiation queries. Both clients pass `World.UserClub()` as observer through `club()`, including reports, live lineups, comparison and other clubs' squads. No stored knowledge or uncertainty labels are introduced. Headless reports and club choice before a human manager exists remain administrative.

## What is needed

Review this composition when evolving knowledge. Public membership, eligibility, prices and match facts keep their existing contracts. `World.Summary().ClubRows.AverageOverall` is still an unscoped aggregate used by the terminal `club` heading (and the pre-manager club chooser); `Agenda()` also composes existing unscoped squad facts. Today's exact policy makes these consistent. Before hiding recruitment truth, provide or coordinate observation-aware aggregate/agenda queries so they cannot reveal facts beyond the deciding club's knowledge. UI must not invent an aggregate knowledge rule.

## Done when

The individual-player composition is accepted for today's exact policy, and the aggregate/agenda boundary is recorded for any future uncertainty work, or a concrete query extension is requested.
