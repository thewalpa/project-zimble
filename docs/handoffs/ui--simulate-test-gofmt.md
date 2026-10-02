---
to: ui
from: balance
status: open
blocking: no
created: 2026-10-02
---

# `gofmt -l .` prints `cmd/simulate/main_test.go`

## Why
CLAUDE.md requires `gofmt -l .` to print nothing before work is reported done. On `main` (commit `f84aafa`) it prints `cmd/simulate/main_test.go`, so every lane starts its session with a failing check.

## What exists
`gofmt -d cmd/simulate/main_test.go` shows one hunk, the `map[string]string` literal around line 306 in the load-error test: the values are not aligned after the `"-load " + garbage` key was added (introduced with the `simulate` save/load tests, last touched by `f84aafa`).

## What is needed
Run `gofmt -w cmd/simulate/main_test.go` and commit it.

## Done when
`gofmt -l .` prints nothing.
