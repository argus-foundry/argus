## Decisions

- Timestamp lives in a `result_at` column rather than `task_meta`, because `SetArchived` wipes `task_meta` and an archived failed task must keep its time.
- The failure projection is computed on read from Result; only the timestamp is stored. Legacy rows with no timestamp omit `recorded_at` instead of fabricating one.
- `task.failure_changed` is emitted from the `internal/db` setters (`SetResult`, `Update`) next to `task.updated`, so MCP, REST and TUI writers all reach it. It fires only when the old or new Result is a failure.
- `reason` is truncated to 4 KiB on a UTF-8 boundary. A malformed or non-object Result yields `failure: null`.
