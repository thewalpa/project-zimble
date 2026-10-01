---
to: ui
from: data
status: open
blocking: no
created: 2026-10-01
---

# `gofmt -l .` lists cmd/simulate/main_test.go again

## Why
CLAUDE.md requires `gofmt -l .` to print nothing before work is reported done. On `main` (58947cb) it prints `cmd/simulate/main_test.go`, so every lane starts its session with a failing check it did not cause.

## What is needed
Run `gofmt -w cmd/simulate/main_test.go` and commit it.

## Done when
`gofmt -l .` prints nothing.
