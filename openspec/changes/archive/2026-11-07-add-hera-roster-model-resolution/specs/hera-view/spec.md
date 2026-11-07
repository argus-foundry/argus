## ADDED Requirements

### Requirement: Shared tview-free tiering resolver

The rail's applied-model, effort, profile-warning and context-percent annotation SHALL be implemented in `internal/hera/tiering`, used by both the TUI and `GET /api/hera`. One resolver SHALL serve one build or snapshot and memoize profile loads for its lifetime only. The 40/65/90 indicator thresholds SHALL live in `tiering.ContextLevel`.

#### Scenario: One load per scope
- **WHEN** many roles share a project and profile in one build
- **THEN** the profile is loaded once
