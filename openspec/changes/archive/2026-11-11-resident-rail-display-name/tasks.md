## 1. Resident rail label

- [x] 1.1 Restate the full resident-tab requirement with a display-name and rename scenario.
- [x] 1.2 Add render, rename and invalid-name fallback regression tests and confirm they fail first.
- [x] 1.3 Implement a resident-mode-only display override using the existing name validator and refresh paths.

## 2. Validation and delivery

- [x] 2.1 Document the shared-rail identity invariant and verify Projects/switcher scope.
- [x] 2.2 Run focused tests and `GOFLAGS=-p=1 make pre-pr`; investigate any package timeout separately at 600s.
- [x] 2.3 Archive the change and validate resident-tui specs for commit and iris PR delivery without merging.

Validation: the full serialized gate passed with the installed Go tools added to PATH; no package timed out. Repository-wide strict spec validation retains two pre-existing failures in unchanged `desktop-web-workspace` and `rest-api` specs; the new change and updated `resident-tui` base spec validate.
