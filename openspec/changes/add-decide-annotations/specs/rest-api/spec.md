## MODIFIED Requirements

### Requirement: Board answers endpoint

The REST API SHALL expose `POST /api/hera/roles/{role_id}/board/answers`, accepting a master or device token for any coordinator role or a decision session for its own role, with body `{"answers":[{"item_id":N,"expected_updated_at":"<RFC3339>","choice":"<option id>|other","note":"<text>"}]}`. The body SHALL contain exactly one JSON value, at most 16 answers, notes of at most 4 KiB, a nonzero `expected_updated_at` per answer, and SHALL be consumed through EOF before any mutation. The endpoint SHALL return 200 with `{"results":[{"item_id":N,"outcome":"<outcome>","item":{…}}],"unread_count":N}` where each outcome is one of `resolved`, `conflict`, `already_resolved`, `not_found` or `invalid`, and `item` is the current item for `conflict` outcomes. A malformed body SHALL return 400 `invalid_request`. All `resolved` items and the single summary message described in `board-decision-page` SHALL commit in one transaction; non-`resolved` outcomes SHALL change nothing and SHALL NOT prevent other answers in the same request from resolving. A `hera.board_changed` event SHALL be emitted once per resolved item after commit.


For currently rail-visible coordinators, rail-board sessions SHALL retain their existing authorization. The answer payload SHALL additionally accept action (answer/comment; omitted means answer), action_id and annotations. Every annotation SHALL carry a unique annotation_id, type (COMMENT or DELETION), source-field start/end paths and UTF-16 rendered-text offsets, selected-text hash and prefix/suffix context bounded to 64 Unicode code points per boundary, plus optional comment; the server SHALL derive and persist its quote from the expected revision. Annotation-bearing answers and all comments SHALL require a valid unique action ID under the existing FYI ID grammar. Comment SHALL require empty choice and at least a non-whitespace note or annotation; it SHALL return commented without resolving. Answers SHALL retain choice and required-note validation. COMMENT annotations SHALL require non-whitespace comment text at most 4096 UTF-8 bytes; DELETION SHALL allow an empty comment with the same maximum when supplied. At most 20 annotations of all types combined SHALL be accepted per item; quote text SHALL be capped at 512 UTF-8 bytes. Unknown/excluded or out-of-bounds ranges, mismatched hashes/context, duplicate annotation IDs or unsupported types/replacement fields SHALL be invalid. Overlapping and identical ranges with distinct IDs SHALL be valid; no source edit SHALL be applied. Oversized fields, duplicate action/item IDs, trailing JSON and invalid envelopes SHALL return 400 before mutation. The bounded decoder SHALL accommodate sixteen maximum-size annotated actions including JSON escaping. Receipt replay SHALL precede staleness/closed checks; payload mismatch SHALL be invalid. New annotations on closed decisions SHALL return invalid, never already_resolved as a feedback success. Unannotated ordinary answers retain existing behavior. All successful results (including commented) and their single summary SHALL follow the durable coordinator-board contract. Only newly resolved items SHALL emit board_changed events. Logs SHALL contain IDs/counts/outcomes, never annotations or quotes.
#### Scenario: Mixed annotated outcomes

- **WHEN** one annotated answer resolves, one decision comment succeeds and another conflicts
- **THEN** the batch returns resolved, commented and conflict, commits one existing summary for successful actions and leaves the comment-only decision open

#### Scenario: Invalid anchors and limits

- **WHEN** an action names an excluded heading range, repeats an annotation ID, exceeds 20 annotations or has an oversized multibyte comment
- **THEN** invalid ranges/IDs/types yield per-action invalid and envelope limit violations return 400 before mutation

#### Scenario: Payload collision

- **WHEN** a committed decision action ID is reused with changed annotations
- **THEN** invalid is returned without changing the original receipt or duplicating feedback


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

### Requirement: Retry-safe FYI actions endpoint

`POST /api/hera/roles/{role_id}/board/fyi-actions` SHALL accept master/device tokens or a rail-board session for a currently rail-visible coordinator. The body SHALL be `{"actions":[{"action_id":"<opaque client ID>","item_id":N,"expected_updated_at":"<RFC3339>","action":"comment|dismiss|comment_and_dismiss","note":"…"}]}` with one to sixteen actions. Each action ID SHALL be 1–128 ASCII letters, digits, hyphens or underscores, unique within the request; item IDs SHALL also be unique within the request. Timestamps SHALL be nonzero, notes SHALL be at most 4 KiB UTF-8 and comment actions SHALL require non-whitespace notes; dismiss SHALL require an empty note. Invalid envelope, duplicate IDs/items, oversized notes or trailing JSON SHALL return 400 `invalid_request` before any mutation. An unsupported action or empty required comment SHALL yield a per-action invalid result. The whole body SHALL be consumed through EOF before mutation.

