## 1. Verification and implementation

- [x] 1.1 Verify Claude's write lock and test Claude/Codex parent trust with real isolated CLIs; report findings to coord before code changes.
- [x] 1.2 Add regression tests for lock contention, fresh reads after handoff, lease ownership and fail-open creation.
- [x] 1.3 Implement the compatible bounded directory lock and pre-rename guard.

## 2. Delivery

- [x] 2.1 Document verified behavior and archive this correction in the same PR.
- [x] 2.2 Pass make pre-pr, commit/push and update PR #1205 with findings; report to coord.
