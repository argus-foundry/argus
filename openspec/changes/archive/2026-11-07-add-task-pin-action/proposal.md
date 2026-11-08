## Why

Pinning a task over REST requires a full raw task read and write, which can overwrite concurrent edits and bypasses the pin/archive mutual-exclusion rule.

## What changes

- `POST /api/tasks/{id}/pin` with `{"pinned":true|false}` returns 200 `{"task_id","pinned","archived"}`.
- Uses the targeted `db.SetPinned` write (only the pinned/archived columns), so unrelated fields (name, status, model, session) are never round-tripped. Pinning an archived task restores it; unpinning leaves archived alone.
- Emits `task.updated` (`changed_fields` `["pinned"]` or `["archived","pinned"]`) only when a flag moved.
- 400 for malformed JSON, missing/null/non-boolean `pinned`, unknown fields or a body over 1 KiB; 404 for a missing task; 500 for store failure. Any authenticated token, same tier as the other task actions.
- Does not start or stop a session.

## Capabilities

### Modified capabilities

- `rest-api`: adds the pin action.

## Non-Goals

- Web consumption. Follow-up: ENG-1084 (Web: Pin tasks without overwriting other edits). Mac and TUI need no change (the TUI already uses `SetPinned`).
- Recovering metadata/messages removed by an earlier archive: restore does not imply it.

## Impact

- Code: `internal/api/handlers.go`, `internal/api/routes.go`.
- Depends on the `task.updated` helper from `add-task-updated-event`.
