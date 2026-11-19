## ADDED Requirements

### Requirement: Durable human FYI actions

Human FYI dismissal SHALL resolve the item with `resolved_by=human`, empty resolution choice and note, and advanced `updated_at`, without creating any coordinator notice, matching TUI `x`. A human comment SHALL leave the FYI open with unchanged read state and item revision, store its full note durably and enqueue feedback through the existing board-notice outbox. Comment and dismiss SHALL atomically store the note, resolve by human with empty choice and the note as resolution note, advance `updated_at` and enqueue feedback. Successful actions SHALL have durable receipts keyed by owning role and client action ID, retaining original payload fingerprint, outcome and any full comment up to 4 KiB across restarts/recycles. Receipts SHALL remain retrievable for the lifetime of the board history; identical retries SHALL return the stored result without a duplicate mutation or notice, and changed-payload reuse SHALL be invalid.

#### Scenario: Silent FYI dismissal

- **WHEN** Aaron dismisses an open FYI from the page or TUI
- **THEN** it is resolved by human and no dropped or comment message is queued

#### Scenario: Comment only

- **WHEN** Aaron comments on an FYI
- **THEN** full feedback is durably recorded and queued for its coordinator while the FYI stays open with unchanged read/revision state

#### Scenario: Combined action rolls back

- **WHEN** committing a comment-and-dismiss transaction fails
- **THEN** neither resolution, receipt nor notice commits

#### Scenario: Idempotent comment

- **WHEN** an identical successful comment action is retried after a lost response or restart
- **THEN** its original result is returned and exactly one full comment and one original notice exist

### Requirement: FYI comment summaries in the board-notice outbox

A successful FYI batch containing new comments SHALL atomically create exactly one self-addressed summary for the owning coordinator through the existing board-notice outbox, attributed `Aaron (board)`. It SHALL list commented item IDs/subjects/notes in ID order, explicitly distinguishing open commented items from comment-and-dismiss items already resolved, followed by that coordinator's remaining open decision and FYI IDs. Silent dismissals SHALL NOT appear as feedback entries or generate notices, though their state SHALL be reflected in remaining-open IDs when a comment notice is produced. A batch with no new comments SHALL enqueue no notice. Summary size SHALL stay within 64 KiB using evenly budgeted notes, preserving full notes in action receipts; shortened notes SHALL end with a pointer naming role ID and action ID. An authenticated `GET /api/hera/roles/{role_id}/board/fyi-actions/{action_id}` SHALL return the successful receipt and full comment to master/device clients and return 404 for an unknown receipt; decision sessions and scope tokens SHALL receive 403. This read route SHALL be documented for coordinator recovery through the authenticated API proxy. Delivery SHALL be durable and exactly once even when no binding exists at commit, and SHALL never also create a dropped notice.

#### Scenario: Mixed comments and dismissals

- **WHEN** a batch comments on one FYI, comments and dismisses another, and silently dismisses a third
- **THEN** one notice describes the first two with their different open/resolved states, no feedback entry describes the silent dismissal and remaining-open IDs reflect all committed changes

#### Scenario: Oversized combined notes

- **WHEN** sixteen comments each contain 4 KiB
- **THEN** full notes survive in receipts, the summary stays within 64 KiB and each shortened note points to its retrievable role/action receipt

#### Scenario: Coordinator unbound

- **WHEN** a comment commits while the coordinator has no binding
- **THEN** feedback remains in the outbox and is delivered once a binding exists without loss or duplication
