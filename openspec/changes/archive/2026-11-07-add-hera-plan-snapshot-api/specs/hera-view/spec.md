## ADDED Requirements

### Requirement: Shared tview-free plan projection

The plan pane's node set, state rules, edge filtering and authored test SHALL be implemented in `internal/hera/plan`, used by both the TUI and `GET /api/hera/orchestrators/{id}/plan`. The TUI SHALL keep its render-only concerns (icons, task-keyed node ids) locally and its rendered output SHALL be unchanged.

#### Scenario: TUI render unchanged
- **WHEN** the plan pane renders after the extraction
- **THEN** node ids, states, descriptions and edges match the previous projection
