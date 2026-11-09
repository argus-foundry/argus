## Why

Web and Mac cannot explain why a worker is waiting: `GET /api/hera` omits planned nodes and blocking edges, and clients have no signal that the plan changed. The TUI already has the graph.

## What Changes

- Extract the tview-free plan projection (node set, state rules, edges, authored test, nested drill target) from `internal/tui/hera/plan.go` into `internal/hera/plan`. The TUI plan pane delegates to it.
- Add `GET /api/hera/orchestrators/{id}/plan` returning the authored plan graph.
- Add the `hera.plan_changed` event, emitted from the shared DB mutators after commit, so MCP tools, the gater and TUI ops all invalidate through one path.

## Impact

- Specs: `rest-api`, `hera-view`.
- Code: `internal/hera/plan`, `internal/db` (mutator wrappers), `internal/api/hera_plan.go`, `internal/tui/hera/plan.go`.

## Non-Goals

- Web and TUI adoption: the web plan graph is ENG-1098 (M3). No new TUI control.
- Mac adoption is a separate native follow-up.
- No mutation over REST; GET never writes.
