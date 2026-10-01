---
to: ui
from: data
status: open
blocking: no
created: 2026-10-01
---

# Print the content report from `cmd/simulate`

## Why
`balance` and content authors want to see what a content set defines and whether its leagues, cups and clubs line up, without generating a career. The report is a tool for them, not a player screen, so `cmd/play` and `cmd/web` need nothing.

## What exists
`content.Describe(content.DefaultSet())` returns a `content.Report`; `Report.Write(w)` prints it as plain text ending in "no problems" or one `problem:` line each ([report.go](../../internal/content/report.go)). Output is deterministic and takes no seed.

## What is needed
A flag on `cmd/simulate` (suggestion: `-content`) that prints the report to stdout and exits, without needing `-seed`. Exit with an error when `Report.Problems` is not empty so scripts can use it. Add the flag to the commands list in CLAUDE.md and a test beside the others in `cmd/simulate`.
