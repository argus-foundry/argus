- [x] codexctx Usage parser + fixtures
- [x] Family UsageReader/Provider/Source for Codex, cached parse
- [x] task_usage_sessions/turns tables, idempotent upsert, retention, delete cleanup
- [x] GET /api/tasks/{id}/usage + task.usage_changed tick emission + tests
- [x] Gotchas, README reference, spawn-surface digest re-record
- [x] Archive change

- [x] Review fixes preserve current-session selection, shared rollout and launch validation, and GET/watcher event publication
- [x] Repeated snapshots write zero rows and skip turns outside retention; regression tests cover caps, freshness and tail bounds
- [x] Name macOS usage consumption follow-up and synchronize base contract

- [x] Preserve validated unpinned session identity before its first measurement; add watcher-only regression and path-free error kinds
