## ADDED Requirements

### Requirement: Dead-session replay carries a stopped marker

A pane bound to a task that replays a dead or absent session's log SHALL, unless its bound task is complete, show a right-aligned "stopped" marker on its top border (the slot shared with the re-render indicator, which takes precedence; the marker is dropped when it would overlap the title), never covering replay rows. The closed-out banner and launch splash keep precedence over the replay entirely. A live session SHALL never show it.

#### Scenario: Replay of a dead session

- **WHEN** a pane has a task ID, no live session, and replay content
- **THEN** the top border shows the stopped marker and no replay row is covered

#### Scenario: Live session

- **WHEN** the pane's session is alive
- **THEN** no stopped banner is drawn
