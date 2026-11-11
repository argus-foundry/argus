# Coordinator board specification

## Purpose

Keep coordinator-curated human decisions and FYIs durable, current and visible across session recycles, with safe TUI reply composition and a shared REST surface.
## Requirements
### Requirement: Durable coordinator-owned board

The system SHALL store stable numeric-id items owned by coordinator role, with decision/fyi kind, one-line subject, body, created/updated timestamps, read state, resolved state, resolver and optional resolution note. Only a coordinator binding SHALL authorize agent post/update/resolve/list tools. Material edits SHALL mark unread; identical edits SHALL preserve read state.

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

The TUI SHALL render collapsed entries with bold kind/id/subject headers, support j/k and arrows, Enter expansion marking read, r reply and x dismissal. Reply SHALL focus the coordinator and paste the subject/id followed by two newlines with user input origin, without submitting. Decision dismissal SHALL durably notify its coordinator; resolution SHALL record the human as resolver.

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

#### Scenario: Fresh coordinator

- **WHEN** a coordinator starts or reclaims its role
- **THEN** it receives the maintenance rule and open board item ids/subjects

### Requirement: Focus-aware board hint row

The TUI SHALL reserve the bottom interior row of every visible board for a single muted hint, including empty boards. Unfocused boards SHALL show `⌘→ open board · p plan`; focused boards SHALL show `j/k move · ⏎ expand · r reply · x dismiss · p plan · ctrl+q back` with default bindings. Rebindable labels SHALL come from the active keymap; structural focus, Enter and back labels SHALL reflect the actual fixed keys. Items SHALL scroll above the hint. Narrow panes SHALL clip the hint to available cells without wrapping, overflow or item overlap.

#### Scenario: Unfocused and empty board

- **WHEN** the board is visible without focus, including with no open items
- **THEN** the bottom interior row shows the muted open-board and plan hint

#### Scenario: Focused board with many items

- **WHEN** focus enters a board whose items exceed its height
- **THEN** the bottom interior row shows movement, expand, reply, dismiss, plan and Ctrl+Q back hints while the selected item scrolls above it

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

The system SHALL ship a builtin `hera-remind-me` skill that Aaron invokes to have the current agent put an item from the conversation on its coordinator's board. With argument text the agent SHALL capture what the text names; without it the agent SHALL capture the most recent actionable follow-up from the conversation. The item SHALL have a one-line subject and a body that states the action, its trigger condition, its rationale and any concrete references. Its kind SHALL be `fyi` unless Aaron must make a choice, in which case it SHALL be `decision`.

A coordinator SHALL list its open items first and update an existing item that already covers the same follow-up instead of posting a duplicate. A worker or freelance agent SHALL NOT call board tools; it SHALL relay the item to its coordinator with `hera_send` and ask the coordinator to post it. An agent with no Hera binding SHALL report that no board exists in this session and SHALL NOT post or send anything. The skill SHALL NOT change board ownership or authorization.

#### Scenario: Coordinator captures a release follow-up

- **WHEN** a coordinator has just said "when we release, bump the service worker version" and Aaron runs `/hera-remind-me`
- **THEN** a new open `fyi` item exists on that coordinator's board whose subject names the follow-up and whose body includes the release trigger and the action

#### Scenario: Existing item covers the follow-up

- **WHEN** the coordinator's board already has an open item for the same follow-up and Aaron runs `/hera-remind-me` for it
- **THEN** the existing item is updated with the newer information and no second item is posted

#### Scenario: Worker relays to its coordinator

- **WHEN** Aaron runs `/hera-remind-me` in a worker session
- **THEN** the worker sends its coordinator a Hera message containing the proposed subject, kind and body with a request to post it, and makes no board tool call

#### Scenario: Plain task has no board

- **WHEN** Aaron runs `/hera-remind-me` in a task with no Hera binding
- **THEN** the agent says there is no board for this session and nothing is posted or sent

