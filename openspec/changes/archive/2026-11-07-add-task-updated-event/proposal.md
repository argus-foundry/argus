## Why

A change made in one REST client stays stale in another until its reconciliation poll. Archive/restore, pin, runtime, account, result and PR association have no event at all, and the specific events (`task.renamed`, `task.status_changed`, `task.archived`) cover only part of the mutation surface. The web workspace compensates with a 30 s poll.

## What changes

- New event `task.updated` with payload `{"changed_fields":[...]}`, an invalidation: identity plus aggregate metadata only, never prompt/result/credential content.
- One shared helper, `events.EmitTaskUpdated(taskID, fields...)`, called from the `internal/db` setters (`Update`, `SetArchived`, `SetPinned`, `Rename`, `RenameIfName`, `SetStatus`, `SetResult`, `SetTaskAccount`, and the PR poller's `SetMetaBatchIfChanged` on the `pr` namespace). REST, MCP, TUI and daemon paths all reach these setters, so every path emits through the same helper.
- Emission happens after commit and outside the DB lock, only for fields that actually changed. Specific events are unchanged and still fire.
- Field vocabulary: `name`, `status`, `archived`, `pinned`, `runtime` (backend/model/session id), `account`, `result`, `pr`.

## Capabilities

### Modified capabilities

- `events-bus`: adds the `task.updated` contract and the shared emission helper.

## Non-Goals

- Web/TUI consumption. Follow-up: ENG-1085 (Web: Update the rail from task events instead of polling). Mac and TUI need no change.
- Atomic store+event persistence. Emission is best-effort after commit; a sink failure is logged by the sink and never rolls the mutation back, so reconciliation stays necessary.
- Events for hard delete (`task.deleted` is unchanged).

## Impact

- Code: `internal/model/event.go`, `internal/events/taskupdated.go`, `internal/db/tasks.go`, `internal/db/task_meta.go`.
- Frontend parity: REST/SSE surface only; web consumption is the named follow-up above.
