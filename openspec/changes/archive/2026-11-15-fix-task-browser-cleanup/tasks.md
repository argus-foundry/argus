## 1. Evidence and safety contract

- [x] 1.1 Inspect live processes read-only and document process groups, cwd, task state and Quit uncertainty.
- [x] 1.2 Write ownership, lifecycle, sweep and doctor delta requirements before code.

## 2. Implementation and tests

- [x] 2.1 Test browser discovery, personal-browser exclusion, identity changes and escalation; implement verified cleanup.
- [x] 2.2 Wire full stop and worktree removal, with regression tests and supervisor surface updates.
- [x] 2.3 Test and implement finished/missing task sweeping and doctor advisory.
- [x] 2.4 Document gotchas and revert the temporary host diagnostic.

## 3. Delivery

- [ ] 3.1 Run targeted tests, review the diff, and pass make pre-pr.
- [ ] 3.2 Archive the change, commit/push via iris and open the PR; report evidence and limitations to coord.

## Validation status

Implementation, targeted race tests, self-review, and spec archival are complete. Strict OpenSpec validation passed all 91 items. The new package's mandatory testenv bootstrap was added after the first full gate identified the omission.

The third full host gate, started at load1 8.42 with GOFLAGS=-p=2 and unchanged 300s limits, passed every changed runtime package: agent 241.133s (89.3%), browserproc 91.2%, daemon 105.192s (88.5%), doctor 98.8%. Build, vet, format, lint, and vulnerability checks passed. It failed on cumulative package timeouts in unchanged API (`TestWtDescriptorCycles/degraded`, 7s into that subtest) and TUI (`TestSmoke_JumpSwitcherFilterAndEnter`, just started). Workspace tests and the final coverage threshold were not reached. Full log: `/Users/aaron/Library/Caches/iris/check-logs/check-2252612961.log`.

Coordinator instructed no more retries after a normal-load failure. Publication remains blocked by the repository's required green pre-pr gate; no PR or push has occurred, and no existing Chrome process was signaled. Temporary host diagnostics and concurrency edits to script/iris-check are fully reverted.
