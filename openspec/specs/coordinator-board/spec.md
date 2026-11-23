# Coordinator board specification

## Purpose

Keep coordinator-curated human decisions and FYIs durable, current and visible across session recycles, with safe TUI reply composition and a shared REST surface.
## Requirements
### Requirement: Durable coordinator-owned board

The system SHALL store stable numeric-id items owned by coordinator role, with decision/fyi/action/todo kind, one-line subject, body, created/updated timestamps, read state, resolved state, resolver and optional resolution note. Only a coordinator binding SHALL authorize agent post/update/resolve/list tools. Material edits SHALL mark unread; identical edits SHALL preserve read state.

#### Scenario: Material edit and recycle

- **WHEN** a coordinator changes a read item's body and its session recycles
- **THEN** the same role retains the same unread item id and the fresh coordinator sees open ids and subjects in join, status and recycle context

#### Scenario: Ownership enforcement

- **WHEN** a worker or another coordinator attempts to mutate an item
- **THEN** the operation is rejected without changing the item

#### Scenario: Edit races a read acknowledgment

- **WHEN** the agent materially edits an item after Aaron expands its prior revision but before the asynchronous read acknowledgment commits
- **THEN** the edited item remains unread

### Requirement: Human board interaction

The TUI SHALL render collapsed entries with bold kind/id/subject headers, support j/k and arrows, Enter expansion marking read, r reply and universal x clearing as defined by Action and board todo controls. Reply SHALL focus the coordinator and paste the subject/id followed by two newlines with user input origin, without submitting. Decision dismissal SHALL durably notify its coordinator; resolution SHALL record the human as resolver.

Collapsed headers SHALL truncate with a trailing ellipsis when necessary, reserving space for the unread ` •` marker. Expanded headers SHALL show the full subject wrapped at word boundaries with continuation lines aligned under the subject's first character; all header lines SHALL be bold and only the selected first line SHALL be reverse-selected. The unread marker SHALL remain visible on the first header line. Indentation SHALL shrink in panes too narrow for the prefix.

Bodies SHALL wrap at word boundaries in display columns, preserve interior blank lines, and hard-break only tokens longer than the available line. Paired `**bold**` spans SHALL render bold without markers. Lines beginning `- `, `* ` or a number then `. `, optionally preceded by spaces, SHALL wrap with a hanging indent under the item text, nesting one level per two leading spaces. Unknown syntax SHALL stay literal. Wide runes SHALL not overwrite borders. Scrolling and cursor follow SHALL account for every wrapped header and body line while keeping the selected header visible after navigation.

#### Scenario: Reply composition

- **WHEN** Aaron presses r on an item in the focused board
- **THEN** the coordinator composer contains the reply prefix with an empty line ready for typing and no submission occurs

#### Scenario: Decision dismissal

- **WHEN** Aaron dismisses a decision
- **THEN** it is resolved by human and the coordinator inbox durably records that it was dropped
- **AND** resolution and notice commit atomically; the unread notice is delivered through the normal notifier as a bridge plugin-framed turn or PTY fallback, labelled `Aaron (board)` in doorbells, inbox/history and bridge mail UI

#### Scenario: Dismissal before binding or notification enqueue

- **WHEN** a human decision dismissal commits before the coordinator is bound or the process exits before enqueue
- **THEN** the pending notice remains durable and the notification tick delivers it after a live binding exists; repeated dismissal creates no second notice

#### Scenario: Expanded readable content

- **WHEN** an expanded item contains a long subject, ordinary words, blank lines, an overlong token and wide runes
- **THEN** its full bold subject hangs under the first subject character, its body wraps words while retaining blank lines and splitting only the overlong token, and wide runes stay inside the panel

#### Scenario: Bold and bullet body

- **WHEN** a body contains paired bold spans, bullet lines, unmatched markers and other markdown syntax
- **THEN** paired bold spans lose their markers and paint bold across wraps, bullet and numbered continuations align under their text, nested items indent one level per two spaces, and unmatched markers and unknown syntax remain literal

#### Scenario: Collapsed unread subject

- **WHEN** an unread collapsed item's subject exceeds the panel width
- **THEN** the subject ends with an ellipsis followed by a visible unread marker

#### Scenario: Cursor follows wrapped headers

- **WHEN** a wrapped expanded header pushes another item's selected header below the visible region and Aaron navigates to it
- **THEN** the selected header scrolls into view above the reserved hint row, and navigating back reveals the expanded item's selected first header line

