## ADDED Requirements

### Requirement: Single header row with Tasks rail

When the Tasks rail is visible, the TUI SHALL render only the navigation tab header above the task content and SHALL omit the separate task-name chip row. The reclaimed row SHALL be available to the content, including terminal PTY geometry. Browsing, terminal focus, zoom, pending launch, and live preference transitions SHALL respect this layout. With the rail disabled, the classic agent task-name header SHALL remain unchanged.

#### Scenario: Rail terminal uses the reclaimed row

- **WHEN** the user browses or enters a task terminal with the Tasks rail enabled
- **THEN** the task content begins immediately beneath the navigation tabs with no separate task-name chip row

#### Scenario: Classic header survives live transitions

- **WHEN** the user disables the rail while viewing an agent and enables it again
- **THEN** the classic view shows its task-name header while the rail view shows only the navigation header, without restarting the session
