## Decisions

- **Emit from the db layer, not handlers.** Handlers differ (REST, MCP, raw PUT, TUI); the db setters are the one shared choke point and already host the specific events, so a handler-level emit would miss paths or duplicate.
- **Diff, don't assume.** `Update` snapshots the old row inside its lock and emits `model.ChangedTaskFields(old, new)`; targeted setters read the prior value in the same lock. A write that changes nothing is silent.
- **`runtime`** groups backend, model and session id. `agent_pid` is deliberately excluded (it churns on every start and is already announced by session events).
- **Archive/restore REST handlers keep using `db.Update`.** Switching to `SetArchived` would also wipe `task_meta`, a behavior change outside this ticket; `Update` already emits `task.archived` and now `task.updated`.
- **Deviation from the ticket:** a semantic change can yield a specific event plus one `task.updated`; clients must tolerate both, as the ticket states. `pr` fires only when `SetMetaBatchIfChanged` actually writes the `pr` namespace.
- **Atomicity boundary:** the store commit and the event are not atomic. A crash between them loses the event; the replay ring and client reconciliation are the recovery path.