### Requirement: Predictable Hera presentation

The default-on coordinator-board setting SHALL enable mailbox icons for unread coordinator items and landing on board when unread items exist at selection time. With the setting off the existing landing and icon behavior SHALL be restored. The lower details region SHALL toggle board/plan with p and SHALL NOT switch automatically during refresh. Cmd+Right SHALL use the details focus ladder. REST SHALL expose role-scoped items and per-coordinator unread counts independently of this setting.

#### Scenario: Refresh preserves view

- **WHEN** an unread item arrives while Aaron views a selected coordinator's plan
- **THEN** the plan remains visible until p or a new selection

#### Scenario: Default and disabled landing

- **WHEN** Aaron selects a coordinator with unread items
- **THEN** the board appears and the mailbox glyph shows if the setting is enabled, otherwise the existing plan and coordinator glyph remain

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

### Requirement: Focus-aware board hint row

The TUI SHALL reserve the bottom interior row of every visible board for a single muted hint, including empty boards. Unfocused boards SHALL show `⌘→ open board · p plan`; focused boards SHALL show `←/→ tabs · j/k move · ⏎ expand · r reply · x clear · c copy · o decide · p plan · ctrl+q back` with default bindings. Rebindable labels SHALL come from the active keymap; structural focus, Enter and back labels SHALL reflect the actual fixed keys. Items SHALL scroll above the hint. Narrow panes SHALL clip the hint to available cells without wrapping, overflow or item overlap.

#### Scenario: Unfocused and empty board

- **WHEN** the board is visible without focus, including with no open items
- **THEN** the bottom interior row shows the muted open-board and plan hint

#### Scenario: Focused board with many items

- **WHEN** focus enters a board whose items exceed its height
- **THEN** the bottom interior row shows movement, expand, reply, clear, copy, decide, plan and Ctrl+Q back hints while the selected item scrolls above it

#### Scenario: Rebound action

- **WHEN** a board action is rebound in the active keymap
- **THEN** the hint uses the new binding for that action

#### Scenario: Narrow board

- **WHEN** the board is narrower than its hint text
- **THEN** the hint clips cleanly on its reserved row without overwriting the border or item content

### Requirement: Orange unread mailbox glyph

The TUI SHALL color only the unread coordinator mailbox glyph with the existing orange theme color, preserving the row's other style attributes on selected rows. Normal coordinator icons and surrounding text SHALL retain their existing styles. This SHALL apply to orchestrator headers and bridging sub-coordinator rows.

#### Scenario: Unread mailbox on normal and selected rows

- **WHEN** a coordinator has unread board items with the board setting enabled
- **THEN** its mailbox glyph is orange on both normal and selected rail rows and the surrounding text keeps its row style

#### Scenario: Normal coordinator marker

- **WHEN** the coordinator has no unread items or the board setting is disabled
- **THEN** its normal coordinator icon keeps its existing style

### Requirement: Single expanded board item

The TUI board SHALL behave as an accordion. Enter on a collapsed item SHALL expand it, collapse any previously expanded item, and acknowledge the expanded revision as read. Enter on the expanded item SHALL collapse it. Cursor movement SHALL NOT change expansion. Refreshes SHALL preserve the expanded item by id while it remains in the open board; when it is resolved or removed, nothing SHALL remain expanded.

#### Scenario: Expand a different item

- **WHEN** item A is expanded and Aaron presses Enter on collapsed item B
- **THEN** only B is expanded and its displayed revision is marked read

#### Scenario: Collapse the expanded item

- **WHEN** Aaron presses Enter on the expanded item
- **THEN** no item is expanded

#### Scenario: Move without expanding

- **WHEN** Aaron moves the cursor with j/k or arrow keys
- **THEN** the expanded item remains unchanged

#### Scenario: Refresh retains expansion

- **WHEN** the board refreshes and the expanded item still exists
- **THEN** the same item remains expanded by id even if its content or order changes

#### Scenario: Expanded item disappears

- **WHEN** the expanded item is resolved or removed during refresh
- **THEN** no item is expanded, including if that id later returns

### Requirement: Scroll expanded board content

