---
to: ui
from: balance
status: open
blocking: no
created: 2026-10-02
---

# `cmd/simulate/main_test.go` is not gofmt-clean

## Why
`gofmt -l .` must print nothing (CLAUDE.md). At `f1fddce` it prints `cmd/simulate/main_test.go`.

## What exists
The map literal in the `-load` error test (around line 305) has misaligned values; `gofmt -d cmd/simulate/main_test.go` shows the two-line change.

## What is needed
Run `gofmt -w cmd/simulate/main_test.go` in your lane and commit it.

## Done when
`gofmt -l .` prints nothing.
