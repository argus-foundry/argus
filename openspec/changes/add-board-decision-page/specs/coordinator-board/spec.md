## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Focus-aware board hint row

The TUI SHALL reserve the bottom interior row of every visible board for a single muted hint, including empty boards. Unfocused boards SHALL show `⌘→ open board · p plan`; focused boards SHALL show `j/k move · ⏎ expand · r reply · x dismiss · o decide · p plan · ctrl+q back` with default bindings. Rebindable labels SHALL come from the active keymap; structural focus, Enter and back labels SHALL reflect the actual fixed keys. Items SHALL scroll above the hint. Narrow panes SHALL clip the hint to available cells without wrapping, overflow or item overlap.

#### Scenario: Unfocused and empty board

- **WHEN** the board is visible without focus, including with no open items
- **THEN** the bottom interior row shows the muted open-board and plan hint

#### Scenario: Focused board with many items

- **WHEN** focus enters a board whose items exceed its height
- **THEN** the bottom interior row shows movement, expand, reply, dismiss, decide, plan and Ctrl+Q back hints while the selected item scrolls above it

#### Scenario: Rebound action

- **WHEN** a board action is rebound in the active keymap
- **THEN** the hint uses the new binding for that action

#### Scenario: Narrow board

- **WHEN** the board is narrower than its hint text
- **THEN** the hint clips cleanly on its reserved row without overwriting the border or item content

### Requirement: Coordinator maintenance guidance

Coordinator orientation and builtin Hera guidance SHALL instruct agents to put human decisions and attention on the board, rewrite superseded items, and resolve settled items including completed follow-up after a human reply. The `hera_board_*` tool description, builtin skill and orientation SHALL also tell coordinators to format each body for scanning: one short lead sentence, a blank line, then one `- **Label:** detail` bullet per fact or option (two-space `  - ` sub-bullets, each decision option its own bullet plus a recommendation bullet, links on their own bullet), never a wall of prose.

The same guidance SHALL tell coordinators to post a decision as structured whenever it has distinct options: two to four options, always a recommendation with a reason, with the body stating only the situation and the options appearing only in `options`. It SHALL tell coordinators that Aaron presses `o` on the board to answer a round of decisions in the browser, that each answer arrives as one summarized message in which the listed items are already resolved, that `Still open` items need follow-up, and that a "Something else" answer's note is the answer.

#### Scenario: Fresh coordinator

- **WHEN** a coordinator starts or reclaims its role
- **THEN** it receives the maintenance rule and open board item ids/subjects

#### Scenario: Decision with options

- **WHEN** a coordinator reads the board tool description, builtin skill or orientation
- **THEN** it is told to post a decision that has distinct options as a structured decision with a recommendation and reason

#### Scenario: Answers arrive

- **WHEN** a coordinator receives Aaron's summarized answers
- **THEN** its guidance tells it the listed items are already resolved and to follow up on any still-open items
