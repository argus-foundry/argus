## ADDED Requirements

### Requirement: Archived tasks reject REST input

The system SHALL load the task before writing any byte for `POST /api/tasks/{id}/input`. An unknown task SHALL return 404 `{"error":"task not found","code":"task_not_found"}`; an archived task SHALL return 409 `{"error":"task is archived","code":"task_archived"}` for both user and system `X-Input-Origin`, with zero bytes reaching the session and no change to needs-input state or input timestamps; a non-archived task with no session SHALL return 404 with code `no_active_session`; allowed input SHALL keep the raw body, 64 KiB limit and `{"status":"ok","bytes":"N"}` response. Input SHALL read the body before acquiring the per-task lock and SHALL check archive state while holding it. Archive, unarchive and input SHALL be serialized per task so that once an archive success is committed no later input reaches the session. A rejection SHALL emit no event and no log line SHALL contain input bytes.

#### Scenario: Archived live session

- **WHEN** a task with a live session is archived and input is posted with either origin
- **THEN** the response is 409 `task_archived` and the session's byte count and input timestamps are unchanged

#### Scenario: Concurrent archive and input

- **WHEN** inputs race an archive request
- **THEN** no input that begins after the archive response returns succeeds and no bytes are written after it

#### Scenario: Distinct failures

- **WHEN** the task is unknown, or live without a session, or live with a session
- **THEN** the responses are 404 `task_not_found`, 404 `no_active_session`, and 200 respectively
