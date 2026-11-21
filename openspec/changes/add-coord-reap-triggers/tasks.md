## 1. Messaging

- [ ] 1.1 Test + implement `db.SendHeraSystemNotice` (self-addressed, `[argus] ` prefix required, shared caps via insertHeraMessage)
- [ ] 1.2 Test + implement `HeraMessage.SenderName` returning `argus` for `[argus] ` self-addressed bodies; update pinned label tests

## 2. Reap nudge watcher (internal/hera/reap_nudge.go)

- [ ] 2.1 Tests: batched one-message-per-coordinator, refused/busy roles skipped, excluded kinds skipped, persistent dedup (kind/status/PR fingerprint + cooldown), coordinator gap, no live coordinator binding, fail-closed on lookup errors, re-eval throttle, sweep budget
- [ ] 2.2 Implement ReapNudger: cheap pre-filter, dry-run `ArchiveRoleWith` verdict, fingerprint cache, task_meta `reap_nudged` record, per-coordinator gap, message build + send, uxlog lines
- [ ] 2.3 Implement Start/Stop loop with coalescing Poke, debounce and periodic sweep (synctest-style test)

## 3. Daemon + MCP wiring

- [ ] 3.1 Construct and start the nudger in the daemon when hera is enabled, using `heraArchiver()` seams and the hera notify service
- [ ] 3.2 Poke from the PR poller on a committed PR-state change
- [ ] 3.3 Add `mcp.Server.SetHeraReapPoke`; poke from `applyRoleStatus(done)` and `toolHeraAccept`; tests

## 4. Prompt guidance

- [ ] 4.1 Rewrite the orientation's archive guidance as the checklist block; update orientation tests (triggers present, own block, length roughly flat)
- [ ] 4.2 Rewrite the `hera` SKILL.md `hera_archive` "when to call it" list and mention the nudge; re-record the spawn surface digest (digest-only)

## 5. Docs and close-out

- [ ] 5.1 Gotchas: nudge invariants (dry-run reuse, task_meta record cleared on archive, poke-not-inline, sender label) in `context/knowledge/gotchas/orchestration.md` + index row
- [ ] 5.2 `make pre-pr` green
- [ ] 5.3 Archive the change (`openspec archive add-coord-reap-triggers --yes`), `openspec validate --all --strict`, commit, open PR via iris
