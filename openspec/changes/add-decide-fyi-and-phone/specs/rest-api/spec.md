## MODIFIED Requirements

### Requirement: Decision session exchange and auth class

POST /api/hera/board/decision-session SHALL accept one ticket body without bearer authentication because the ticket is the credential, returning `{session,initial_role_id,expires_at}`. An unknown/expired/used ticket SHALL return 401. Sessions SHALL have 256 random bits, be stored only as hashes in daemon memory and expire absolutely eight hours after exchange. Both TUI and phone launches SHALL use an explicit rail-board auth class permitting only GET /api/hera/board/decision-navigation and the per-role GET board/decisions, POST board/answers, GET board/decide-items and POST board/fyi-actions routes for currently rail-visible coordinators. The launch role SHALL be initial selection only. Middleware SHALL not trust client-supplied internal auth tags. Each request SHALL use canonical rail visibility checks excluding hidden, archived, nuked or non-coordinator targets, including parent visibility restrictions. All other routes, including broad roster/count discovery, launch minting, receipt-history reads and privileged handlers, SHALL return 403 for this session class. Master/device/scope gates SHALL not admit a rail-board session implicitly. Daemon restart SHALL invalidate tickets/sessions. An empty visible roster SHALL yield an empty page, not broader permissions.

#### Scenario: Ticket is single-use and short-lived

- **WHEN** a ticket is exchanged a second time or after 90 seconds
- **THEN** the response is 401

#### Scenario: Shared TUI and phone scope

- **WHEN** a session launched on role 12 requests another currently rail-visible coordinator’s allowed board route
- **THEN** the request is authorized; the same request for a hidden/archived/nuked role or worker returns 403

#### Scenario: Visibility changes

- **WHEN** a coordinator is newly visible or becomes hidden during a session
- **THEN** navigation refresh may include the new row and hidden-board requests fail immediately, regardless of stale client metadata

#### Scenario: Privileged route rejected

- **WHEN** a rail-board session requests link minting, aggregate counts, receipt history, general mutation or MCP tool registration
- **THEN** it receives 403 with no side effects

#### Scenario: Restart invalidates

- **WHEN** the daemon restarts
- **THEN** prior sessions return 401 while durable receipts remain available after a new authorized launch

### Requirement: Board decision launch link

POST /api/hera/roles/{role_id}/board/decision-link SHALL require a master or device token and return `{path:"/decide?role=<id>[&focus=<item_id>]",ticket,expires_at}`. An optional focus_item_id body SHALL select initial focus. The ticket SHALL be 256 random bits hashed only in daemon memory, valid 90 seconds and single-use. It SHALL mint the same rail-board session class as phone launch; the role ID SHALL specify preselection only. Unknown targets SHALL return 404 board_item_not_found; non-coordinators SHALL return 400 invalid_request; hidden/archived/nuked targets SHALL be refused without granting access. Empty boards SHALL be valid launch targets. Decision sessions SHALL not mint tickets. No ticket SHALL be logged or persisted.

#### Scenario: TUI preselection

- **WHEN** a master/device client launches an empty visible coordinator role
- **THEN** it receives a common-page ticket whose initial_role_id selects that coordinator while navigation may reach other visible coordinators

#### Scenario: Invalid target

- **WHEN** the target is unknown, non-coordinator or not visible under rail rules
- **THEN** launch is rejected with no ticket or unauthorized board access

## ADDED Requirements

### Requirement: Authenticated aggregate decision launch

`POST /api/hera/board/decision-link` SHALL require a master or device token, reject other authenticated token classes with 403 and unauthenticated requests with 401, and return `{"path":"/decide","ticket":"<opaque>","expires_at":"<RFC3339>"}`. It SHALL accept an empty body or one empty JSON object and reject malformed/trailing JSON with 400 before minting. The server SHALL mint the rail-board auth class, whose permitted coordinator membership is resolved through current canonical rail visibility rules, including empty and unbound visible coordinators and nested sub-coordinators. Clients SHALL NOT supply authorization scope. The initial role SHALL be the first non-empty coordinator in rail order, or the first visible row when all are empty, or null when no rows exist. The ticket SHALL have 256 bits of randomness, be held only as a hash in daemon memory, expire in 90 seconds and exchange once through the existing session endpoint. The path SHALL be same-origin relative and contain no credentials. The role-specific launch endpoint SHALL use the same session class with its requested coordinator as preselection.

#### Scenario: Phone launch without the TUI

- **WHEN** a device-authenticated PWA requests an aggregate launch
- **THEN** it receives a ticket exchangeable for the rail-board session without any TUI action

