## MODIFIED Requirements

### Requirement: Board hint and mailbox presentation

The Hera TUI SHALL render the coordinator-board focus-aware muted hint on a reserved bottom interior row, clipping at narrow widths and scrolling items above it, including when empty. Its rebindable labels SHALL track the active keymap. Mailbox markers on orchestrator headers and bridging sub-coordinator rows SHALL keep the envelope glyph and take their color from the most urgent open attention item, graded through the shared attention predicate: red for an open blocking action, orange for an open decision or a ready non-blocking action, blue when only FYIs are open. A coordinator whose open items are only todos and/or trigger-pending actions SHALL show the normal coordinator marker with its existing style. The color SHALL retain selected-row attributes. The next-needs-input jump hotkey SHALL continue to target only coordinators with an unread decision or FYI; an action, blocking or not, SHALL NOT by itself make a coordinator a jump target.

#### Scenario: Board focus changes

- **WHEN** focus moves from the rail or coordinator pane to the board
- **THEN** the hint changes from open-board and plan shortcuts to item actions, plan and the actual Ctrl+Q back shortcut

#### Scenario: Short and narrow board

- **WHEN** a board has little space or many items
- **THEN** its single muted hint clips within the reserved bottom row and items never paint over it

#### Scenario: Selected unread coordinator

- **WHEN** an unread coordinator header or bridging sub-coordinator row is selected
- **THEN** only its mailbox glyph takes the urgency color and its selected-row attributes remain intact

#### Scenario: Blocking action

- **WHEN** a coordinator has an open blocking action, with or without other items
- **THEN** its envelope is red

#### Scenario: Decision or ready action

- **WHEN** the most urgent open item is a decision or a ready non-blocking action
- **THEN** its envelope is orange

#### Scenario: FYI only

- **WHEN** the only open attention items are FYIs
- **THEN** its envelope is blue

#### Scenario: Quiet items only

- **WHEN** a coordinator's open items are only todos and/or trigger-pending actions
- **THEN** it shows no envelope and the normal coordinator marker

#### Scenario: Blocking action does not make a jump target

- **WHEN** a coordinator's only attention item is a blocking action
- **THEN** the next-needs-input jump does not select that coordinator
