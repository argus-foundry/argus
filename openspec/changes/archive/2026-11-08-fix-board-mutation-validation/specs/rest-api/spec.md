## MODIFIED Requirements

### Requirement: Human board read-state and dismissal

The system SHALL expose `PUT /api/hera/roles/{role_id}/board/{item_id}/read-state` and `POST .../dismiss`, each requiring `expected_updated_at`, returning `{item, unread_count}`, and failing with coded errors `invalid_request`, `board_item_not_found`, `revision_conflict`, `item_resolved` or `board_write_failed`. Dismissal notes SHALL allow up to 64 KiB of decoded UTF-8 bytes, including JSON-escaped content, while the encoded request body remains bounded. A read-state conditional UPDATE that affects no row SHALL return the current missing, resolved or revision-conflicted error rather than success, and SHALL NOT emit a success event. REST and strict store mutations SHALL reject zero expected revision timestamps. Existing legacy resolution without an expected revision SHALL remain unchanged. Both REST handlers SHALL require exactly one JSON value and consume the bounded body through EOF before any mutation, including trailing whitespace in the encoded allowance.

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

#### Scenario: Independent writer edits during acknowledgment

- **WHEN** another connection commits a revision change between read-state validation and its conditional UPDATE
- **THEN** the response is 409 `revision_conflict`, the edited item stays unread and no read-state success event is emitted

#### Scenario: Independent writer resolves or deletes during acknowledgment

- **WHEN** another connection resolves or deletes the item between read-state validation and its conditional UPDATE
- **THEN** the response is 409 `item_resolved` or 404 `board_item_not_found` respectively, with no successful item/count response or read-state event

#### Scenario: Zero expected revision

- **WHEN** either REST mutation or strict store mutation receives a zero expected revision timestamp
- **THEN** it fails as invalid input, REST returns 400 `invalid_request`, and the item, notices and events remain unchanged

#### Scenario: Trailing invalid input

- **WHEN** a valid board mutation object is followed by garbage or another JSON value
- **THEN** the response is 400 `invalid_request` and no item, notice or event is changed

#### Scenario: Trailing whitespace exceeds allowance

- **WHEN** trailing whitespace takes the entire encoded mutation body above its route's allowance
- **THEN** the response is 400 `invalid_request` before mutation, while whitespace within the allowance remains valid
