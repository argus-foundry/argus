## ADDED Requirements

### Requirement: Board decision launch link

The REST API SHALL expose `POST /api/hera/roles/{role_id}/board/decision-link`, requiring a master or device token (a decision session SHALL NOT mint links), that returns `{"path":"/decide?role=<id>[&focus=<item_id>]","ticket":"<opaque>","expires_at":"<RFC3339>"}`. The optional JSON body `{"focus_item_id":N}` selects the focused card. The ticket SHALL be 256 bits of randomness, held only in daemon memory as a hash, bound to that role id, valid for 90 seconds, and usable once. The endpoint SHALL return 404 `board_item_not_found` for an unknown role and 400 `invalid_request` for a non-coordinator role. The ticket SHALL NOT be logged or persisted.

#### Scenario: Mint a link

- **WHEN** an authenticated client posts to the decision-link endpoint for a coordinator role
- **THEN** it receives a relative path, a ticket and an expiry roughly 90 seconds ahead

#### Scenario: Non-coordinator role

- **WHEN** the role is a worker or freelance role
- **THEN** the response is 400 `invalid_request` and no ticket exists

#### Scenario: Decision session cannot mint

- **WHEN** a request authenticated by a decision session calls the decision-link endpoint
- **THEN** the response is 403

### Requirement: Decision session exchange and auth class

The REST API SHALL expose `POST /api/hera/board/decision-session` with body `{"ticket":"…"}`, served without bearer authentication because the ticket is the credential, returning `{"session":"<opaque>","role_id":N,"expires_at":"<RFC3339>"}`. An unknown, expired or already-used ticket SHALL return 401 and consume nothing. The session SHALL be 256 bits of randomness, held in daemon memory as a hash, bound to the ticket's role id, and valid for eight hours. A session presented as a bearer token SHALL authenticate the request as `X-Argus-Auth: decision:<role_id>` and SHALL be permitted only on `GET /api/hera/roles/{role_id}/board/decisions` and `POST /api/hera/roles/{role_id}/board/answers` with its own role id. Every other route, and those routes with a different role id, SHALL return 403. No existing handler that gates on `master`, `device` or `scope:*` SHALL admit a decision session. Daemon restart SHALL invalidate all tickets and sessions.

#### Scenario: Ticket is single-use

- **WHEN** a ticket is exchanged once and then exchanged again
- **THEN** the first returns a session and the second returns 401

#### Scenario: Expired ticket

- **WHEN** a ticket is exchanged more than 90 seconds after it was minted
- **THEN** the response is 401

#### Scenario: Session is role-scoped

- **WHEN** a session for role 12 requests the decisions or answers route for role 13, or any other API route
- **THEN** the response is 403

#### Scenario: Session cannot reach privileged handlers

- **WHEN** a decision session calls an endpoint that accepts a scope token, such as MCP tool registration
- **THEN** the response is 403 and nothing is registered

#### Scenario: Restart invalidates

- **WHEN** the daemon restarts after a session was issued
- **THEN** that session is rejected with 401

### Requirement: Board decisions read endpoint

The REST API SHALL expose `GET /api/hera/roles/{role_id}/board/decisions`, accepting a master or device token for any coordinator role or a decision session for its own role, returning `{"role_id":N,"coordinator":"<name>","items":[…]}` where `items` are the role's open `decision` items in ascending id order. Each item SHALL carry `id`, `subject`, `body`, `updated_at`, `structured` (null for a plain item) and `read`. The endpoint SHALL NOT mark items read and SHALL NOT include `fyi` or resolved items.

#### Scenario: Round contents

- **WHEN** a board has two structured decisions, one plain decision, one fyi and one resolved decision
- **THEN** the response lists the three open decisions in id order and omits the fyi and the resolved item

#### Scenario: Read state untouched

- **WHEN** the endpoint is called
- **THEN** no item's read state or `updated_at` changes

### Requirement: Board answers endpoint

The REST API SHALL expose `POST /api/hera/roles/{role_id}/board/answers`, accepting a master or device token for any coordinator role or a decision session for its own role, with body `{"answers":[{"item_id":N,"expected_updated_at":"<RFC3339>","choice":"<option id>|other","note":"<text>"}]}`. The body SHALL contain exactly one JSON value, at most 16 answers, notes of at most 4 KiB, a nonzero `expected_updated_at` per answer, and SHALL be consumed through EOF before any mutation. The endpoint SHALL return 200 with `{"results":[{"item_id":N,"outcome":"<outcome>","item":{…}}],"unread_count":N}` where each outcome is one of `resolved`, `conflict`, `already_resolved`, `not_found` or `invalid`, and `item` is the current item for `conflict` outcomes. A malformed body SHALL return 400 `invalid_request`. All `resolved` items and the single summary message described in `board-decision-page` SHALL commit in one transaction; non-`resolved` outcomes SHALL change nothing and SHALL NOT prevent other answers in the same request from resolving. A `hera.board_changed` event SHALL be emitted once per resolved item after commit.

#### Scenario: Mixed outcomes

- **WHEN** a request answers one current item, one item the coordinator edited after load, and one already-resolved item
- **THEN** the response reports `resolved`, `conflict` (with the current item) and `already_resolved`, only the first item changes, and one summary message exists

#### Scenario: Nothing resolved

- **WHEN** every answer returns a non-`resolved` outcome
- **THEN** no item changes and no message is written

#### Scenario: Trailing garbage

- **WHEN** a valid body is followed by another JSON value
- **THEN** the response is 400 `invalid_request` and nothing changes

#### Scenario: Oversized round

- **WHEN** a request carries 17 answers or a note over 4 KiB
- **THEN** the response is 400 `invalid_request` and nothing changes

### Requirement: Board item JSON carries structured data and resolution choice

Board item JSON returned by `GET /api/hera/roles/{id}/board` and the new decision endpoints SHALL include `structured` (an object with `options`, `recommended` and `reason`, or null for a plain item) and `resolution_choice` (the chosen option id, `other`, or empty). These fields SHALL be additive; existing fields and their semantics SHALL be unchanged.

#### Scenario: Resolved structured item

- **WHEN** a structured item was answered with option `auto-retry`
- **THEN** the board JSON for that item carries `resolution_choice: "auto-retry"`, `resolved: true` and `resolved_by: "human"`

#### Scenario: Legacy item

- **WHEN** an item was posted before this change
- **THEN** its JSON carries `structured: null` and `resolution_choice: ""`
