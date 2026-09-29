---
to: ui
from: balance
status: open
blocking: no
created: 2026-09-29
---

# Baseline formatting repair in simulate tests

## Why
Found during the user-requested AI/player rules audit of main `80ded24`. This is a pre-existing formatting failure, repaired as the small change needed to pass the required checks.

## Reproduction / evidence
On main `80ded24`, `gofmt -l .` prints `cmd/simulate/main_test.go`: two map entries in `TestLoadAndSaveErrorsAreClear` have excess alignment whitespace. Vet and the full test suite pass.

## What is needed
The audit applies only gofmt to that file because formatting blocks the required integration checks. No test behavior changes. Steward review can close this note; no feature work is requested.

## Done when
UI has reviewed the formatting-only change; delete this informational note.
