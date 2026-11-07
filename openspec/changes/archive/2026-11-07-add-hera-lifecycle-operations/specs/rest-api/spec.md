## ADDED Requirements

### Requirement: Cooperative worker bounce over REST

`POST /api/hera/roles/{id}/bounce` with `{"request_id":"<uuid>"}` SHALL, for a worker or freelance role with a live binding and live session, record an operation and send the bounce handoff instruction as system input exactly once, responding 202 `{operation_id, role_id, task_id, kind:"bounce", state:"awaiting_handoff"}`. It SHALL NOT kill the session, infer handoff text from output, or time out. A malformed UUID, body, unknown field or `force` SHALL return 400; a missing role 404; a coordinator, a role without a live binding or session, a `request_id` reused for a different request, or another active operation on the role 409; failure to persist the receipt 500. The client SHALL NOT be able to supply instruction text or other targets.

#### Scenario: Identical retry
- **WHEN** the same `request_id` is POSTed again for the same role
- **THEN** the stored operation is returned and no second instruction is written

#### Scenario: Coordinator target
- **WHEN** the role is a coordinator
- **THEN** the response is 409 and nothing is written

### Requirement: Forced coordinator recycle over REST

`POST /api/hera/roles/{id}/recycle` with `{"request_id":"<uuid>","force":true}` SHALL, for a coordinator with a live binding and session, record an operation and restart its session immediately on the same task, worktree and branch, seeded from mission, plan state and any handoff note, responding 202 with `kind:"force_recycle"` and `state:"restarting"`. `force` other than true SHALL return 400. Other status rules match bounce. Runtime failure SHALL end the operation as `failed`, never as success.

#### Scenario: Recycle completes
- **WHEN** a live coordinator is force-recycled
- **THEN** the operation reaches `restarted` and the task, worktree and branch are unchanged

### Requirement: Lifecycle operation receipts are readable

`GET /api/hera/roles/{id}/lifecycle-operations/{operation_id}` SHALL return the operation with `"error":null` or a safe error string, 404 for an unknown operation or one belonging to another role, and 400 for a malformed id. States are `awaiting_handoff`, `restarting`, `restarted` and `failed`.

#### Scenario: Failed operation
- **WHEN** the restart fails at runtime
- **THEN** the receipt shows `failed` with a non-null error that carries no internal detail