The response SHALL be `{"results":[{"action_id":"…","item_id":N,"outcome":"commented|resolved|already_resolved|conflict|not_found|invalid","item":{…}}],"unread_count":N}`; conflict SHALL include current item content. Missing/wrong-role/non-FYI items SHALL return not_found without leaking content; resident approvals SHALL be invalid. Current revision mismatch SHALL return conflict. Already-resolved FYIs SHALL return already_resolved for dismiss/comment_and_dismiss without storing a new comment or notice; comment-only SHALL return invalid because feedback was not delivered. Successful receipt replay SHALL precede these item-state checks and return the original result. Successful outcomes SHALL obey the coordinator-board durable FYI contract; other outcomes SHALL not mutate or block unrelated successful actions. All successful receipts, resolutions and at most one FYI summary SHALL commit atomically per request. After commit, each newly resolved item SHALL emit one `hera.board_changed` event; comment-only SHALL not pretend the item changed revision. Decision answers SHALL retain their existing endpoint, payload and summary semantics.


Actions SHALL additionally accept annotations using the same revision-bound range/type/ID validation, 20-per-item maximum, 4096 UTF-8 byte comment cap and 512-byte server-derived quote cap as decision responses. Comment/comment_and_dismiss SHALL require a non-whitespace note or at least one annotation; dismiss SHALL require both empty note and no annotations. Existing durable fingerprint/replay semantics SHALL include full annotations. New feedback-bearing actions targeting closed FYIs SHALL be invalid, including comment_and_dismiss with annotations; only replay may confirm earlier delivered feedback. Request decoding SHALL remain bounded while accommodating sixteen maximal annotated actions and JSON escaping. Decision answers now support annotation-bearing answers and comment actions as specified by Board answers endpoint.
#### Scenario: FYI annotation without general note

- **WHEN** a valid Comment action has annotations and empty note
- **THEN** commented is returned with full annotations durably stored and no item resolution

#### Scenario: Dismiss cannot swallow annotations

- **WHEN** dismiss carries an annotation
- **THEN** invalid is returned without dropping or delivering that feedback


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

## ADDED Requirements

### Requirement: Full board response receipt recovery

The existing master/device-only GET /api/hera/roles/{role_id}/board/fyi-actions/{action_id} receipt read path SHALL be extended to return full durable decision response receipts as well as FYI receipts, with explicit item kind/action and full annotations. IDs SHALL be unique across response kinds within the owning role. Unknown/wrong-role receipts SHALL return 404; rail-board sessions and scope tokens SHALL remain forbidden (403). Coordinator recovery through the authenticated API proxy SHALL be documented with the role/action pointer emitted in summaries. No new transport SHALL be introduced.

#### Scenario: Full feedback recovery

- **WHEN** an authorized coordinator proxy requests a receipt named by a truncated annotation summary
- **THEN** the existing receipt path returns the original kind/action, types, offsets/context, stored quoted previews and full comments

#### Scenario: Receipt auth boundary

- **WHEN** a rail-board session requests stored annotation receipt history
- **THEN** 403 is returned even when that role is visible


### Requirement: Range annotation validation and replay

Both existing response endpoints SHALL derive selectable text using the canonical rendered-text projection described in board-decision-page, validate item-scoped ranges against the expected revision, exclude subject/headings/UI controls and reject cross-item ranges. Selected-text hash and offset/context SHALL distinguish identical text; quote previews SHALL truncate safely to 512 UTF-8 bytes while receipts retain the complete anchor. Types and anchors SHALL be part of the normalized durable fingerprint. Overlapping ranges SHALL remain independent records. Logs SHALL include IDs/types/counts/outcomes only, never quoted text, context, comments or hashes.

#### Scenario: Allowed overlap

- **WHEN** two saved annotations overlap or select the same text with distinct IDs
- **THEN** both are valid within the total cap, persist independently and replay without duplication

#### Scenario: Typed durable delivery

- **WHEN** a decision or FYI receives a DELETION with no comment and a COMMENT with text
- **THEN** one existing summary lists both types and quotes, all full comments/range anchors are in receipts and board text is unchanged

#### Scenario: Bad range

- **WHEN** a client submits an offset outside authored text or a hash/context not matching that revision
- **THEN** that action is invalid with no feedback delivery or item mutation
