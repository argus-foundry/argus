## Decisions

- Clear first, then write the status, so no committed `working` state coexists with a stale mark. If the status write then fails the mark is already clear, which is the safe direction (the worker is mid-recovery, not falsely ready).
- Worker-kind only, scoped to the caller's own binding task, mirroring the done/failed rolls. The marker is per task, so another role's task is never touched.
- The TUI's step path clears for any non-done status; this change mirrors only the `working` case the ticket names.
