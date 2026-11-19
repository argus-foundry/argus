## MODIFIED Requirements

### Requirement: Decision session exchange and auth class

The REST API SHALL expose `POST /api/hera/board/decision-session` with body `{"ticket":"…"}`, served without bearer authentication because the ticket is the credential, returning `{"session":"<opaque>","role_ids":[N,…],"expires_at":"<RFC3339>"}` and `role_id` for a single-role TUI ticket. An unknown, expired or already-used ticket SHALL return 401 and consume nothing. The session SHALL have 256 bits of randomness, be held in daemon memory only as a hash, be bound to the ticket's immutable role-ID set and expire absolutely eight hours after exchange. An empty role set SHALL produce an empty page with no board permissions. Session auth SHALL remain a distinct decision auth class; middleware SHALL NOT trust a client-supplied internal auth tag. It SHALL permit only `GET .../board/decisions`, `POST .../board/answers`, `GET .../board/decide-items` and `POST .../board/fyi-actions` under `/api/hera/roles/{role_id}` for roles in that set. Each request SHALL recheck the role is an existing coordinator whose role and orchestrator are neither archived nor nuked. Any other route or role SHALL return 403; no handler gating on master, device or scope tokens SHALL admit a decision session. Neither polling nor new coordinator creation SHALL widen its role set. Daemon restart SHALL invalidate all tickets and sessions.

#### Scenario: Ticket is single-use

- **WHEN** a ticket is exchanged twice
- **THEN** the first returns a session and the second returns 401

#### Scenario: Expired ticket

- **WHEN** a ticket is exchanged after its 90-second lifetime
- **THEN** the response is 401

#### Scenario: Session is role-scoped

- **WHEN** a TUI session for role 12 accesses role 13 or a phone session accesses a role outside its snapshot
- **THEN** the response is 403

#### Scenario: New or removed coordinator

- **WHEN** a coordinator is created after minting or an included coordinator is archived or nuked
- **THEN** the existing session gains no new role and cannot access the removed role

#### Scenario: Session cannot reach privileged handlers

- **WHEN** a decision session requests launch minting, aggregate counts, general board mutation or MCP tool registration
- **THEN** the response is 403 and nothing changes

#### Scenario: Restart invalidates

- **WHEN** the daemon restarts
- **THEN** prior sessions return 401 while durable action receipts remain available to a newly authorized session

## ADDED Requirements

### Requirement: Authenticated aggregate decision launch

`POST /api/hera/board/decision-link` SHALL require a master or device token, reject other authenticated token classes with 403 and unauthenticated requests with 401, and return `{"path":"/decide","ticket":"<opaque>","expires_at":"<RFC3339>"}`. It SHALL accept an empty body or one empty JSON object and reject malformed/trailing JSON with 400 before minting. The server SHALL choose an explicit snapshot of all existing coordinator role IDs whose role and orchestrator are neither archived nor nuked, including those with no open items or no live binding. Clients SHALL NOT supply their own session scope. The ticket SHALL have 256 bits of randomness, be held only as a hash in daemon memory, expire in 90 seconds and exchange once through the existing session endpoint. The path SHALL be same-origin relative and contain no credentials. The existing role-specific launch endpoint SHALL retain its single-role ticket behavior.

#### Scenario: Phone launch without the TUI

- **WHEN** a device-authenticated PWA requests an aggregate launch
- **THEN** it receives a ticket exchangeable for the server-selected role set without any TUI action

#### Scenario: No eligible coordinators

- **WHEN** the eligible role set is empty
- **THEN** launch succeeds with an empty-scope ticket and the page shows zero open items

#### Scenario: Scoped credential rejected

- **WHEN** a plugin scope token or decision session tries to mint an aggregate ticket
- **THEN** it receives 403 and no ticket exists

### Requirement: Aggregate open board counts

`GET /api/hera/board/decision-counts` SHALL require master/device authentication and return `{"decisions":N,"fyis":N,"total":N,"roles":[{"role_id":N,"orchestrator":"…","coordinator":"…","decisions":N,"fyis":N}]}` for the same eligibility rule as aggregate launch, ordered by role ID. Counts SHALL include open items regardless of read state, exclude resolved items and leave read state and timestamps unchanged. Totals and per-role counts SHALL come from one consistent read snapshot. Decision sessions SHALL NOT call this discovery endpoint.

