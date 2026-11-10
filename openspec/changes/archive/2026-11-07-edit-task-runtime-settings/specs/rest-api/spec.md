## ADDED Requirements

### Requirement: Task runtime settings are editable with an explicit apply policy

`GET /api/tasks/{id}/runtime` SHALL return `task_id`, `runtime` (`backend`, `model`, `sandbox_override`, `prompt`), `live`, `busy`, `available_apply` and `revision`. `PATCH /api/tasks/{id}/runtime` SHALL accept optional `model`, `backend`, `sandbox_override`, `prompt`, required `apply` of `next_resume` or `restart`, optional `force` and `force_revive`, and a required `If-Match` revision. Responses: 400 for bad JSON, trailing data, a body over 256 KiB, a NUL-containing prompt, unknown backend, invalid override, invalid model, blank prompt, bad or missing `apply`, or no field; 403 for `sandbox_override` without the master token; 404 unknown task; 428 missing `If-Match`; 412 stale revision; 409 for `next_resume` on a live session, a busy restart without `force`, a pending restart, a backend family change with a saved session, an unresumable saved session, or an unforced Hera close-out. Every rejection SHALL leave all fields and the session untouched. On success the response SHALL carry `apply`, `changed_fields`, the new `revision`, the actual `session_id`, `live` and `started`. `restart` SHALL be a controlled stop, persist and resume, never an in-process switch. The If-Match check and runtime update SHALL compare the editable fields and session identity atomically. Conversation discovery SHALL NOT persist before admission; an admitted edit MAY commit the discovered session identity with the runtime columns. The bounded body SHALL be decoded before acquiring the cancellable task lock. If the restart fails after persisting, the response SHALL be 500 `{"error":"restart failed","code":"restart_failed","settings_applied","started":false,"live","rolled_back","revision"}` stating what actually happened; A kick error, failed outcome probe, or a restart still pending at the response deadline SHALL return 500 with code `restart_unknown`, `settings_applied:true`, `rolled_back:false` and `started:false`, retaining the candidate settings because the replacement may still start. A completed kick with no live session SHALL return `restart_failed` with `started:false`. An ambiguous session start SHALL preserve the cleared Hera close-out state because the session may still start. The resident row SHALL be hidden from GET with 404 and rejected by PATCH with 409 through the existing resident gate and error shape, without runtime writes or session changes.

#### Scenario: Next-resume edit

- **WHEN** a stopped task's model is patched with `apply=next_resume`
- **THEN** only the runtime columns change and `task.runtime_changed` carries `started:false` and no prompt text

#### Scenario: Stale revision

- **WHEN** `If-Match` does not equal the current revision
- **THEN** the response is 412 and nothing changes

#### Scenario: Restart fails after persist

- **WHEN** a restart cannot start the session after settings were stored
- **THEN** the response is 500 with `settings_applied:true`, `started:false`

### Requirement: task.runtime_changed event

The daemon SHALL emit `task.runtime_changed` with payload `{"apply","changed_fields","started"}` once a runtime PATCH that persisted settles, and SHALL NOT emit it for rejections or a rolled-back restart. `task.updated` SHALL list `runtime` when `sandbox_override` changes.

Client runtime controls remain named follow-ups for web (ENG-1076), TUI (ENG-1091), and macOS runtime controls: named follow-up, ticket to be filed.