The TUI coordinator board SHALL scroll its rendered headers and bodies by three lines per mouse-wheel notch over its visible rectangle, even when another pane has keyboard focus. The offset SHALL clamp between zero and the final content viewport, reserving borders and the hint row. Wheel scrolling SHALL preserve keyboard focus, selection and expansion, and SHALL NOT affect the board when the pointer is over the roster or another pane. PgUp/PgDn SHALL continue to scroll a focused board. Selection navigation and Enter toggling SHALL bring the selected header into view.

#### Scenario: Read a tall expanded item

- **WHEN** Aaron expands a body taller than the board viewport and wheels down over the board
- **THEN** previously hidden body lines become visible, including the final line at the bottom clamp
- **AND** wheeling up returns to the first header without overscrolling

#### Scenario: Wheel without board focus

- **WHEN** the coordinator terminal or rail has keyboard focus and Aaron wheels over the board
- **THEN** board content scrolls and keyboard focus remains on its existing pane

#### Scenario: Pointer outside board

- **WHEN** Aaron wheels over the roster above the board
- **THEN** the board offset remains unchanged

#### Scenario: Keyboard and selection after scrolling

- **WHEN** Aaron uses PgUp/PgDn in the focused board and then navigates to another item or toggles expansion
- **THEN** page scrolling reads the body and subsequent selection or expansion reveals the selected header

### Requirement: Human-directed board capture

The system SHALL ship a builtin `hera-remind-me` skill that Aaron invokes to have the current agent put an item from the conversation on its coordinator's board. With argument text the agent SHALL capture what the text names; without it the agent SHALL capture the most recent actionable follow-up from the conversation. The item SHALL have a one-line subject and a body that states the action, its trigger condition, its rationale and any concrete references. Its kind SHALL be `todo` for future team work, `action` with blocking false for a deferred step Aaron must perform, `action` with blocking true when Aaron must act to unblock work now, `decision` for a human choice, and `fyi` for information only. Trigger text, pending readiness and any exact human step SHALL use the corresponding typed fields. The board kind `todo` SHALL remain separate from the Things 3 backend.

A coordinator SHALL list its open items first and update an existing item that already covers the same follow-up instead of posting a duplicate. A worker or freelance agent SHALL NOT call board tools; it SHALL relay the item to its coordinator with `hera_send` and ask the coordinator to post it. An agent with no Hera binding SHALL report that no board exists in this session and SHALL NOT post or send anything. The skill SHALL NOT change board ownership or authorization.

#### Scenario: Coordinator captures a release follow-up

- **WHEN** a coordinator has just said "when we release, bump the service worker version" and Aaron runs `/hera-remind-me`
- **THEN** a new open `todo` item exists on that coordinator's board whose subject names the follow-up and whose body includes the release trigger and the action

#### Scenario: Existing item covers the follow-up

- **WHEN** the coordinator's board already has an open item for the same follow-up and Aaron runs `/hera-remind-me` for it
- **THEN** the existing item is updated with the newer information and no second item is posted

#### Scenario: Worker relays to its coordinator

- **WHEN** Aaron runs `/hera-remind-me` in a worker session
- **THEN** the worker sends its coordinator a Hera message containing the proposed subject, kind, metadata and body with a request to post it, and makes no board tool call

#### Scenario: Plain task has no board

- **WHEN** Aaron runs `/hera-remind-me` in a task with no Hera binding
- **THEN** the agent says there is no board for this session and nothing is posted or sent

### Requirement: Structured board decisions

A `decision` board item MAY carry structured decision data: two to four options, each with a lowercase-kebab id (at most 40 characters, unique within the item, never `other`), a label (required, at most 80 characters), and a summary, upside and downside (each at most 400 characters); an optional `recommended` option id naming one of the options; and an optional `reason` (at most 400 characters). The item's `body` SHALL serve as the situation text. Structured data SHALL be stored on the item, serialized to at most 16 KiB, and SHALL be rejected on `fyi` items, on resident-approval items, and when any rule above is violated. An item with no structured data SHALL behave exactly as before.

`hera_board_post` and `hera_board_update` SHALL accept `options`, `recommended` and `reason`. `hera_board_update` SHALL keep the stored structured data when `options` is omitted and SHALL clear it when `options` is an empty array. A change to the structured data SHALL be a material edit: it marks the item unread and bumps `updated_at`; an identical resubmission SHALL preserve read state and `updated_at`. `hera_board_list`, the roster board REST route and the TUI SHALL return the structured data and, for resolved items, the recorded `resolution_choice`.

