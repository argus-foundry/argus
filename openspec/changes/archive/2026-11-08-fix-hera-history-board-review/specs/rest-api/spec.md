## MODIFIED Requirements

### Requirement: Read-only Hera message history

The system SHALL expose `GET /api/tasks/{id}/hera-messages` returning mail addressed to every role the task is or was bound to, paged by message id (`limit` 1-500 default 100, exclusive `before_id`), oldest first within a page, with `next_before_id`. It SHALL NOT mark messages read, alter delivery state or advance role tree cursors. Store failures SHALL return HTTP 500 with a generic error message; detailed diagnostics SHALL be logged on the server.

#### Scenario: History does not consume mail

- **WHEN** a client reads the history
- **THEN** no `read_at`, delivery state or unread count changes

#### Scenario: Invalid cursor

- **WHEN** `limit` or `before_id` is invalid
- **THEN** the response is 400

#### Scenario: History store failure

- **WHEN** task lookup or history retrieval fails because the store is unavailable
- **THEN** the response is 500 with `internal server error` and the store diagnostic appears only in server logs

### Requirement: Board change event

The system SHALL emit `hera.board_changed` after each committed board post, material edit, read-state change or resolution, with payload `{role_id, item_id, unread_count, operation}` and no item bodies.

#### Scenario: Event follows commit

- **WHEN** a board mutation commits
- **THEN** one event is emitted with the updated unread count

#### Scenario: Reopening an acknowledged item

- **WHEN** a legacy reader acknowledges the same open board item more than once without an intervening unread transition
- **THEN** only the first unread to read transition emits an event
