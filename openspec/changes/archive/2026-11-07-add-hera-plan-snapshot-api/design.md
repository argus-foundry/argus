## Decisions

- **Wire identity is `role:<id>`.** The TUI keys nodes by bound task id, which changes at materialization. The wire uses the role id, stable before and after; `task_id` is the latest bound task (live or ended) or null. The TUI keeps its own task-keyed ids.
- **State derivation is shared.** `plan.StateOf` holds the TUI rules (cancelled, planned, `{"failed":true}` result, then task status). The wire state `in_progress` corresponds to the TUI `working` state.
- **Emission lives in DB mutators, outside the lock.** Each public mutator wraps an unexported implementation and emits after it returns, so the events sink can re-enter the DB. This covers MCP `hera_plan*`/block/cancel tools, gater materialization (binding creation) and TUI ops with no per-caller wiring. Failed mutations emit nothing. Task status, archive and delete also emit for the orchestrators the task is bound to, because node state derives from task status.
- **Payload is `{"orchestrator_id":N}`** with no task id; consumers re-fetch the snapshot.
- **Size caps return 413** (2000 nodes, 5000 edges) instead of truncating.
- **Nested teams:** `child_orchestrator_id` comes from `Model.BridgeIndex`, which in this repo is driven by explicit orchestrator links.
- **Deviation:** nuked orchestrators return 404 because `BuildModel` excludes them.
