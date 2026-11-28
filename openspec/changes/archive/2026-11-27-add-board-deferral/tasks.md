## 1. Durable behavior

- [x] 1.1 Add failing regressions for every kind, idempotency, stale revisions, update preservation and attention suppression.
- [x] 1.2 Implement timestamp persistence, revision-checked REST mutation, list output and shared attention predicates with uxlog.

## 2. Client behavior

- [x] 2.1 Add TUI defer/restore and visibility keys, counts, filtering, help and README coverage.
- [x] 2.2 Add web per-item controls, visibility toggle/count, Decide filtering, regressions and SW_VERSION bump.
- [x] 2.3 Evaluate macOS board rendering and hide deferred items where present.

## 3. Delivery

- [x] 3.1 Document gotchas, run focused tests and review the diff.
- [x] 3.2 Archive the change, validate specs and run make pre-pr with Go parallelism 3.
- [x] 3.3 Prepare the reviewable branch and PR description without merging.
