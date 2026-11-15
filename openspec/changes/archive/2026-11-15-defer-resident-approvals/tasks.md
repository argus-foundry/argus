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

- [x] 4.1 Run focused tests and full required pre-pr gates; review behavior and tests.
- [x] 4.2 Archive the OpenSpec change on this branch and validate all specs strictly.
- [x] 4.3 Commit, push through iris, open an unmerged PR and report checks to coordinator.

Validation: GitHub CI run 43669640320 passed all gates for implementation commit ee4c8e98. Focused race regressions and local build, vet, format, lint, govulncheck and all 790 workspace tests passed. The monolithic local pre-pr run exceeded the 300-second shared-host guard; race/coverage batches run with the same guard and no skipped tests. PR #1231 targets master and remains unmerged.

## 5. Independent review corrections

- [x] 5.1 Restrict settings and hook binary to Bash/MCP tools; leave human questions and plan dialogs native.
- [x] 5.2 Scope waiting debounce to the observed chooser and matching call, with an immutable fifteen-second maximum; test unrelated hook traffic and real Waiting publication.
- [x] 5.3 Exclude only Bash description from identity, include canonical input in notices, and test execution-field mismatch.
- [x] 5.4 Prove hook exit/nonzero and timeout with onFailure:block on Claude 2.1.325 in default/auto print/PTy; record eight zero-execution continuations in design.
- [x] 5.5 Check consumption rows affected and test current-session mismatch independently of the launch file.

Final integration: merged master normally after #1228 and #1230, retaining timer/tidy behavior and the approval changes. Re-recorded spawn digest at surface 52 and verified both spawn/stream digests (stream 19). Focused race tests across nine affected packages and local build, vet, format, lint, vulnerability and workspace gates pass; all 90 specs/changes validate strictly. Exact-head full CI is the final handoff gate under the coordinator's shared-host timeout exception.
