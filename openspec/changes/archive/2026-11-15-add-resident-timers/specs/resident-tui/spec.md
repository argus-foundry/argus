## ADDED Requirements

### Requirement: Solo timer summary

The Solo tab SHALL display active timer count and next due time from daemon state without blocking the UI thread. Web and macOS timer views are named follow-ups `add-web-resident-timers` and `add-mac-resident-timers`.

#### Scenario: Pending timers

- **WHEN** Solo has active timers
- **THEN** its TUI pane shows their count and earliest due time
