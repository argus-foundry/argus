## MODIFIED Requirements

### Requirement: Coordinator board kind tabs

The TUI coordinator board SHALL show a single-row Decisions, FYIs, Actions, Todos, All tab strip, in that order, separated by ` · `. Counts SHALL include all open items of each kind on that coordinator's board, regardless of read or deferred state; All SHALL sum the counts. Every label SHALL be followed by one plain space before its count or count box. The selected tab SHALL be a single filled block containing one leading space, label, space, count and one trailing space, using its kind's existing color (decisions blue, FYIs teal, actions orange/brown, todos slate, All blue) and contrasting dark text, including when its count is zero. Unselected labels SHALL use normal/dim text followed by one plain unfilled space; nonzero counts SHALL retain a symmetric colored count box with one padding cell on each side, while zero counts SHALL remain dim with no fill or extra trailing padding. Rendering SHALL stay within the panel, abbreviating labels and clipping around the selected tab when necessary; the selected tab SHALL never be clipped entirely out of view at positive widths. Visible strips SHALL NOT begin or end with an orphan separator.

#### Scenario: Mixed board

- **WHEN** a board contains four decisions, three FYIs, two actions and six todos
- **THEN** the tabs show counts 4, 3, 2, 6 and 15 and filtering does not change those totals

#### Scenario: Navigation and selection

- **WHEN** the focused board receives the rebindable previous/next tab action (default Left/Right)
- **THEN** the selected tab moves with clamped endpoints, logs a `[board] tab` change and filters all navigation, expansion, scrolling, reply, clear and copy actions (clear dismisses FYIs/decisions, completes actions/todos as done, and retains resident-call denial) to visible items
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
- **THEN** the strip stays inside its rectangle and the selected tab stays represented at every positive width
- **AND** unselected empty counts have no colored fill, while the selected tab retains its filled block

#### Scenario: Selected filled tab in either terminal theme

- **WHEN** any tab is selected on a light or dark terminal background
- **THEN** its padding, label, separating space and count share one kind-colored fill and contrasting dark ink

#### Scenario: Unselected label and count spacing

- **WHEN** an unselected tab is rendered
- **THEN** its normal/dim label is followed by one plain unfilled space and its count or symmetric padded count box
- **AND** a nonzero count retains its kind-colored count box, while a zero count remains dim without a box or extra trailing padding

#### Scenario: Clean truncated boundaries

- **WHEN** the strip cannot fit every abbreviated tab
- **THEN** whole neighboring tabs are omitted around the selected tab without leaving a leading or trailing separator
- **AND** only the selected tab itself is clipped when it cannot fit by itself
