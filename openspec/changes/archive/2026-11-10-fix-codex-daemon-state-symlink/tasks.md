## 1. Regression coverage

- [x] 1.1 Add a failing test for a source daemon state directory and an existing overlay symlink, including default and account-specific homes.
- [x] 1.2 Verify source daemon state and unrelated overlay links remain intact.

## 2. Implementation

- [x] 2.1 Exclude `app-server-daemon` from source-home linking and reconcile the overlay entry to a real, private directory on every Codex launch.
- [x] 2.2 Record the startup invariant in the relevant gotcha file and merge this delta into the base spec before PR merge.

## 3. Verification

- [x] 3.1 Run focused tests and `make test` (the full race suite passed as part of `make pre-pr`).
- [x] 3.2 Run `make pre-pr` before pushing or opening a PR.
