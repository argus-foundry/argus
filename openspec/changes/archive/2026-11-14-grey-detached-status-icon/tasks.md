# Tasks — grey-detached-status-icon

## 1. Tests (from the deltas)

- [x] 1.1 `widget.RoleStatusIcon`: detached + each state (ready_to_close, accepted, done, failed, idle, live, needs-input) returns the same glyph as non-detached with the dimmed style; detached + Active returns a static glyph identical across frames.
- [x] 1.2 `hera.rosterStatusText`: detached suffix ` · detached` on top of the state text (and with PR suffix).
- [x] 1.3 `widget.TaskStatusIcon`: detached input greys the glyph for in_progress/in_review/pending/complete states; spinner static.
- [x] 1.4 Tasks tab smoke: a detached task row paints the grey glyph.

## 2. Implement

- [x] 2.1 `RoleStatusIcon`: drop the `Disconnected` case and `IconDisconnected`; resolve glyph normally then force dimmed style (static frame for Active) when `Disconnected`.
- [x] 2.2 `rosterStatusText`: remove the "disconnected" label, append ` · detached`.
- [x] 2.3 `TaskStatusInputs.Detached`; feed from the App's `disconnectedIDs` into `TaskListView` (new setter); update jump-switcher/rail task list callers.
- [x] 2.4 uxlog line for the Tasks-tab detached set change if not already covered.

## 3. Docs

- [x] 3.1 Update help/README icon legend if it documents `○`.
- [x] 3.2 Gotcha note in `context/knowledge/gotchas/hera-view.md` (detached = grey normal icon, static spinner, detection unchanged).
- [x] 3.3 (n/a, no static change) Bump `SW_VERSION` only if static assets change (not expected).

## 4. Ship

- [x] 4.1 `make pre-pr` clean.
- [x] 4.2 `openspec archive grey-detached-status-icon --yes`, `openspec validate --all --strict`, commit.
- [x] 4.3 Open PR via iris_gh_pr_create.
