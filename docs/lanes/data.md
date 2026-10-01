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

**Club aggregates from club knowledge (PAR-10):** `World.ObservedClubs(observer)` aggregates squad positions and average overall from the observer's `ObservePlayers`; `Agenda()` reads its contract items the same way; `Summary()` is the administrative view. Match's adoption (`ca727e9`) reviewed and accepted. The terminal club heading is asked of `ui` in `ui--observed-clubs` ([progress](../progress.md#data-club-aggregates-from-club-knowledge-done)). No version moves. ui delivered save recovery in both clients (`b58f3e4`), so that backlog item is closed.

**Even league sizes enforced in content:** `League.Validate` rejects odd sizes and sizes above `content.MaxLeagueEntrants` (kept equal to competitions' bound by an app test). Closes `data--even-league-sizes` ([progress](../progress.md#data-content-accepts-only-even-league-sizes-done)).

**Cup prize tables delivered:** `content.Cup.Prizes` by exit stage, validated, with the Continental Cup's 1,000,000 / 600,000 / 350,000 / 200,000; `content.LeagueVersion` 6, save schema 31 with its fixture. Squad's posting note was updated. Closes `data--cup-prize-table` ([progress](../progress.md#data-cup-prize-tables-done)). Also accepted ui's observation composition (`data--ui-observations-adopted`). The next free numbers are `storage.SchemaVersion` 32 and `content.LeagueVersion` 7.

**Appearances and goals in careers delivered:** `events.MatchCompleted` carries `Appeared` and `Scorers` (`events.SchemaVersion` 2), filled from the checked outcome in `resolveBatch` and checked against the command's `MatchReport` on restore. Each `careers.Spell` counts `Appearances` and `Goals`. Save schema 30 with its fixture; schema 29 saves are refused. Match was told about the emission in `match--match-completed-facts`; the career columns are asked of `ui` in `ui--career-appearances-goals` ([progress](../progress.md#data-appearances-and-goals-in-careers-done)).

**Reviewed squad's observation adoption:** accepted for exact information; the conditions for scouting are in the PAR-10 backlog item below. Closes `data--squad-observations-adopted`.

**Shared club knowledge contract delivered (PAR-10 foundation):** `World.ObservePlayers(club, requested)` returns detached `ClubObservations` tagged with observer, revision and game instant. The current policy reveals exact player information to every registered club; existing squad/profile/free-agent rows use the same player projection. Asking prices remain seller policy, separate from knowledge. Controller parity, deterministic requests, rejection without partial results, detached match inputs and lifecycle/restore coverage pass. No authoritative state or version moves. Adoption is requested in `match--club-observations`, `squad--club-observations` and `ui--club-observations`; PAR-10 remains open until those consumers use the club scope ([progress](../progress.md#data-shared-club-knowledge-contract-done)).

**Reviewed competitions' explicit season champion:** `SeasonEnded.Champion` is emitted from the competition result, checked on restore, and passed through by the inbox; play-offs have neither a champion nor a finishing place. Schema 29's frozen fixture covers the play-off and league/cup cases. Accepted without production changes; closes `data--season-ended-champion` ([progress](../progress.md#data-season-champion-contract-reviewed-done)).

**Content report delivered:** `content.Describe(content.DefaultSet())` and `Report.Write` describe a content set and list every problem in it; printing it from `cmd/simulate` is asked of `ui` in `ui--content-report` ([progress](../progress.md#data-content-report-done)). No version moves.

**Answered competitions' play-off note and match's schema 28 note:** the `Promotion` comment describes the link, the ID floor (below `competitions.PlayoffBase`) is documented in content and enforced by `app`; schema 28 (offsides) needed nothing.

**Save fixtures per schema version delivered:** `internal/storage/testdata` holds a frozen save and payload shape for schema 24, from a managed career that uses every command kind and stops mid live match. `TestSaveFixtures` checks that every fixture loads or is refused explicitly, and that the current schema's fixture and shape still match the snapshot types ([progress](../progress.md#data-save-fixtures-per-schema-version-done)). A schema bump now needs `go test ./internal/storage -run TestSaveFixtures -fixture` (CLAUDE.md, AGENTS.md).

**Reviewed squad's academy intake rule (PAR-01):** no generation, content or event version moves; the `content.Quota` comment names the academy target.

**Save recovery foundation delivered:** `storage.Save` preserves one validated prior save at `storage.PreviousPath(path)` and refuses to overwrite a damaged or incompatible current file. `storage.RecoverPrevious(path)` is the explicit restore action; the UI handoff `ui--save-recovery` asks both clients to require player choice before calling it. No schema bump was needed. The next free version numbers are `worldgen.Version` 9, `content.Version` 10, `worldgen.YouthVersion` 5 (for `content.LeagueVersion` and `storage.SchemaVersion`, see the top entry).

**Reviewed match's engine choice and statistics (PAR-09):** the engine ID and version are pinned in `Versions`, an unknown engine is `ErrIncompatibleSave`, and `restoreResolve`/`restoreStep` require statistics exactly when the engine has `DetailedStats`. Nothing was missed; no data change needed.

## Backlog

- **Richer identities:** what else the registry should hold (a preferred foot, a birthplace, stadium identities), only when a milestone needs it.
- **Weaker lower divisions** (only if `balance` asks): a rating gap or smaller youth intake per division in `content.Division`.
- **Divisions of other sizes** (only when content adds them): check at load that `Division.Clubs` sums to the leagues' `Entrants` in block order (load checks only the total), and ask competitions for the football-year check it offered in `data--even-league-sizes` (seasons plus cup inside the contract year; today only `TestSeasonsStayInsideTheContractYear` covers the built-in content).

### AI/player rule parity audit

- **P3 — Shared club knowledge adoption (PAR-10):** the exact-information contract and data tests are delivered. Squad's, ui's and match's adoptions are reviewed, and the manager-facing aggregates read observations (`ObservedClubs`, `Agenda`). Before scouting hides facts, ensure every manager input uses its observing club and keep authoritative match/development inputs separate. Any stored reports, uncertainty or observation policy need save/version coverage; no such state exists yet. Before time-sensitive knowledge, add an observation-at-instant query (it replaces the local age projection in `recruitmentPlayers` and `valuation`). Once a second `registry.TeamKind` exists, agree one definition of a club's squad average with squad (`squadAverage`: senior team; the market's cache: whole club; `sellingPrice`: the player's team).
- **P1–P3 support — Durable parity settings and provenance (PAR-02/06/08/09/10):** review actor/club identity, task/command event facts and restore validation as squad/match unify their workflows. Any assistance, delegation, observations or engine choice added as authoritative state must be pinned, versioned and restored; do not derive privileges merely from `userClub`. Rule owners retain their validation logic. See [audit](../ai-manager-parity.md).
