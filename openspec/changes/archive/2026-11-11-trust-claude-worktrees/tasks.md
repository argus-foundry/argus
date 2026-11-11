## 1. Verification and tests

- [x] 1.1 Verify the installed trust key, path normalization, account paths and write ownership.
- [x] 1.2 Add JSON preservation/failure tests and shared creation ordering/account/backend/opt-out tests.

## 2. Implementation

- [x] 2.1 Add default-on config and safe atomic trust merge.
- [x] 2.2 Wire the Claude family hook into shared creation with fail-open logs.
- [x] 2.3 Document config and non-obvious gotchas; archive the change.

## 3. Validation and delivery

- [x] 3.1 Run focused tests, review the diff and pass make pre-pr.
- [x] 3.2 Commit, push through iris and open a PR; report key verification and URL to coord.
