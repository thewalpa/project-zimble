# Lane: data

The data the world starts from and how it is kept: content definitions, world generation, identities, the core value types, saves, the event contract and the read models built from it (the inbox and player careers). This lane is also the steward of most [hub files](../../AGENTS.md#hub-files).

## Owns

- `internal/content` (definitions, leagues, cups, tuning), `internal/worldgen`, `internal/registry`.
- `internal/core/ids`, `internal/core/money`, `internal/core/random`.
- `internal/storage` (the save codec and `SchemaVersion`), `internal/events`, `internal/inbox`, `internal/careers`.
- `internal/app/world.go` (loading and `Validate`), `save.go`, `journal.go`, `summary.go`, `careers.go`, and `boundaries_test.go`.
- Versions: `content.Version`, `content.LeagueVersion`, `worldgen.Version`, `worldgen.YouthVersion`, `random.Version`, `events.SchemaVersion`, `storage.SchemaVersion`, and the world fingerprint golden.

## Rules for this lane

- **Steward, not gatekeeper.** Other lanes add events, snapshot fields and validation to the hub files as part of their features. Every session, read the diffs to your files since your last session (`git log -p -- internal/app/save.go internal/app/journal.go internal/app/world.go internal/events internal/inbox`). Check that the project rules held: every authoritative field is saved, restored and validated, events are emitted in the committing change, and the schema version was bumped. File a note to the lane when something was missed.
- **Content vs runtime.** Definitions are read-only and versioned. Generation turns definitions and a seed into a plain snapshot. Changing generated output for a seed means bumping a version and updating the fingerprint golden, which every lane notices. Announce it with a note to all lanes that have goldens depending on generated worlds.
- **Saves are forever.** Keep old-save behavior deliberate: either reject incompatible saves with `ErrIncompatibleSave`, or migrate them. Never load one silently wrong. `internal/storage/testdata` keeps a frozen save and payload shape per schema version; `TestSaveFixtures` checks that each loads or is refused explicitly. A migration makes an old fixture load instead of being refused.
- **Events are a contract.** An event kind's meaning never changes after it lands. The inbox is built only from events. If a lane needs a message the events can't support, the event gets the field, not the inbox.
- **New packages** need an entry in `boundaries_test.go` chosen deliberately. Review every entry another lane adds.

## Depends on

- **`match`, `competitions` and `squad`:** they add the events and state you steward. You ask them for facts when a read model needs more.
- **`ui`:** it shows the inbox and generated names. Tell it about new message kinds.
- **`balance`:** it reports implausible generated content (attribute spreads, ages, squad strength across nations).

## Now

**Answered competitions' play-off note and match's schema 28 note:** the `Promotion` comment describes the link, the ID floor (below `competitions.PlayoffBase`) is documented in content and enforced by `app`; schema 28 (offsides) needed nothing. The next free `storage.SchemaVersion` is 29.

**Save fixtures per schema version delivered:** `internal/storage/testdata` holds a frozen save and payload shape for schema 24, from a managed career that uses every command kind and stops mid live match. `TestSaveFixtures` checks that every fixture loads or is refused explicitly, and that the current schema's fixture and shape still match the snapshot types ([progress](../progress.md#data-save-fixtures-per-schema-version-done)). A schema bump now needs `go test ./internal/storage -run TestSaveFixtures -fixture` (CLAUDE.md, AGENTS.md).

**Reviewed squad's academy intake rule (PAR-01):** no generation, content or event version moves; the `content.Quota` comment names the academy target.

**Save recovery foundation delivered:** `storage.Save` preserves one validated prior save at `storage.PreviousPath(path)` and refuses to overwrite a damaged or incompatible current file. `storage.RecoverPrevious(path)` is the explicit restore action; the UI handoff `ui--save-recovery` asks both clients to require player choice before calling it. No schema bump was needed. The next free version numbers are `worldgen.Version` 9, `content.Version` 10, `worldgen.YouthVersion` 5, `content.LeagueVersion` 6 and `storage.SchemaVersion` 29 (27 and 28 are `match`'s statistics).

**Reviewed match's engine choice and statistics (PAR-09):** the engine ID and version are pinned in `Versions`, an unknown engine is `ErrIncompatibleSave`, and `restoreResolve`/`restoreStep` require statistics exactly when the engine has `DetailedStats`. Nothing was missed; no data change needed.

## Backlog

- **Richer identities:** what else the registry should hold (a preferred foot, a birthplace, stadium identities), only when a milestone needs it.
- **Weaker lower divisions** (only if `balance` asks): a rating gap or smaller youth intake per division in `content.Division`.
- **Content validation report:** `cmd/simulate` prints what a content set defines, to help balance work.

- **Appearances and goals in careers:** per spell (or per season), once `match` agrees which facts a completed match report or event should carry for them (asked in handoff `match--careers-appearances-goals`). `careers` would read them from events like the spells.
- **Complete save recovery UI:** deliver the explicit recovery choice in both clients from `ui--save-recovery`.

### AI/player rule parity audit

- **P3 — Shared club knowledge (PAR-10):** before scouting hides facts, provide one club-scoped observation contract to human views and AI decisions. Both currently receive exact ratings; this is a preventive requirement, not a current hidden-information finding. Test equal knowledge for equal club state and separation from authoritative match inputs.
- **P1–P3 support — Durable parity settings and provenance (PAR-02/06/08/09/10):** review actor/club identity, task/command event facts and restore validation as squad/match unify their workflows. Any assistance, delegation, observations or engine choice added as authoritative state must be pinned, versioned and restored; do not derive privileges merely from `userClub`. Rule owners retain their validation logic. See [audit](../ai-manager-parity.md).
