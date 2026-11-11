## 1. Regression coverage

- [x] 1.1 Add scoped-reader, render and handler tests for the resident board and its controls.
- [x] 1.2 Add attention, help/palette and SimulationScreen toggle/arrival tests.

## 2. Implementation

- [x] 2.1 Forward resident board reads and enable shared board controls and unread arrival auto-show.
- [x] 2.2 Include unread board items in resident attention without acknowledging them on tab entry.
- [x] 2.3 Expose the board keys in resident help and palette, add board logs and document the reader/attention gotcha.

## 3. Validation and delivery

- [x] 3.1 Run focused tests and review the implementation and test assertions.
- [x] 3.2 Wait for coordinator gate release, then run `GOFLAGS=-p=1 make pre-pr`.
- [x] 3.3 Archive the change, validate specs, commit, push and open a PR through iris without merging.

Validation: the full serialized gate passed (89.3% filtered coverage; 741 workspace tests). The change and resident-tui spec validate strictly. Global strict validation retains two pre-existing failures in unchanged `desktop-web-workspace` (missing SHALL/MUST) and `rest-api` (missing scenario) specs.
