## 1. Design approval

- [x] 1.1 Inspect supplied example rows read-only, current board specs, store, MCP and frontend coverage.
- [x] 1.2 Write proposal, design and delta specs using spec-writer instructions.
- [x] 1.3 Send the coordinator field/order/outcome choices, report blocked and wait for explicit approval before implementation (coord message 31597 relayed Aaron's approval).

## 2. Store and migration

- [ ] 2.1 Read testing and database gotchas; write failing migration tests preserving IDs, old content, decision structures, resident approvals, receipts and enforced foreign keys across repeated opens.
- [ ] 2.2 Widen the SQLite CHECK through an idempotent rebuild and add typed metadata columns with neutral defaults, without reclassifying existing data.
- [ ] 2.3 Write failing validation/update tests, then implement four-kind storage, field defaults/restrictions, metadata preservation/clearing, material-change unread state and optional kind filtering.
- [ ] 2.4 Write failing order/count/recovery tests, then implement blocking priority, quiet pending items and non-attention board todos across all attention readers.
- [ ] 2.5 Write failing completion tests for human/agent outcomes, optional note, revision conflict, incompatible outcomes, idempotent receipts, atomic notice/outcome writes and post-commit events; implement completion and uxlog success/failure/skip paths.
- [ ] 2.6 Write failing archive verdict/transaction race tests, then refuse open action/todo obligations in normal sub-coordinator archive while preserving existing force semantics and FYI forwarding.

## 3. Agent and REST contracts

- [ ] 3.1 Update named MCP decode shapes and advertised post/update/list/resolve fields, four-kind enums and descriptions; add schema-versus-decode drift tests.
- [ ] 3.2 Extend REST board/nav/count/launch-focus and completion batch contracts, preserving decision/FYI semantics; test visibility, revision checks, partial results and uncertain retry receipts.
- [ ] 3.3 Update coordinator spawn/reclaim/recycle orientation, recovery summaries and builtin hera/remind-me guidance for ownership, trigger readiness and Things naming distinction.

## 4. Frontend parity

- [ ] 4.1 Read TUI threading/keybinding gotchas; write render and SimulationScreen tests for new kinds, blocking order, quiet landing, one-key Done, todo Dropped and exact copy.
- [ ] 4.2 Implement TUI metadata and completion/copy dispatch, keymap labels, generated help assertions and asynchronous result logs.
- [ ] 4.3 Write web board/shared Decide rail/page tests for four tabs/counts, priority, all-kind launch focus, safe metadata and copy controls, keyboard/phone access and Done/Dropped with optional notes.
- [ ] 4.4 Implement shared web queue/review/copy/draft/receipt completion controls; test mixed batches, conflicts, refresh/reload, partial failures and Submit & close.
- [ ] 4.5 Bump SW_VERSION and verify native macOS has no existing board; retain the named macOS coordinator-board parity follow-up in Non-Goals.

## 5. Documentation and delivery

- [ ] 5.1 Update README Reference MCP/REST/keybinding tables and add concise migration/trigger/archive/naming gotchas; retain Things sync and historical reclassification as explicit follow-ups.
- [ ] 5.2 Run targeted package tests during implementation, then make test-cover and the complete make pre-pr gate before pushing.
- [ ] 5.3 Validate all OpenSpec changes strictly; archive this change and its deltas into base specs on this branch before the PR is merged.
- [ ] 5.4 Commit, push via iris_push, create the PR via iris_gh_pr_create and send the coordinator the URL without merging.
