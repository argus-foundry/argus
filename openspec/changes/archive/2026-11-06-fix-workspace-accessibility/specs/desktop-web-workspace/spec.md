## ADDED Requirements

### Requirement: Accessible asynchronous workspace feedback
The optional workspace SHALL announce asynchronous load failures through alert or live semantics without moving focus. Replacing layout controls SHALL preserve focus on the equivalent control only when that control previously owned focus. Settings SHALL focus Back on initial entry and retain navigation control focus when changing sections.

#### Scenario: Async load fails
- **WHEN** a task tool, review or Settings asynchronous load fails
- **THEN** the error SHALL have alert or live semantics and focus SHALL remain in place

#### Scenario: Layout controls rerender
- **WHEN** a focused Changes tree/flat or Review split/unified control rerenders its content
- **THEN** focus SHALL remain on the equivalent layout control

#### Scenario: Settings section changes
- **WHEN** the focused compact selector or desktop navigation changes section
- **THEN** focus SHALL stay on that navigation control

## MODIFIED Requirements

### Requirement: Shared tooltips and compact rail footer

The optional workspace SHALL use one shared delegated tooltip bubble for data-tooltip elements, with a separate aria-label on icon-only controls and no native title on opted-in controls. Hover SHALL wait 400 ms, focus-visible SHALL show immediately, optional data-tooltip-shortcut SHALL render a dim key hint, and Escape, scroll and click SHALL hide it. The bubble SHALL accept pointer events, allow a 200 ms grace to cross the gap, and remain visible while its owner or bubble is hovered or its owner is focused. Leaving both hover and focus SHALL hide it after that grace. The bubble SHALL stay in the viewport, use role=tooltip and preserve existing aria-describedby values. Motion SHALL respect reduced-motion preferences and surfaces SHALL use theme tokens.

The rail footer SHALL contain one horizontal line with a live connection-state icon, keyboard-shortcuts icon and Settings gear. All icon-only rail controls and status indicators SHALL expose data-tooltip and aria-label. This requirement applies only to the optional workspace, with no REST or other frontend changes.

#### Scenario: Dynamic rail control tooltip
- **WHEN** a dynamically rendered rail icon is hovered or keyboard focused
- **THEN** one reusable bubble SHALL describe its action or current status
- **AND** any shortcut hint SHALL remain separate from its accessible name
- **AND** the footer SHALL remain one line at 1280 and 390 pixel viewport widths

#### Scenario: Nested tooltip owners
- **WHEN** the pointer leaves a nested count or status tooltip owner for its parent path owner
- **THEN** the parent tooltip SHALL appear after the hover delay and the nested owner's aria-describedby SHALL be removed
- **AND** entering the nested owner again SHALL replace the parent's tooltip
- **AND** movement between plain descendants of the same owner SHALL retain its visible tooltip

#### Scenario: Read a tooltip with the pointer
- **WHEN** the pointer crosses from its owner to the tooltip within the close grace
- **THEN** the tooltip SHALL persist while hovered
- **AND** owner focus SHALL retain the tooltip after the pointer leaves
- **AND** Escape SHALL dismiss it without moving focus