#### Scenario: No eligible coordinators

- **WHEN** the eligible role set is empty
- **THEN** launch succeeds with no initial selection and the page shows no visible coordinators without broadening API access

#### Scenario: Scoped credential rejected

- **WHEN** a plugin scope token or decision session tries to mint an aggregate ticket
- **THEN** it receives 403 and no ticket exists

### Requirement: Aggregate open board counts

`GET /api/hera/board/decision-counts` SHALL require master/device authentication and return `{"decisions":N,"fyis":N,"total":N,"roles":[{"role_id":N,"orchestrator":"…","coordinator":"…","decisions":N,"fyis":N}]}` for the same canonical rail visibility/order policy as navigation, including visible empty and nested coordinators. Counts SHALL include open items regardless of read state, exclude resolved items and leave read state and timestamps unchanged. Totals and per-role counts SHALL come from one consistent read snapshot. Decision sessions SHALL NOT call this discovery endpoint.

#### Scenario: Count both kinds

- **WHEN** eligible boards contain two unread decisions, a read FYI and a resolved FYI
- **THEN** counts are decisions 2, FYIs 1 and total 3 without mutating board state

### Requirement: Mixed decision page item reads

`GET /api/hera/roles/{role_id}/board/decide-items` SHALL accept master/device tokens for an eligible coordinator or a rail-board session for a currently rail-visible coordinator. It SHALL return `{"role_id":N,"orchestrator":"…","coordinator":"…","decisions":N,"fyis":N,"items":[…]}` from a consistent snapshot, with all open decisions and FYIs in ascending item-ID order. Each item SHALL include existing decision fields plus `kind`; FYIs SHALL have null structured data. The endpoint SHALL NOT mark items read or change timestamps. Existing `/board/decisions` SHALL retain its decision-only filter and response shape; a rail-board session SHALL access it only for currently rail-visible coordinators.

#### Scenario: Mixed read

- **WHEN** an eligible board has a decision, an FYI and a resolved item
- **THEN** decide-items returns the two open items with kind and counts while decisions returns only the open decision

#### Scenario: Wrong role

- **WHEN** a session requests decide-items for a role not visible under canonical rail rules
- **THEN** it receives 403 and no board data

### Requirement: Retry-safe FYI actions endpoint

`POST /api/hera/roles/{role_id}/board/fyi-actions` SHALL accept master/device tokens or a rail-board session for a currently rail-visible coordinator. The body SHALL be `{"actions":[{"action_id":"<opaque client ID>","item_id":N,"expected_updated_at":"<RFC3339>","action":"comment|dismiss|comment_and_dismiss","note":"…"}]}` with one to sixteen actions. Each action ID SHALL be 1–128 ASCII letters, digits, hyphens or underscores, unique within the request; item IDs SHALL also be unique within the request. Timestamps SHALL be nonzero, notes SHALL be at most 4 KiB UTF-8 and comment actions SHALL require non-whitespace notes; dismiss SHALL require an empty note. Invalid envelope, duplicate IDs/items, oversized notes or trailing JSON SHALL return 400 `invalid_request` before any mutation. An unsupported action or empty required comment SHALL yield a per-action invalid result. The whole body SHALL be consumed through EOF before mutation.

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

### Requirement: Scoped decision navigation metadata

GET /api/hera/board/decision-navigation SHALL accept master/device tokens or a rail-board session and return `{roles:[{role_id,parent_role_id,orchestrator,coordinator,decisions,fyis}]}` from a consistent snapshot. Rows SHALL be exactly the canonical Hera rail-visible coordinator projection in depth-first rail order, excluding hidden/archived/nuked branches and workers, including nested sub-coordinators and empty or unbound visible coordinators. parent_role_id SHALL reference the canonical visible coordinator parent or null. Counts SHALL include open items regardless of read state; reads SHALL not mutate item state. The endpoint SHALL share visibility/order policy with the TUI rather than inventing a name/ID sort. It SHALL expose no task/session bodies or broad roster details. Kanban section headings and collapse controls are omitted while their ordering is preserved.

#### Scenario: Canonical tree and counts

- **WHEN** the rail includes a parent, its sub-coordinator, an empty coordinator, plain workers and hidden/archived branches
- **THEN** metadata contains only the visible coordinator rows in rail order with canonical parent IDs and accurate kind counts, including the empty row

#### Scenario: Session cannot use broad roster

- **WHEN** a rail-board session calls scoped navigation then the general Hera roster
- **THEN** scoped navigation succeeds and the general roster is forbidden
