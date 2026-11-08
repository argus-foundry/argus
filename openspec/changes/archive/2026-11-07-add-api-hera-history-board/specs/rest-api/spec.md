## ADDED Requirements

### Requirement: Read-only Hera message history

The system SHALL expose `GET /api/tasks/{id}/hera-messages` returning mail addressed to every role the task is or was bound to, paged by message id (`limit` 1-500 default 100, exclusive `before_id`), oldest first within a page, with `next_before_id`. It SHALL NOT mark messages read or alter delivery state.

#### Scenario: History does not consume mail

- **WHEN** a client reads the history
- **THEN** no `read_at`, delivery state or unread count changes

#### Scenario: Invalid cursor

- **WHEN** `limit` or `before_id` is invalid
- **THEN** the response is 400

### Requirement: Human board read-state and dismissal

The system SHALL expose `PUT /api/hera/roles/{role_id}/board/{item_id}/read-state` and `POST .../dismiss`, each requiring `expected_updated_at`, returning `{item, unread_count}`, and failing with coded errors `invalid_request`, `board_item_not_found`, `revision_conflict`, `item_resolved` or `board_write_failed`.

#### Scenario: Stale revision

- **WHEN** the item changed since `expected_updated_at`
- **THEN** the response is 409 `revision_conflict` and the item stays unread and open

#### Scenario: Repeated dismissal

- **WHEN** an identical dismissal is repeated
- **THEN** the resolved item is returned and no second coordinator notice is written

### Requirement: Board change event

The system SHALL emit `hera.board_changed` after each committed board post, material edit, read-state change or resolution, with payload `{role_id, item_id, unread_count, operation}` and no item bodies.

#### Scenario: Event follows commit

- **WHEN** a board mutation commits
- **THEN** one event is emitted with the updated unread count
