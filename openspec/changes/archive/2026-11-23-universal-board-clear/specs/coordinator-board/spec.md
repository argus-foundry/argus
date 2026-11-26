## MODIFIED Requirements

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
