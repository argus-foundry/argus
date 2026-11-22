## ADDED Requirements

### Requirement: Coordinator board kind tabs

The TUI coordinator board SHALL show a single-row Decisions, FYIs, Actions, Todos, All tab strip, in that order. Counts SHALL include all open items of each kind on that coordinator's board, regardless of read or deferred state; All SHALL sum the counts. Nonzero counts SHALL use space-padded colored backgrounds (blue, teal, orange/brown, slate; All blue), zero counts SHALL be dim with no fill, and the selected label SHALL use the accent style. Rendering SHALL stay within the panel, abbreviating labels and clipping around the selected tab when necessary.

#### Scenario: Mixed board

- **WHEN** a board contains four decisions, three FYIs, two actions and six todos
- **THEN** the tabs show counts 4, 3, 2, 6 and 15 and filtering does not change those totals

#### Scenario: Navigation and selection

- **WHEN** the focused board receives the rebindable previous/next tab action (default Left/Right)
- **THEN** the selected tab moves with clamped endpoints, logs a `[board] tab` change and filters all navigation, expansion, scrolling, reply, dismiss, complete and copy actions to visible items
- **AND** the selected item is retained if visible, otherwise the first visible item is selected

#### Scenario: Initial and remembered selection

- **WHEN** a coordinator is shown for the first time
- **THEN** Decisions is selected if nonempty, otherwise the first nonempty kind, otherwise All
- **AND** subsequent visits restore the session's chosen tab for that coordinator without persistence

#### Scenario: Pane traversal

- **WHEN** modified arrows are used on the board
- **THEN** the existing pane focus ladder still operates

#### Scenario: Explicit attention jump

- **WHEN** ctrl+g targets a board whose current kind has no attention item
- **THEN** the board switches to All and selects its first attention item

#### Scenario: Narrow and empty board

- **WHEN** the panel is narrow or counts are zero
- **THEN** the strip stays inside its rectangle, the selected tab stays represented when space permits, and empty counts have no colored fill
