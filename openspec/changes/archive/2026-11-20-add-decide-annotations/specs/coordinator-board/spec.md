## MODIFIED Requirements

### Requirement: Coordinator maintenance guidance

Coordinator orientation and builtin Hera guidance SHALL instruct agents to put human decisions and attention on the board, rewrite superseded items, and resolve settled items including completed follow-up after a human reply. The `hera_board_*` tool description, builtin skill and orientation SHALL also tell coordinators to format each body for scanning: one short lead sentence, a blank line, then one `- **Label:** detail` bullet per fact or option (two-space `  - ` sub-bullets, each decision option its own bullet plus a recommendation bullet, links on their own bullet), never a wall of prose.

The same guidance SHALL tell coordinators to post a decision as structured whenever it has distinct options: two to four options, always a recommendation with a reason, with the body stating only the situation and the options appearing only in `options`. It SHALL tell coordinators that Aaron presses `o` on the board to answer a round of decisions in the browser, that each answer arrives as one summarized message in which the listed items are already resolved, that `Still open` items need follow-up, and that a "Something else" answer's note is the answer.


The board tool description, builtin Hera skill and coordinator spawn/reclaim orientation SHALL additionally explain typed range annotations and require coordinators to address each comment, retrieving full response receipts when summaries are shortened. They SHALL distinguish feedback-only open decisions from answered decisions and update settled follow-up on the board.
#### Scenario: Annotated feedback arrives

- **WHEN** a coordinator receives a summary with several typed quoted comments
- **THEN** its guidance requires addressing each annotation and does not treat a comment-only decision as resolved


#### Scenario: Fresh coordinator

- **WHEN** a coordinator starts or reclaims its role
- **THEN** it receives the maintenance rule and open board item ids/subjects

#### Scenario: Decision with options

- **WHEN** a coordinator reads the board tool description, builtin skill or orientation
- **THEN** it is told to post a decision that has distinct options as a structured decision with a recommendation and reason

#### Scenario: Answers arrive

- **WHEN** a coordinator receives Aaron's summarized answers
- **THEN** its guidance tells it the listed items are already resolved and to follow up on any still-open items

### Requirement: Durable human FYI actions

Human FYI dismissal SHALL resolve the item with `resolved_by=human`, empty resolution choice and note, and advanced `updated_at`, without creating any coordinator notice, matching TUI `x`. A human comment SHALL leave the FYI open with unchanged read state and item revision, store its full note durably and enqueue feedback through the existing board-notice outbox. Comment and dismiss SHALL atomically store the note, resolve by human with empty choice and the note as resolution note, advance `updated_at` and enqueue feedback. Successful actions SHALL have durable receipts keyed by owning role and client action ID, retaining original payload fingerprint, outcome and any full comment up to 4 KiB across restarts/recycles. Receipts SHALL remain retrievable for the lifetime of the board history; identical retries SHALL return the stored result without a duplicate mutation or notice, and changed-payload reuse SHALL be invalid.


An FYI comment or comment-and-dismiss SHALL also accept annotations in addition to or instead of a general note. Full annotations SHALL be stored with the existing durable receipt and payload fingerprint. Dismiss-only SHALL reject annotations rather than silently discard them; page submission SHALL use comment-and-dismiss when feedback accompanies dismissal.
#### Scenario: Annotated FYI receipt

- **WHEN** an FYI annotation without a general note is submitted as Comment
- **THEN** full type/range/quote/comment entries persist in the receipt and the FYI stays open


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


The same existing summary SHALL include each annotation as a type, quoted selection and any comment; no separate annotation notice SHALL be sent. Notes and annotation entries SHALL share the 64 KiB summary budget, with a role/action receipt pointer for every shortened or omitted entry. Full stored annotation feedback SHALL be included in the privileged receipt response.
#### Scenario: Large annotation summary

- **WHEN** sixteen FYI actions each carry twenty 4 KiB annotations
- **THEN** the single existing batch summary fits 64 KiB, identifies shortened/omitted feedback with receipt pointers and full feedback remains retrievable


#### Scenario: Mixed comments and dismissals

- **WHEN** a batch comments on one FYI, comments and dismisses another, and silently dismisses a third
- **THEN** one notice describes the first two with their different open/resolved states, no feedback entry describes the silent dismissal and remaining-open IDs reflect all committed changes

#### Scenario: Oversized combined notes

- **WHEN** sixteen comments each contain 4 KiB
- **THEN** full notes survive in receipts, the summary stays within 64 KiB and each shortened note points to its retrievable role/action receipt

#### Scenario: Coordinator unbound

- **WHEN** a comment commits while the coordinator has no binding
- **THEN** feedback remains in the outbox and is delivered once a binding exists without loss or duplication

## ADDED Requirements

### Requirement: Durable human decision annotations

Each stored annotation SHALL retain its type, stable ID, server-validated range offsets/context and quote preview. COMMENT requires a nonempty comment; DELETION may have an empty comment and denotes a removal suggestion, never a direct board edit. Summaries and full receipt recovery SHALL expose type, quote and any comment; no replacement text field SHALL exist because Replace is not supported. Coordinators SHALL address deletion suggestions as well as comments.

Annotation-bearing answers and all decision comments SHALL have durable receipts keyed by owning role and action ID, with normalized payload fingerprint, original revision, annotation IDs/types, range offsets/context, server-derived quote text and full comments retained for board-history lifetime. Decision comments SHALL leave the decision open with unchanged revision/read state. Receipt, resolution if any and one existing answer-route summary SHALL commit atomically through the board-notice outbox, attributed Aaron (board). Summaries SHALL distinguish comments still open from answers already resolved and include remaining open IDs of both kinds. Quotes SHALL be bounded to 512 UTF-8 bytes without splitting characters. Summary notes/annotations SHALL fit 64 KiB with receipt recovery pointers. Identical receipt replay SHALL precede current item checks and create no duplicate notices/events. Changed-payload ID reuse SHALL be invalid. New feedback on already-closed items SHALL be invalid rather than accepted as delivered. Resident approvals SHALL remain protected. Successful annotation feedback SHALL survive daemon restarts/coordinator recycles and pending notices SHALL deliver once binding exists.

#### Scenario: Atomic annotation response

- **WHEN** the process exits around receipt/notice commit
- **THEN** either neither commits or full receipt and pending single summary exist; delivery later occurs once

#### Scenario: Decision comment replay

- **WHEN** a successful comment is retried after restart and item revision change
- **THEN** the original receipt result returns and no new message or mutation occurs

#### Scenario: Closed elsewhere

- **WHEN** fresh annotation feedback targets an already-resolved decision
- **THEN** invalid reports undelivered feedback and no receipt/notice falsely confirms delivery

#### Scenario: Owner isolation

- **WHEN** a summary includes annotations from one coordinator batch
- **THEN** it contains no other coordinator board content
