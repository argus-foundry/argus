## MODIFIED Requirements

### Requirement: Human board read-state and dismissal

The system SHALL expose `PUT /api/hera/roles/{role_id}/board/{item_id}/read-state` and `POST .../dismiss`, each requiring `expected_updated_at`, returning `{item, unread_count}`, and failing with coded errors `invalid_request`, `board_item_not_found`, `revision_conflict`, `item_resolved` or `board_write_failed`. Dismissal notes SHALL allow up to 64 KiB of decoded UTF-8 bytes, including JSON-escaped content, while the encoded request body remains bounded.

#### Scenario: Stale revision

- **WHEN** the item changed since `expected_updated_at`
- **THEN** the response is 409 `revision_conflict` and the item stays unread and open

#### Scenario: Repeated dismissal

- **WHEN** an identical dismissal is repeated
- **THEN** the resolved item is returned and no second coordinator notice is written

#### Scenario: Escaped note at the limit

- **WHEN** a dismissal contains a valid note of at most 64 KiB decoded bytes, even when JSON escaping expands its encoded size
- **THEN** the dismissal succeeds and preserves the complete note

#### Scenario: Oversized dismissal

- **WHEN** the decoded note exceeds 64 KiB or the encoded request exceeds its bounded transport allowance
- **THEN** the response is 400 and the item remains unchanged

### Requirement: Board change event

The system SHALL emit `hera.board_changed` after each committed board post, material edit, read-state change or resolution in the daemon process, with payload `{role_id, item_id, unread_count, operation}` and no item bodies. Forwarding TUI-originated events across the process boundary to daemon SSE is deferred to ENG-1125; those writes remain durable and clients can refetch the roster for `board_unread`.

#### Scenario: Event follows commit

- **WHEN** a board mutation commits
- **THEN** one event is emitted with the updated unread count

#### Scenario: Reopening an acknowledged item

- **WHEN** a legacy reader acknowledges the same open board item more than once without an intervening unread transition
- **THEN** only the first unread to read transition emits an event

## ADDED Requirements

### Requirement: Recipient-indexed Hera history

History paging SHALL use a full recipient/message-id index covering both read and unread messages, with separate first-page and cursor queries so a cursor supplies a direct exclusive id range. Historical and current bindings SHALL retain the same message selection and ordering.

#### Scenario: History with unrelated read mail

- **WHEN** a task's ended and active roles have matching mail amid substantial unrelated read history
- **THEN** each page seeks the matching recipients, cursor pages also seek `id < before_id`, and returned IDs remain oldest first within the selected newest page
