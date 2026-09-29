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
- **Saves are forever.** Keep old-save behavior deliberate: either reject incompatible saves with `ErrIncompatibleSave`, or migrate them. Never load one silently wrong. Save fixtures for past schema versions belong in `testdata/` once migrations exist.
- **Events are a contract.** An event kind's meaning never changes after it lands. The inbox is built only from events. If a lane needs a message the events can't support, the event gets the field, not the inbox.
- **New packages** need an entry in `boundaries_test.go` chosen deliberately. Review every entry another lane adds.

## Depends on

- **`match`, `competitions` and `squad`:** they add the events and state you steward. You ask them for facts when a read model needs more.
- **`ui`:** it shows the inbox and generated names. Tell it about new message kinds.
- **`balance`:** it reports implausible generated content (attribute spreads, ages, squad strength across nations).

## Now

**Per-nation name pools delivered:** each nation's own first and last names, drawn last from the nationality's pools, so only names moved (`worldgen.Version` 8, `YouthVersion` 4, `content.Version` 9, `storage.SchemaVersion` 24; [progress](../progress.md#data-per-nation-name-pools-done)). Note to `ui` (pinned names in its tests were updated; new pins after rebasing).

**Next:** save fixtures per schema version (see the backlog). The next free version numbers are `worldgen.Version` 9, `content.Version` 10, `worldgen.YouthVersion` 5, `content.LeagueVersion` 6 and `storage.SchemaVersion` 25.

## Backlog

- **Richer identities:** what else the registry should hold (a preferred foot, a birthplace, stadium identities), only when a milestone needs it.
- **Weaker lower divisions** (only if `balance` asks): a rating gap or smaller youth intake per division in `content.Division`.
- **Save compatibility:** a fixture save per schema version in `testdata/`, and a policy test that each one either loads or is refused explicitly.
- **Content validation report:** `cmd/simulate` prints what a content set defines, to help balance work.

- **Appearances and goals in careers:** per spell (or per season), once `match` agrees which facts a completed match report or event should carry for them. `careers` would read them from events like the spells.
- **Recover the previous valid save:** retain one verified previous save alongside atomic replacement and expose recovery explicitly to clients. A corrupt or incompatible load must not overwrite either file, and recovery must never silently replace the chosen career. Coordinate naming and the recovery action with `ui`; test interrupted writes and a damaged newest save.

### AI/player rule parity audit

- **P1 dependency — Controller-independent youth entitlement (PAR-01):** agree content/generation inputs with squad when it removes the AI-only vacancy refill. Keep identity allocation and intake contracts deterministic; version changed youth output and measure population effects. See [audit](../ai-manager-parity.md#par-01-youth-replenishment-depends-on-who-manages-the-club).
- **P3 — Shared club knowledge (PAR-10):** before scouting hides facts, provide one club-scoped observation contract to human views and AI decisions. Both currently receive exact ratings; this is a preventive requirement, not a current hidden-information finding. Test equal knowledge for equal club state and separation from authoritative match inputs.
- **P1–P3 support — Durable parity settings and provenance (PAR-02/06/08/09/10):** review actor/club identity, task/command event facts and restore validation as squad/match unify their workflows. Any assistance, delegation, observations or engine choice added as authoritative state must be pinned, versioned and restored; do not derive privileges merely from `userClub`. Rule owners retain their validation logic. See [audit](../ai-manager-parity.md).
