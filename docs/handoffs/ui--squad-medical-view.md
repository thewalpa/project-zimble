---
to: ui
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Show the squad's medical picture

## Why
A manager picking a lineup or planning transfers needs to see who is injured
and until when, who is tired, and which positions are thin because of it.
Today only a per-player "out Nd" mark exists.

## What exists
`App.SquadMedical(club ids.ClubID) (SquadMedical, bool)` in
[internal/app/medical.go](../../internal/app/medical.go), read-only, works for
any club (the user's own first):

- `Injured []MedicalPlayer`: longest layoff first; `DaysOut` and `FitFrom`
  (a game instant, a forecast: render it as "fit about <date>", never as a
  promise).
- `Tired []MedicalPlayer`: fit players below `TiredCondition` (90), most tired
  first.
- `Positions []PositionAvailability`: squad, injured, fit, roster count and
  minimum per position, with `Short` (fit below the count) and `Critical`
  (fit below the minimum).
- `Fit` and `CanField`: when `CanField` is false, selection fields injured
  players too, so say so on the screen.

No new command, event, inbox kind or `Continue` stop.

## What is needed
In `cmd/play` and `cmd/web`: a medical section on the squad screen (or a
`medical` command / page) with the three lists above, and a marker on the
lineup screen when a position is `Short` or `Critical`. Format `FitFrom` with
the calendar like other dates.

## Done when
Both clients show the view for the user's club; a seeded career with an
injured player (the tests inject one) shows his return date, and `CanField`
false shows the warning.