#### Scenario: Post a structured decision

- **WHEN** a coordinator posts a `decision` with three valid options and a recommended id
- **THEN** the item is stored with that structure and is returned unchanged by `hera_board_list`

#### Scenario: Plain decision unaffected

- **WHEN** a coordinator posts a `decision` with no options
- **THEN** the item behaves as a plain decision: `r` reply, `x` dismissal and Enter expansion are unchanged

#### Scenario: Invalid structure rejected

- **WHEN** a post supplies one option, five options, a duplicate id, the id `other`, a `recommended` id that names no option, or options on a `fyi` item
- **THEN** the call is rejected and no item is created or changed

#### Scenario: Body-only update keeps options

- **WHEN** a coordinator updates a structured item's body and omits `options`
- **THEN** the options, recommendation and reason are retained and the item is marked unread

#### Scenario: Clearing options

- **WHEN** a coordinator updates a structured item with `options: []`
- **THEN** the item becomes a plain decision

#### Scenario: Structure change is a material edit

- **WHEN** a coordinator changes only an option's downside text on a read item
- **THEN** the item becomes unread and its `updated_at` advances

### Requirement: Structured decision rendering in the TUI board

The TUI board SHALL render a structured decision's expanded view as its body, a blank line, an Options list in which each option shows its label (marking the recommended option) with Upside and Downside sub-bullets, and a Recommendation line carrying the reason. This content SHALL pass through the same wrapping, bold and bullet rendering as any body. Collapsed headers, Enter expansion, `r` reply and `x` dismissal SHALL be unchanged for structured and plain items.

#### Scenario: Expanded structured decision

- **WHEN** Aaron expands a structured decision
- **THEN** he sees the situation, each option with its upside and downside, which option is recommended, and the reason, all wrapped inside the panel

#### Scenario: Reply on a structured decision

- **WHEN** Aaron presses `r` on a structured decision
- **THEN** the coordinator composer receives the plain `Re: <subject> (#id)` prefix exactly as for a plain item

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

### Requirement: Typed board action and todo metadata

Board items SHALL accept kind `action` for a step only Aaron can perform and kind `todo` for future work the team performs. Decisions and FYIs SHALL retain their behavior. Top-level fields SHALL be `blocking` (boolean), `trigger` (text up to 1024 UTF-8 bytes), `trigger_pending` (boolean) and `command` (exact copyable text up to 16 KiB). Blocking and command SHALL be action-only; trigger and trigger_pending SHALL be action/todo-only. Decision structure SHALL remain decision-only. Action posts SHALL explicitly specify blocking. Pending triggers SHALL require nonempty trigger text and SHALL NOT be blocking. Empty strings SHALL clear optional text; nonempty whitespace-only text SHALL be invalid. Omitted update fields SHALL preserve values, except values incompatible with a changed kind SHALL clear. A trigger on post SHALL default to pending unless readiness is explicitly supplied; without a trigger it SHALL default ready. Material metadata edits SHALL mark unread and identical edits SHALL preserve read state. Agent post/update/list tools SHALL advertise and decode all fields consistently, list SHALL accept an optional four-kind filter, and REST SHALL expose the same stored metadata. Only existing coordinator authorization SHALL permit agent mutations.

#### Scenario: Blocking command

- **WHEN** a coordinator posts an action with blocking true and an exact command
- **THEN** the stored/listed item retains the exact command as data and no process executes it

#### Scenario: Deferred trigger

- **WHEN** a coordinator posts a todo with trigger “after 1.42 ships” and no readiness override
- **THEN** it is stored pending until the coordinator explicitly updates trigger_pending to false

#### Scenario: Invalid combinations and safe updates

- **WHEN** a coordinator supplies blocking on a todo, a pending blocking action or decision options on an action
- **THEN** the request is invalid without writing; valid omitted metadata updates preserve existing values and changing kind clears incompatible values

#### Scenario: Migration preserves dependent data

- **WHEN** an existing database upgrades to support all four kinds
- **THEN** IDs, existing row values, decision structures, approvals and response receipts remain intact, metadata has neutral defaults, foreign keys remain enforced and reopening is idempotent

### Requirement: Quiet deferred work and blocking priority

