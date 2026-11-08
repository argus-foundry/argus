- [x] Verify base and inspect task context API and event consumption.
- [x] Scope approved by em in Hera message 1283 before implementation.
- [x] Add red tests for measured categories, age, fallback and selection/event fencing.
- [x] Implement context projection, rendering and refresh wiring.
- [x] Run workspace unit and browser coverage, review, and full make pre-pr.
- [x] Update context documentation and gotchas, archive in branch, commit locally.

Validation passed with Go 1.26.10 and GOFLAGS=-p=3, with Codex/task environment overrides unset. Full make pre-pr passed, including 604 workspace tests. Expanded browser coverage passed 25 tests, and the final context suite passed all 8 tests after session fence refinements.