#### Scenario: Count both kinds

- **WHEN** eligible boards contain two unread decisions, a read FYI and a resolved FYI
- **THEN** counts are decisions 2, FYIs 1 and total 3 without mutating board state

### Requirement: Mixed decision page item reads

`GET /api/hera/roles/{role_id}/board/decide-items` SHALL accept master/device tokens for an eligible coordinator or a decision session containing that eligible role ID. It SHALL return `{"role_id":N,"orchestrator":"…","coordinator":"…","decisions":N,"fyis":N,"items":[…]}` from a consistent snapshot, with all open decisions and FYIs in ascending item-ID order. Each item SHALL include existing decision fields plus `kind`; FYIs SHALL have null structured data. The endpoint SHALL NOT mark items read or change timestamps. Existing `/board/decisions` SHALL retain its decision-only filter and response shape; a multi-role session SHALL access it only for included eligible roles.

#### Scenario: Mixed read

- **WHEN** an eligible board has a decision, an FYI and a resolved item
- **THEN** decide-items returns the two open items with kind and counts while decisions returns only the open decision

#### Scenario: Wrong role

- **WHEN** a session requests decide-items for a role outside its immutable set
- **THEN** it receives 403 and no board data

### Requirement: Retry-safe FYI actions endpoint

`POST /api/hera/roles/{role_id}/board/fyi-actions` SHALL accept master/device tokens or a decision session containing that eligible role. The body SHALL be `{"actions":[{"action_id":"<opaque client ID>","item_id":N,"expected_updated_at":"<RFC3339>","action":"comment|dismiss|comment_and_dismiss","note":"…"}]}` with one to sixteen actions. Each action ID SHALL be 1–128 ASCII letters, digits, hyphens or underscores, unique within the request; item IDs SHALL also be unique within the request. Timestamps SHALL be nonzero, notes SHALL be at most 4 KiB UTF-8 and comment actions SHALL require non-whitespace notes; dismiss SHALL require an empty note. Invalid envelope, duplicate IDs/items, oversized notes or trailing JSON SHALL return 400 `invalid_request` before any mutation. An unsupported action or empty required comment SHALL yield a per-action invalid result. The whole body SHALL be consumed through EOF before mutation.

The response SHALL be `{"results":[{"action_id":"…","item_id":N,"outcome":"commented|resolved|already_resolved|conflict|not_found|invalid","item":{…}}],"unread_count":N}`; conflict SHALL include current item content. Missing/wrong-role/non-FYI items SHALL return not_found without leaking content; resident approvals SHALL be invalid. Current revision mismatch SHALL return conflict. Already-resolved FYIs SHALL return already_resolved for dismiss/comment_and_dismiss without storing a new comment or notice; comment-only SHALL return invalid because feedback was not delivered. Successful receipt replay SHALL precede these item-state checks and return the original result. Successful outcomes SHALL obey the coordinator-board durable FYI contract; other outcomes SHALL not mutate or block unrelated successful actions. All successful receipts, resolutions and at most one FYI summary SHALL commit atomically per request. After commit, each newly resolved item SHALL emit one `hera.board_changed` event; comment-only SHALL not pretend the item changed revision. Decision answers SHALL retain their existing endpoint, payload and summary semantics.

#### Scenario: Mixed action batch

- **WHEN** one FYI receives a comment, another is dismissed and a third conflicts
- **THEN** results are commented, resolved and conflict, with one comment notice, no dismissal notice and no change to the conflicted item

#### Scenario: Replay after restart

- **WHEN** a newly authorized session retries an identical committed action ID after daemon restart
- **THEN** it receives the stored outcome and no duplicate comment, resolution event or notice

#### Scenario: ID reused with changed payload

- **WHEN** a committed action ID is reused with a different item, revision, action or note
- **THEN** that action is invalid and the original receipt/comment remains unchanged

#### Scenario: Closed FYI

- **WHEN** an FYI is resolved elsewhere before a new comment-only action arrives
- **THEN** the result is invalid and no comment/notice is recorded

#### Scenario: Invalid envelope

- **WHEN** a request contains seventeen actions, duplicate item IDs, an oversized note or trailing JSON
- **THEN** it returns 400 before any receipt, item change or notice is committed
