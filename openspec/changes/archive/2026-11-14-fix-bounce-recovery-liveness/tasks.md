## 1. Recovery candidates

- [x] 1.1 Add failing tests for stale-status live tasks, eligible bindings and exclusions.
- [x] 1.2 Implement supervisor snapshot and reconciliation; preserve idle-worker dormancy.

## 2. Restart correctness

- [x] 2.1 Test and fix recycle/account-move status; audit sibling restarts.
- [x] 2.2 Test and fix owned late-start note delivery and unrelated-session skips.
- [x] 2.3 Test and reject automatic recovery without a resumable conversation.

## 3. Ship

- [x] 3.1 Update gotchas and surface digests as needed; review diff and tests.
- [ ] 3.2 Archive change, validate specs and pass make pre-pr.
- [x] 3.3 Prepare one PR summary and restart requirements for the iris handoff to coord.

Validation evidence:

- Feature regressions and the final full daemon race/coverage suite pass.
- All 905 API top-level tests pass in disjoint 453/452 batches with race detection and the unchanged 300-second timeout.
- Combined complete coverage is 89.3%, above the 88% filtered floor; all 790 workspace tests pass.
- Host build/vet/format/lint/vulnerability checks pass. The standard full gate remains blocked by the API package's aggregate 300-second timeout, also reproduced on clean master; no push is authorized by that result.
- The bounce-recovery base spec validates strictly. Global strict validation retains missing-scenario failures in untouched desktop-web-workspace, git-integration and rest-api specs.
