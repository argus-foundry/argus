## Decisions

- Intent lives in a new `task_pr_links` table, not `task_meta`, because `SetArchived` wipes `task_meta`. Explicit clearing and task deletion remove a link; both prune entry points delete links transactionally with tasks.
- Setting or clearing an existing link deletes the task's `pr` cache so nothing derived from the previous association survives; PUT therefore reports state `unknown` until the poller verifies.
- Race safety is in the store: `SetAutoPRMetaIfChanged` refuses a linked task and `SetManualPRMetaIfChanged` refuses a changed link, each returning ErrPRLinkChanged inside the write transaction, so an in-flight poll cannot overwrite intent or send merge notices for a rejected association.
- Manual polling skips missing and archived tasks and uses shared dormancy and terminal verification rules. Only cache for the linked URL is trusted; a foreign terminal cache receives bounded re-verification.
- A lookup error keeps the association and last verified cache and is logged.

## Deviations

- `source` uses `automatic` (the ticket's term) rather than `detected`.
- A linked task's merge notifications fire from the manual pass once, on a non-terminal to merged transition of cache for that link.
- DELETE on an absent link preserves automatic cache and emits nothing. Clearing an existing link reports the automatic projection from the (now empty) cache: `state:"none"`, `lifecycle:"unknown"`, until the next branch poll.
