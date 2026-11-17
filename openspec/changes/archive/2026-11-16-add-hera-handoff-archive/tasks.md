## 1. Shared verdict and DB twin

- [x] 1.1 Failing tests: `commonChecks` per mode (done/handoff/forced), handoff status in {in_review, complete}, hard refusals under force, outbox kept, inbox no longer blocks; `ArchivableVerdict` and `archiveUnitChecks` parity table test across modes
- [x] 1.2 Implement `HandoffVerdict`, forced path, `commonChecks(mode)`; mirror in `archiveUnitChecks(tx, role, taskID, mode)`
- [x] 1.3 Migration: `hera_archive_records` table + accessors (`InsertArchiveRecord` in-tx, `HandedOffTaskIDs`, `SettleArchiveRecord`, `ArchiveRecordsForRole`)
- [x] 1.4 Failing tests then implement in-transaction mail forward (digest to coordinator, merge notices retired as today, inbox-full/rate-limit rolls back)

## 2. Handoff check and archive primitive

- [x] 2.1 gitutil: `FetchPRHandoff` (gh pr view JSON incl. rollup, headRefOid) + `RemoteBranchTip`; pure-parser tests incl. every fail-closed case
- [x] 2.2 Move production `GitCheck` into `internal/hera`; add `HandoffCheck` constructor; daemon and TUI both use it
- [x] 2.3 `ArchiveRole` gains mode/force/reason; records written in the tx; uxlog `[hera] archive handoff|forced ...`
- [x] 2.4 `hera_unarchive` primitive (`UnarchiveRole`): task un-archive, role un-hide, revive to `in_progress`, settle record `handed back`

## 3. Poller watch

- [x] 3.1 Failing tests: archived+unsettled handed-off task is eligible; settled/forced/merged records are not; merged settles + completes + board fyi; closed-unmerged un-archives, revives, posts decision board item; revive failure still posts the item; orchestrator gone is skipped with a log
- [x] 3.2 Implement in `pollPRStatesOnce` (single tx per task); add `PRResult.CI` to the batch query

## 4. MCP tools

- [x] 4.1 `hera_archive`: `force` + `reason` params, schema-vs-decode drift test, force requires `role_name`, reason validation
- [x] 4.2 `hera_unarchive` tool (coordinator-only, `hera_accept` auth shape) + schema + drift test + resident allow/deny lists audited (`internal/config/resident.go`)

## 5. Argus archive agreement

- [x] 5.1 `hera.ArchiveTaskGuard` + tests (plain pass-through, worker pass, refusal reason + suggestion, coordinator refusal, forced)
- [x] 5.2 Wire MCP `task_archive`, REST `setArchive` (409 + force body), local TUI `OnArchive` (notice + force confirm modal)
- [x] 5.3 DB backstop `ErrHeraRoleLive` in `SetArchived` and `Update` (0→1 flip); audit every `SetArchived`/`Update` caller (nuke, heraactions, TUI, apistore, tests); pin test that `hera_archive` archives the task
- [x] 5.4 Verify web and macOS surface the 409 reason; record named follow-ups (force UI, handed-off label)
- [x] 5.5 TUI: show handed-off / forced label (+ reason) in the inbox viewer (archive history entries). The rail role row label is a named follow-up (archived roles are already hidden/dimmed there).

## 6. Docs

- [x] 6.1 hera skill (`internal/skills/builtin/hera/SKILL.md`) + coordinator orientation (`internal/agent/hera_spawn.go`): handed-off archive, force, unarchive; re-record skill digest
- [x] 6.2 README Reference: `hera_archive` params, `hera_unarchive` row, REST archive endpoint note
- [x] 6.3 Gotchas (`context/knowledge/gotchas/orchestration.md`, `misc.md` index): record table vs task_meta wipe, poller eligibility, closed-vs-merged, digest-not-forwards, backstop + caller audit, verdict twins in sync

## 7. Verify and archive

- [x] 7.1 Targeted package tests while iterating; `make pre-pr` once at the end
- [ ] 7.2 `/ralph-review add-hera-handoff-archive`, then `openspec archive add-hera-handoff-archive --yes` and `openspec validate --all --strict` in the PR
