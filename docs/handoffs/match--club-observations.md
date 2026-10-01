---
to: match
from: data
status: open
blocking: no
created: 2026-10-01
---

# Use club observations for lineup decisions

## Why
PAR-10 calls for human and AI managers to use the same club knowledge before scouting introduces uncertainty. Data has delivered the exact-information contract; selection and authoritative match inputs currently share `candidate` in `resolve.go`, so that boundary needs care when adopting observations.

## What exists
`World.ObservePlayers(club, requested)` in [summary.go](../../internal/app/summary.go) returns `ClubObservations{Observer, Revision, AsOf, Players}`. Each `PlayerObservation` carries identity, employment, position, attributes, overall, condition, injury days, contract, demand and retirement. Every registered club currently knows exact values. IDs are sorted/deduplicated, inputs and outputs are detached, unknown IDs/clubs are rejected. No save or version change.

## What is needed
Project observations for the selected team's club into AI selection inputs (including human suggestions). Keep availability and legal-lineup validation authoritative. Select player IDs/roles/tactics from knowledge, then materialize the actual engine `TeamInput` from authoritative profiles and medical records. `ai.SelectTeam` currently returns ratings in that same `TeamInput`; those must not become future scouting estimates used as match physics. Keep live, background and carried-lineup paths coherent. Ask data for any additional observation facts needed; do not change shared observation fields without a note.

## Done when
Selection for equivalent club state sees the same information for either controller. Reads do not alter selected lineups or seeded outcomes under today's exact policy. Engine inputs remain authoritative, and changing manager knowledge later cannot change a selected player's actual match attributes. No need to introduce scouting uncertainty in this adoption.
