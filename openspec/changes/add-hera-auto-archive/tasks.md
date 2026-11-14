## 1. Predicate and primitive

- [ ] 1.1 Write failing tests for `internal/hera.ArchivableVerdict` covering each D1 clause and fail-closed unknowns
- [ ] 1.2 Implement `ArchivableVerdict` using an injected store + `mergesafety.ClassifyTask` seam
- [ ] 1.3 Write failing tests for `internal/hera.ArchiveRole` (re-verify, SetArchived, role hide, idle stop, busy refuse, idempotent)
- [ ] 1.4 Implement `ArchiveRole`; add uxlog `[hera] archive ...` calls for success, refusal, skip

## 2. MCP tool

- [ ] 2.1 Write failing tests for `hera_archive` (single, sweep, dry_run, non-coordinator, own role, top-level coordinator refusal)
- [ ] 2.2 Implement `toolHeraArchive` and register its schema (mirror `hera_accept` auth shape); add schema-vs-decode drift test entry

## 3. Guidance and docs

- [ ] 3.1 Update coordinator orientation and the builtin `hera` skill with the proactive-archive duty; re-record skill digest
- [ ] 3.2 Add `hera_archive` row to README Reference MCP table
- [ ] 3.3 Add gotchas to `context/knowledge/gotchas/orchestration.md` (archive wipes task_meta/mail; non-destructive; fail-closed)

## 4. Verify and archive

- [ ] 4.1 Run `make pre-pr`
- [ ] 4.2 Run `/ralph-review add-hera-auto-archive`, then `openspec archive add-hera-auto-archive --yes` in the PR

## 5. Process teardown

- [ ] 5.1 Test that an archived task's session is stopped and its dev stack is picked up by the finished-task devstack sweeper (add archived-by-`hera_archive` case)
