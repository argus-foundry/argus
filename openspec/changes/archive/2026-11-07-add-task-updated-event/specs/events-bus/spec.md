## ADDED Requirements

### Requirement: task.updated invalidation event

The system SHALL publish a `task.updated` event after a task mutation commits, through a single shared helper that REST, MCP, TUI and daemon mutation paths all reach via the `internal/db` setters. The payload SHALL be `{"changed_fields":[...]}` listing only the fields that actually changed, drawn from `name`, `status`, `archived`, `pinned`, `runtime`, `account`, `result`, `pr`, in that canonical order, deduplicated. The payload SHALL carry no prompt, result or credential content. The event SHALL be emitted outside DB and runner locks and SHALL NOT be emitted for a failed write or a write that changed nothing. Existing specific events (`task.renamed`, `task.status_changed`, `task.archived`, `task.completed`, `task.deleted`) SHALL keep their payloads and still fire; a semantic mutation MAY therefore produce a specific event plus one `task.updated`. Event persistence failure SHALL NOT roll back or retry the mutation.

#### Scenario: Archive and restore invalidate other clients

- **WHEN** a client archives then restores a task over REST
- **THEN** a second SSE client receives `task.updated` with `changed_fields` `["archived"]` for each

#### Scenario: Pin restoring an archived task

- **WHEN** an archived task is pinned
- **THEN** one `task.updated` fires with `changed_fields` `["archived","pinned"]`

#### Scenario: No-op write is silent

- **WHEN** a setter or raw PUT writes values identical to the stored row
- **THEN** no `task.updated` is emitted

#### Scenario: Failed write publishes nothing

- **WHEN** a mutation fails because the task does not exist
- **THEN** no `task.updated` event is emitted
