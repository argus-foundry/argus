## 1. Review gate

- [x] 1.1 Read the survey, current launch/cleanup code, base specs and installed Playwright sources without browser experiments.
- [x] 1.2 Prepare proposal, design, tasks and capability deltas through spec-writer templates.
- [x] 1.3 Review in Plannotator, notify the coordinator, address annotations, and record Aaron's explicit approval before implementation or live browser experiments. Aaron returned “approved” on the proposal in folder-review session 66c5b81b-8169-4375-afd4-14a3f62dc656 on 2026-11-19; the proposed five-minute default stands, with no separate timeout answer submitted.

## 2. Resolve and test the launch contract

- [ ] 2.1 Verify supported CLI precedence/plugin suppression with isolated fixture homes, select and record an approved exact MCP/Playwright/browser pin, and return material design changes for review.
- [ ] 2.2 Write failing policy tests for headless shell selection, explicit headed bundle, missing/mismatched revision, hostile inherited options, and nested sandbox preservation; assert default execution never targets installed Google Chrome.
- [ ] 2.3 Write failing Claude/Codex launch matrix tests for account/user/project/plugin precedence, duplicates, unrelated tools, attachment exceptions, disabled/malformed sources, fresh/resume/recycle, and supervisor/in-process paths.
- [ ] 2.4 Implement shared policy/config validation and task-scoped Family delivery; add pinned adapter installation/materialization and actionable conflict/missing-runtime errors.

## 3. Browser-job lifecycle

- [ ] 3.1 Write fake-browser/transport and clock tests for sequential job closure, failure/cancellation, multiple in-flight calls, idle expiry racing resume, headed expiry in review, and attached-browser exclusion.
- [ ] 3.2 Implement managed MCP adapter, explicit job start/finish, direct script runner with finally cleanup, finite idle/renewal policy, bounded cancellation and lazy reopen.
- [ ] 3.3 Add shared Claude/Codex routing guidance for browser completion, explicit headed use, direct automation, and personal/attached-browser protection.

## 4. Process lifecycle and scanner

- [ ] 4.1 Write failing tests for natural exit, full stop, replacement restart, removal, daemon bounce, rerender preservation, old-generation escalation, PID reuse and concurrent new ownership.
- [ ] 4.2 Connect graceful close and identity-checked fallback to the supervisor/local owner lifecycle and worktree deletion ordering.
- [ ] 4.3 Write failing scanner/reaper/doctor tests where one PID vanishes or becomes unreadable while another eligible orphan progresses; cover whole-scan failures and partial diagnostics.
- [ ] 4.4 Implement candidate-local errors and exact selected macOS/Linux executable/helper recognition, preserving personal Chrome and Claude-in-Chrome.

## 5. Telemetry and documentation

- [ ] 5.1 Test and implement allowlisted launch/lifecycle/skip telemetry, including launch failure attribution and secret/URL/content redaction.
- [ ] 5.2 Update Reference docs, backend matrix and non-obvious gotchas; evaluate all three frontend stop/restart/remove paths and document browser-policy-settings-parity.
- [ ] 5.3 Prepare PR follow-up list for ai-ron, Sketch, MerchantUI, sherlock and official Playwright plugin; do not modify those sources.

## 6. Verification and delivery

- [ ] 6.1 After approval, run isolated disposable smoke jobs: default executable, sequential jobs, idle headed expiry, active/resumed operations, attached exclusion and every cleanup trigger; never use personal profiles.
- [ ] 6.2 Run focused/race/coverage checks, hera-review plus test-adversary, fix findings, and pass full make pre-pr.
- [ ] 6.3 Validate OpenSpec, archive deltas into base specs on this branch before merge, commit, push through iris, and obtain green PR CI.
- [ ] 6.4 Obtain production deployment confirmation; verify adapter install, effective configuration, both running daemon/supervisor versions and fresh Claude/Codex launches; report any unrestarted/unmanaged old sessions explicitly.
