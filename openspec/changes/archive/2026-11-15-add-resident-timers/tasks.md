## 1. Durable timers

- [x] 1.1 Write scheduling and store tests, including limits, DST and missed fires.
- [x] 1.2 Implement timer parsing, SQLite store and atomic inbox outbox handoff.
- [x] 1.3 Implement daemon delivery retry and coalescing using resident notifier.

## 2. Resident and human surfaces

- [x] 2.1 Add resident-only MCP tools and identity tests.
- [x] 2.2 Add REST listing and TUI summary with tests.
- [x] 2.3 Update orientation, reference documentation and gotchas.

## 3. Ship

- [x] 3.1 Review changes and run make pre-pr.
- [x] 3.2 Archive specs in the same PR, commit, push and open PR through iris.

## Local validation

- Direct full-suite runs reached the local 300-second API/TUI hang guard; the API timeout also reproduced on master. The coordinator approved complete batched race verification and CI as the authoritative gate.
- Batched runs executed each top-level test once: API 906, DB 569, TUI 1,585. Combined filtered coverage: 89.2% (88% floor).
- Build, vet, formatting, lint and vulnerability checks passed. Workspace tests: 790 passed; batch-runner tests: 3 passed.
- Timer schema initialization is deferred to first use. Post-change OpenInMemory benchmark medians: branch 172 ms, master 182 ms (five samples each, variable host load).
