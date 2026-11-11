## ADDED Requirements

### Requirement: Dead-session replay carries a stopped marker

A pane bound to a task that replays a dead or absent session's log SHALL overlay a one-line banner "stopped — press Enter to resume" on its first row, reusing the existing replay reason-banner slot, unless the closed-out banner, launch splash, or an explicit placeholder note applies (those keep precedence). A live session SHALL never show it.

#### Scenario: Replay of a dead session

- **WHEN** a pane has a task ID, no live session, and replay content
- **THEN** the first row shows the stopped banner over the replay

#### Scenario: Live session

- **WHEN** the pane's session is alive
- **THEN** no stopped banner is drawn
