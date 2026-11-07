## Decisions

- Intent lives in a new `task_pr_links` table, not `task_meta`, because `SetArchived` wipes `task_meta`. Only task deletion removes a link.
- Setting or clearing a link deletes the task's `pr` cache so nothing derived from the previous association survives; PUT therefore reports state `unknown` until the poller verifies.
- Race safety is in the store: `SetAutoPRMetaIfChanged` refuses a linked task and `SetManualPRMetaIfChanged` refuses a changed link, each inside the write transaction, so an in-flight poll cannot overwrite intent.
- A lookup error keeps the association and last verified cache and is logged.

## Deviations

- `source` uses `automatic` (the ticket's term) rather than `detected`.
- A linked task's merge notifications fire from the manual pass once, when verification first stores a changed value.
- DELETE reports the automatic projection from the (now empty) cache: `state:"none"`, `lifecycle:"unknown"`, until the next branch poll.
