## MODIFIED Requirements

### Requirement: Rail and terminal focus routing

Rail focus SHALL retain Tasks mutations and substring-filter behavior. Enter on a task SHALL enter its terminal; Tab from the rail SHALL enter an already-bound visible terminal without starting it. Terminal Tab and Shift+Tab SHALL reach the agent. Ctrl+Q SHALL return focus to the rail without hiding the task pane or stopping its session. Modified Left/Right SHALL traverse the rail and visible content regions in the same direction as Projects, skipping hidden panels. Existing modified Up/Down task navigation SHALL select only tasks admitted by Tasks filters. Cross-tab arrows, when enabled, SHALL continue from the rightmost content region into Projects and return from Projects to Tasks content. The rail row for the task shown in the content pane SHALL render in the selected color whether the rail or the terminal holds focus; only the rail border reflects which region is focused.

#### Scenario: Terminal autocomplete remains usable

- **WHEN** the terminal is focused and Tab or Shift+Tab is pressed
- **THEN** the input is forwarded to the task's PTY rather than moving focus

#### Scenario: Return to rail

- **WHEN** Ctrl+Q is pressed in the selected terminal
- **THEN** focus moves to the rail, its task pane remains visible, and ordinary navigation changes the previewed task

#### Scenario: Zoom keeps rail accessible

- **WHEN** Ctrl+Z hides the right area's git/file panels and modified Left is pressed in the terminal
- **THEN** the rail is still visible and gains focus without focusing a zero-width panel

#### Scenario: Mouse and paste target the intended region

- **WHEN** the user clicks a non-interactive content panel, clicks the rail, or pastes into a focused terminal
- **THEN** focus remains on a visible interactive region, the rail click focuses the rail, and terminal paste reaches only the bound task

#### Scenario: Selected row stays highlighted while the agent is focused

- **WHEN** Enter moves focus from the rail to the selected task's terminal
- **THEN** that task's rail row still renders in the selected color while the rail border shows unfocused

