## ADDED Requirements

### Requirement: Human board deferral

Every open board item, including decision, fyi, action, todo and resident approval items, SHALL support a reversible human-owned `deferred_at` timestamp through a revision-checked REST mutation. Coordinator MCP tools SHALL NOT set or clear deferral; their list output SHALL expose it. Deferral SHALL preserve read state and authored `updated_at`, and repeated identical operations SHALL be idempotent. Deferred items SHALL remain ordinarily resolvable. Material coordinator updates SHALL retain deferral and mark the item unread normally.

#### Scenario: Defer and restore every kind

- **WHEN** Aaron defers an open item of any kind then restores it
- **THEN** its timestamp is set then cleared without changing its read state, authored revision or resolution state

#### Scenario: Stale deferral

- **WHEN** Aaron defers a revision that the coordinator has since changed
- **THEN** REST returns a conflict and the item is unchanged

#### Scenario: Update while deferred

- **WHEN** the coordinator materially updates a read deferred item
- **THEN** it remains deferred and becomes unread, and coordinator list output includes its deferral timestamp

### Requirement: Deferred board visibility and attention

Deferred items SHALL NOT contribute to human attention, unread counts or board needs-input rollups. TUI and web boards SHALL hide them by default and provide show/hide controls with an open deferred count. Each item SHALL offer defer/restore, and shown deferred items SHALL retain visible unread state. The Decide page SHALL exclude deferred items from its default review list. Any read-only macOS board rendering SHALL hide deferred items. The label “deferred” SHALL refer only to the human flag; trigger/readiness labels for future work SHALL use “later.”

Deferring an item in Decide SHALL clear its queued answer or action while preserving written notes and annotations, so a hidden item is not accidentally submitted in the current response round.

#### Scenario: Hidden unread item

- **WHEN** Aaron defers an unread blocking action
- **THEN** it disappears from the default board and attention summaries, and the deferred count increases

#### Scenario: Show deferred

- **WHEN** Aaron toggles deferred visibility in the TUI or web board
- **THEN** deferred entries appear with defer state and unread markers, and another toggle hides them

#### Scenario: Resolve deferred item

- **WHEN** an item is resolved while deferred
- **THEN** it leaves the open deferred count and normal resolution semantics remain intact

#### Scenario: Defer a queued response

- **WHEN** Aaron queues Done or an answer and then defers that item in Decide
- **THEN** the item stays open and leaves the response queue while its written feedback is retained

## MODIFIED Requirements

### Requirement: Focus-aware board hint row

The TUI SHALL reserve the bottom interior row of every visible board for a single muted hint, including empty boards. Unfocused boards SHALL show `⌘→ open board · p plan`; focused boards SHALL show `←/→ tabs · j/k move · ⏎ expand · r reply · x clear · c copy · d defer · D deferred · o decide · p plan · ctrl+q back` with default bindings. Rebindable labels SHALL come from the active keymap; structural focus, Enter and back labels SHALL reflect the actual fixed keys. Items SHALL scroll above the hint. Narrow panes SHALL clip the hint to available cells without wrapping, overflow or item overlap.

#### Scenario: Unfocused and empty board

- **WHEN** the board is visible without focus, including with no open items
- **THEN** the bottom interior row shows the muted open-board and plan hint

#### Scenario: Focused board with many items

- **WHEN** focus enters a board whose items exceed its height
- **THEN** the bottom interior row shows movement, expand, reply, clear, copy, defer, deferred visibility, decide, plan and Ctrl+Q back hints while the selected item scrolls above it

#### Scenario: Rebound action

- **WHEN** a board action is rebound in the active keymap
- **THEN** the hint uses the new binding for that action

#### Scenario: Narrow board

- **WHEN** the board is narrower than its hint text
- **THEN** the hint clips cleanly on its reserved row without overwriting the border or item content

### Requirement: Quiet deferred work and blocking priority

The board SHALL order unresolved blocking actions first, existing decisions/FYIs and ready nonblocking actions second, and pending items plus all board todos last, using ascending id within each band. Existing decisions/FYIs SHALL keep their relative order. Pending-trigger items and every board todo SHALL remain listable and visible unless human-deferred with their trigger/readiness but SHALL NOT contribute to human-attention unread counts, mailbox badges, automatic board landing or needs-input jumps. Coordinator recovery SHALL include their ids, kinds and readiness. Coordinators SHALL verify triggers explicitly; the system SHALL NOT infer readiness from prose or automatically schedule work. Ready actions SHALL retain ordinary unread behavior.

#### Scenario: Blocking beats freshness and read state

- **WHEN** a read blocking action and a newer unread decision exist
- **THEN** the blocking action is displayed first while the decision retains its existing unread behavior

#### Scenario: Quiet future work

- **WHEN** the only unread items are pending actions and board todos
- **THEN** all items stay visible but the coordinator has no human-attention unread badge or board-only jump target

#### Scenario: Human step becomes urgent

- **WHEN** a coordinator verifies a later action's trigger and updates it to trigger_pending false and blocking true on an item that is not human-deferred
- **THEN** the material edit is unread, contributes to attention and sorts first

### Requirement: Action and board todo controls

The TUI and SPA board SHALL label all four kinds and show action blocking state, trigger/readiness and exact copyable step. The TUI SHALL provide a rebindable universal clear action (hera_board.dismiss, default x): dismiss FYIs and decisions, resolve actions and board todos as done, and retain the resident approval review-and-deny flow. No separate hera_board.done action SHALL exist. The default d binding SHALL defer or restore the selected item and D SHALL show or hide deferred entries. Dropping a board todo SHALL remain available on the web Decide page. A copy action SHALL copy the exact command/step. Optional completion notes SHALL be available in the browser completion UI. Keymap help and Reference documentation SHALL describe the new controls. Coordinator orientation, board tools and builtin Hera/remind-me guidance SHALL distinguish human steps, future team work, choices and FYIs, including explicit trigger updates and the Things naming distinction. New operations SHALL log success, failure and skipped/stale work without command contents.

#### Scenario: One-key done

- **WHEN** Aaron presses x on the selected action or board todo
- **THEN** the exact visible revision resolves done without requiring a note and the coordinator receives its durable completion notice

#### Scenario: Copy human step

- **WHEN** Aaron invokes Copy on an action's command
- **THEN** the exact stored text is copied without execution

#### Scenario: Guidance classifies reminders

- **WHEN** a coordinator captures future team work or a later command Aaron must run
- **THEN** guidance selects board todo for the former and later action for the latter, with typed trigger data

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
