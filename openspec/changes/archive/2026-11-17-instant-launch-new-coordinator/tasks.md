## 1. Model and page

- [x] 1.1 Add `Pending` to `heramodel.OrchView` and `RoleView`; synthetic IDs use a large positive base so `Rail.currentRef` (role id vs negated orch id) never collides with real IDs.
- [x] 1.2 `HeraPage` pending-coordinator registry + overlay applied in `applyResult` (UI goroutine); add/resolve API (select + focus coord pane, completion-only focus when still selected).
- [x] 1.3 `applySelection` drives `coordPane.SetPending` from the selected pending orchestrator.
- [x] 1.4 Rail draws pending rows as launching; mutation keys on a pending row are inert with a status message.

## 2. App wiring

- [x] 2.1 `heraDoNewCoordinator` adds the pending row before spawning; completion/failure resolve it; uxlog lines throughout.

## 3. Tests and docs

- [x] 3.1 SimulationScreen smoke tests: pending row selected + LAUNCH banner on submit, focus on coord pane at completion, navigate-away suppresses focus, failure removes the row.
- [x] 3.2 Gotcha in `context/knowledge/gotchas/hera-view.md` + index row.
- [x] 3.3 `make pre-pr`; archive the change in the PR.
