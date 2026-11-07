## Why

A worker can report completion while its agent keeps running, but `GET /api/tasks*` only populated runtime flags for `in_progress` tasks, so clients lost the busy indicator. Live session existence and workflow status are independent concepts.

## What Changes

- `live` is true whenever the runner holds an input-addressable session and no kill+resume is pending, for every workflow status.
- `idle` is true only for a live idle session; absent sessions are never idle (previously an `in_progress` task without a session reported `idle:true`).
- A pending restart clears `live`, `idle` and `needs_input`.
- New `session.runtime_changed` event with `{"live","idle","needs_input"}`, published by the idle watcher tick whenever any flag changes, including changes caused only by a workflow transition.

## Non-Goals

- No Web, TUI or Mac UI change. Follow-ups: ENG-1071 (Web) and ENG-1089 (TUI).
- `needs_input` keeps its `in_progress` gate and its detector; it additionally requires a live session.