The board SHALL order unresolved blocking actions first, existing decisions/FYIs and ready nonblocking actions second, and pending items plus all board todos last, using ascending id within each band. Existing decisions/FYIs SHALL keep their relative order. Pending-trigger items and every board todo SHALL remain visible/listable with their trigger/readiness but SHALL NOT contribute to human-attention unread counts, mailbox badges, automatic board landing or needs-input jumps. Coordinator recovery SHALL include their ids, kinds and readiness. Coordinators SHALL verify triggers explicitly; the system SHALL NOT infer readiness from prose or automatically schedule work. Ready actions SHALL retain ordinary unread behavior.

#### Scenario: Blocking beats freshness and read state

- **WHEN** a read blocking action and a newer unread decision exist
- **THEN** the blocking action is displayed first while the decision retains its existing unread behavior

#### Scenario: Quiet future work

- **WHEN** the only unread items are pending actions and board todos
- **THEN** all items stay visible but the coordinator has no human-attention unread badge or board-only jump target

#### Scenario: Human step becomes urgent

- **WHEN** a coordinator verifies a deferred action's trigger and updates it to trigger_pending false and blocking true
- **THEN** the material edit is unread, contributes to attention and sorts first

### Requirement: Durable action and board todo completion

An action SHALL resolve as `done` with optional note. A board todo SHALL resolve as `done` or `dropped` with optional note. Resolution SHALL store outcome in resolution_choice, resolver and note, atomically enqueue a durable coordinator notice and publish an event only after commit. Human operations SHALL require expected revision, use idempotent action receipts and reject incompatible outcomes/kinds. Agent resolution SHALL default to done for these kinds and accept an explicit dropped outcome only for board todos. Read/acknowledgment SHALL NOT complete either kind, and no UI SHALL execute commands. Existing decision/FYI resolution and notice semantics SHALL remain unchanged.

#### Scenario: Human completion survives delivery loss

- **WHEN** Aaron marks an action done, the transaction commits and the HTTP response or live notice delivery is lost
- **THEN** the optional note and done outcome persist, retry replays the same receipt and exactly one notice remains deliverable

#### Scenario: Team work dropped

- **WHEN** Aaron or its coordinator resolves a board todo as dropped
- **THEN** it resolves with dropped outcome and its notice distinguishes dropped from done

#### Scenario: Invalid or stale completion

- **WHEN** Aaron attempts dropped on an action or completes a stale revision
- **THEN** the operation returns invalid or conflict without changing the item or sending a completion notice

### Requirement: Action and board todo controls

The TUI and SPA board SHALL label all four kinds and show action blocking state, trigger/readiness and exact copyable step. The TUI SHALL provide a rebindable universal clear action (hera_board.dismiss, default x): dismiss FYIs and decisions, resolve actions and board todos as done, and retain the resident approval review-and-deny flow. No separate hera_board.done action or default d binding SHALL exist. Dropping a board todo SHALL remain available on the web Decide page. A copy action SHALL copy the exact command/step. Optional completion notes SHALL be available in the browser completion UI. Keymap help and Reference documentation SHALL describe the new controls. Coordinator orientation, board tools and builtin Hera/remind-me guidance SHALL distinguish human steps, future team work, choices and FYIs, including explicit trigger updates and the Things naming distinction. New operations SHALL log success, failure and skipped/stale work without command contents.

#### Scenario: One-key done

- **WHEN** Aaron presses x on the selected action or board todo
- **THEN** the exact visible revision resolves done without requiring a note and the coordinator receives its durable completion notice

#### Scenario: Copy human step

- **WHEN** Aaron invokes Copy on an action's command
- **THEN** the exact stored text is copied without execution

#### Scenario: Guidance classifies reminders

- **WHEN** a coordinator captures future team work or a deferred command Aaron must run
- **THEN** guidance selects board todo for the former and deferred action for the latter, with typed trigger data

#### Scenario: Clear informational and decision items

- **WHEN** Aaron presses x on a selected FYI or decision
- **THEN** existing dismissal semantics remain, including silent FYI dismissal and the durable decision notice

#### Scenario: Deny resident call

- **WHEN** Aaron presses x on a resident approval call
- **THEN** the existing review and explicit denial flow remains and generic dismissal/completion is not invoked

#### Scenario: Separate done key removed

- **WHEN** Aaron presses d with default bindings on any board item
- **THEN** no board completion or dismissal occurs

#### Scenario: Browser todo dropping retained

- **WHEN** Aaron needs to drop rather than complete a todo
- **THEN** web Decide still offers Dropped while TUI x records done
