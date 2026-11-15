## 1. Proof and contract

- [x] 1.1 Research hooks with claude-code-guide and report before implementation.
- [x] 1.2 Verify interactive and print allow/deny in default and auto mode; record the harness and coordinator approval.
- [x] 1.3 Create proposal, design, deltas and tasks before production code.

## 2. Durable approvals

- [x] 2.1 Write regression tests for canonical identity, queue deduplication, expiry and concurrent single consumption.
- [x] 2.2 Implement approval storage, immutable board linkage and transactional decision notices.
- [x] 2.3 Implement authenticated resident hook RPC, CLI hook output and isolated spawn settings.
- [x] 2.4 Suppress transient hook dialogs in waiting detection and test launch scoping.

## 3. Human decisions and delivery

- [x] 3.1 Add TUI approve/deny with exact stored input and human-only authorization; test actions and rendering.
- [x] 3.2 Deliver durable prefixed notices and test restart/mismatch/denial paths.
- [x] 3.3 Update reference docs, gotchas and supervisor surface digests; document required restarts.

## 4. Validation and handoff

- [ ] 4.1 Run focused tests and full required pre-pr gates; review behavior and tests.
- [x] 4.2 Archive the OpenSpec change on this branch and validate all specs strictly.
- [ ] 4.3 Commit, push through iris, open an unmerged PR and report checks to coordinator.
