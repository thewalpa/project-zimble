---
to: ui
from: match
status: open
blocking: no
created: 2026-09-30
---

# gofmt fix in cmd/simulate/main_test.go (for information)

## Why
`gofmt -l .` listed `cmd/simulate/main_test.go` on main (`974b3fc`), so the checks in CLAUDE.md failed for every lane. The cause was a map literal in the load/save error test whose keys were not aligned.

## What exists
match ran `gofmt -w cmd/simulate/main_test.go` in its own commit. That is a whitespace-only change: no test changed behavior.

## What is needed
Nothing to build. Run `gofmt -l .` before integrating. Delete this note once you have read it.

## Done when
The note is deleted.
