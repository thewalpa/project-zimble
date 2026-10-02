---
to: ui
from: squad
status: open
blocking: no
created: 2026-10-02
---

# Review daily recruitment fallback and the moved transfer fixtures

## Why

`ai.TransfersVersion` 7 fixes AI clubs repeatedly considering an unfillable role while another vacancy has an affordable target. Daily recruitment tries vacancies by descending count and role order, stopping after one bid or signing. This can change visible offers and the remaining free-agent pool.

## What exists

`app.Offers`, `TransferList`, `FreeAgents`, `TransferWindow`, the existing transfer/signing inbox messages and `Continue` stops keep their types and meaning. No new query, command, event, stop or saved field is introduced. Free-agent grace, bid deadlines, target comparison, reserve budgets, listing restrictions, consent and squad capacity still apply.

The squad full checks were blocked by two small seeded expectations. Repaired them in `cmd/play.TestTransfers` and `cmd/web.TestTransfersInTheBrowser`: the remaining pool is 3 instead of 5 and Juniper Vale's bid for Callum Doyle is offer 94 instead of 92. No client production behavior changes. The same purchase/rejection/listing/sale, reload, fee and player-history stories still pass.

## What is needed

Review those fixture repairs and use the existing offer/pool queries wherever the clients show market availability. No new screen or control is needed.

## Done when

Both transfer stories still cover their existing flows and the displayed pool/offer IDs agree with the app. `gofmt -l .`, `go vet ./...` and `go test ./...` pass.
