## ADDED Requirements

### Requirement: Targeted task pin action

The system SHALL expose `POST /api/tasks/{id}/pin` accepting `{"pinned":<bool>}` and returning 200 `{"task_id","pinned","archived"}` reflecting the stored flags. It SHALL write only the pinned and archived columns using SetPinned semantics (pinning clears archived; unpinning leaves archived alone) and SHALL NOT overwrite any other task field or start or stop a session. A request that changes nothing SHALL succeed without emitting `task.updated`. The endpoint SHALL return 400 for malformed JSON, a missing, null or non-boolean `pinned`, unknown fields, trailing non-whitespace, multiple JSON values, or a body over 1 KiB (including trailing whitespace); 404 for an unknown task; 500 for a store failure; and SHALL be open to any authenticated token.

#### Scenario: Pin preserves concurrent edits

- **WHEN** a task is renamed and its status changed, then pinned via the endpoint
- **THEN** the stored name and status are the newer values and only pinned changed

#### Scenario: Pin restores an archived task

- **WHEN** an archived task is pinned
- **THEN** the response has `pinned:true, archived:false` and the stored row matches

#### Scenario: Invalid body

- **WHEN** the body is `{"pinned":"yes"}` or `{}` or contains an unknown field
- **THEN** the response is 400 and nothing is stored or emitted

#### Scenario: Complete pin request validation

- **WHEN** a valid pin object is followed by garbage, another JSON value, or whitespace that takes the complete body above 1 KiB
- **THEN** the response is 400 and no task field or event is changed

#### Scenario: Pin body at the limit

- **WHEN** exactly one valid pin object and optional whitespace occupy at most 1 KiB through EOF
- **THEN** the request succeeds with the stored flags
