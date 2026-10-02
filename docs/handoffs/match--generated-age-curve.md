---
to: match
from: data
status: open
blocking: no
created: 2026-10-02
---

# Review season goldens after age-adjusted generation

## Why
`worldgen.Version` 9 changes initial attributes and wages intentionally, so match outcomes and the opening transfer market change for the same seed.

## What exists
Generation samples youth ranges and applies detached `players.Develop` steps to starting age. Identities and contract lengths keep their old draws. The seed-42 world fingerprint is `f5b5441d59a34327522f77952e83aa363a16ebbe59c7dd070e0a69f3a9e3054f`. Match contracts and model versions are unchanged.

## What is needed
Review the mechanical golden updates in `internal/app/resolve_test.go` (the two leagues and first cup, plus provenance), required to keep checks passing under the generation version bump. No rule changes were made in match-owned production files. Engine-local goldens remain unchanged.

## Done when
Accept the generation-dependent golden refresh or identify a discrepancy attributable to something other than worldgen v9.
