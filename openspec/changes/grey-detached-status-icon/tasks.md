# Tasks — grey-detached-status-icon

## 1. Tests (from the deltas)

- [ ] 1.1 `widget.RoleStatusIcon`: detached + each state (ready_to_close, accepted, done, failed, idle, live, needs-input) returns the same glyph as non-detached with the dimmed style; detached + Active returns a static glyph identical across frames.
- [ ] 1.2 `hera.rosterStatusText`: detached suffix ` · detached` on top of the state text (and with PR suffix).
- [ ] 1.3 `widget.TaskStatusIcon`: detached input greys the glyph for in_progress/in_review/pending/complete states; spinner static.
- [ ] 1.4 Tasks tab smoke: a detached task row paints the grey glyph.

## 2. Implement

- [ ] 2.1 `RoleStatusIcon`: drop the `Disconnected` case and `IconDisconnected`; resolve glyph normally then force dimmed style (static frame for Active) when `Disconnected`.
- [ ] 2.2 `rosterStatusText`: remove the "disconnected" label, append ` · detached`.
- [ ] 2.3 `TaskStatusInputs.Detached`; feed from the App's `disconnectedIDs` into `TaskListView` (new setter); update jump-switcher/rail task list callers.
- [ ] 2.4 uxlog line for the Tasks-tab detached set change if not already covered.

## 3. Docs

- [ ] 3.1 Update help/README icon legend if it documents `○`.
- [ ] 3.2 Gotcha note in `context/knowledge/gotchas/hera-view.md` (detached = grey normal icon, static spinner, detection unchanged).
- [ ] 3.3 Bump `SW_VERSION` only if static assets change (not expected).

## 4. Ship

- [ ] 4.1 `make pre-pr` clean.
- [ ] 4.2 `openspec archive grey-detached-status-icon --yes`, `openspec validate --all --strict`, commit.
- [ ] 4.3 Open PR via iris_gh_pr_create.
