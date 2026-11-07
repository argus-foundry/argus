## ADDED Requirements

### Requirement: Multiplexed terminal output stream

The REST API SHALL let an authenticated client open one SSE connection carrying terminal output for up to 64 tasks. `POST /api/terminal-streams` with `{"subscriptions":[{"task_id","session_id","since"}]}` SHALL return 201 `{"stream_id","url"}`. `GET` on the url SHALL emit `output` (`task_id`, `session_id`, `from` inclusive, `to` exclusive, `data_base64`), `gap` (`requested`, `oldest`, `current`), `reset`, `exit` and `subscription_error` (`code`) events. `PUT /api/terminal-streams/{id}` SHALL atomically replace the subscriptions and return 200 with the stream id; `DELETE` SHALL return 204. Invalid or duplicate subscriptions SHALL return 400, more than 64 SHALL return 413, an unknown or expired stream SHALL return 404 and a stream owned by another principal SHALL return 403. An unused stream SHALL expire after 60 seconds without a connection. Closing, disconnecting, deleting or expiring a stream SHALL detach every session writer it registered.

#### Scenario: Several tasks over one connection

- **WHEN** a client subscribes to two tasks with live sessions
- **THEN** output from both arrives as `output` events on the single connection with contiguous cursors

#### Scenario: Evicted history produces a gap

- **WHEN** a subscription's `since` is older than the session ring's oldest retained byte
- **THEN** a `gap` event reports requested, oldest and current offsets before the retained bytes

#### Scenario: Session replacement produces a reset

- **WHEN** the task's session changes identity
- **THEN** a `reset` event with the new session id precedes any bytes of the new session

#### Scenario: Per-subscription failure isolation

- **WHEN** one subscribed session exits or a task has no session
- **THEN** only that subscription receives `exit` or `subscription_error` and the others continue

#### Scenario: Slow client cannot stall a session

- **WHEN** a client stops reading while a session keeps producing output
- **THEN** the queue is bounded, a `gap` is emitted rather than silent loss, and the session reader is never blocked

#### Scenario: Writers released

- **WHEN** the stream is deleted, disconnected, expired or its subscriptions replaced
- **THEN** every writer registered on the affected sessions is removed
