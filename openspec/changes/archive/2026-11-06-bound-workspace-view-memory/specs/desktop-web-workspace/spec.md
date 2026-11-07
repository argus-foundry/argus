## MODIFIED Requirements

### Requirement: Retained task terminals

The optional workspace SHALL retain at most eight terminal controllers and their scrollback in least recently used order, protecting the selected and split-visible tasks. Task selection SHALL move a retained view without rebuilding its terminal. At the budget limit, the least recently used inactive controller SHALL be destroyed with its listeners, observers, input and output resources. A task disappearing from the snapshot SHALL also dispose its controller. Evicted tasks SHALL rebuild from a fresh bounded snapshot and attach at its byte cursor when revisited. Composer drafts SHALL remain scoped to their task and survive controller eviction. Hidden cached output streams SHALL be suspended to release browser connections; foreground resume SHALL append only unseen bytes from a cursor-fenced catchup snapshot, or replace with bounded replay when the process counter resets or history has been evicted. Pending input SHALL be canceled on task selection independently of the retained output stream. Global disconnect/pagehide SHALL dispose every controller and clear its Fit hint. The unified project grouping and coordinator rosters SHALL include hoisted freelance roles through their owning orchestrator ID.

#### Scenario: Return to a visited task

- **WHEN** the user selects another task and returns before its controller is evicted
- **THEN** the same terminal is shown with deduplicated cursor-based catchup, retaining scrollback when the replay window covers the saved cursor
- **AND** a send interrupted by the selection cannot submit its trailing Enter


#### Scenario: Visit more tasks than the terminal budget

- **WHEN** task navigation exceeds eight cached controllers
- **THEN** the least recently used inactive controller SHALL be destroyed
- **AND** selected and split-visible controllers SHALL remain alive

#### Scenario: Revisit an evicted task

- **WHEN** an evicted task is selected again
- **THEN** a fresh controller SHALL render the latest bounded snapshot exactly once and attach at its byte cursor
- **AND** existing input cancellation and per-task draft rules SHALL remain in force


### Requirement: Explicit split terminal view

The agent screen SHALL offer an explicit split toggle for any selected task, independent of rail grouping and coordinator selection. It SHALL default off and persist its enabled state per device through guarded localStorage operations. The left terminal SHALL remain the current task. The right pane SHALL offer a picker for any other task; a coordinator task MAY default to its selected live worker, while ordinary tasks SHALL start with an empty right pane. A compact collapsible coordinator roster SHALL be available in split mode without enabling split automatically.

Closing split SHALL return to the single view without disposing either retained controller. A task controller SHALL occupy only one slot at a time, and input/state callbacks SHALL remain scoped to the current slot. The shortcut help SHALL document Alt+Shift+D globally and Cmd+Shift+Enter in the terminal; the terminal-safe allowlist SHALL claim the latter without sending PTY bytes. No Hera mutation controls SHALL appear.

#### Scenario: Explicit split and retained secondary task

- **WHEN** split is enabled and a secondary task is chosen
- **THEN** the current task stays left and the selected other task appears right
- **AND** closing and reopening split reuses the retained secondary terminal unless intervening navigation evicted it under the terminal cache budget
- **AND** selecting a task never enables split implicitly


## ADDED Requirements

### Requirement: Review selection memory
The optional workspace SHALL save review path and layout metadata without retaining inactive diff bodies. Only the active rendered review SHALL keep the response text. Selecting another task or review SHALL release the old body while retaining metadata for a fresh read on return.

#### Scenario: Leave a diff review
- **WHEN** another task or review is selected
- **THEN** the old diff body SHALL be released while its path and layout mode remain available for a fresh read on return
