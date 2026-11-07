# Design

- Predicate reuse: `TaskHoldsLiveHeraWorkerOrFreelanceBinding` + `HeraWorkerAwaitingCloseout`, the same pair the TUI uses.
- `SnapshotAndClearHeraCloseout` atomically snapshots and clears before launch. `RestoreHeraCloseout` restores only the unchanged cleared state on a definite launch failure (`start_failed`), never on an ambiguous outcome (`start_ambiguous`).
- Deviation: the ticket asks for a `task.status_changed` payload of `{"status":"in_progress"}`; the existing `db.Update` emission with the standard `{from,to}` payload is kept so consumers see one shape. `session.started` is already emitted only by an actual Runner start, so reattach emits none.
- The guard also applies when the task is in_progress with a live idle session (ticket-literal), after the busy and pending-restart checks.
