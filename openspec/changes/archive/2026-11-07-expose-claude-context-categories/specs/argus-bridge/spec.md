## ADDED Requirements

### Requirement: Measurements carry optional context categories
`session.measure` context SHALL optionally carry non-negative integer token counts for `messages`, `memory`, `tools`, `agents`, `skills`, `system` and `compaction`, taken from the engine's context breakdown. A category the engine did not report SHALL be absent, never zero-filled. Breakdown read failures SHALL send the measurement without categories. The daemon SHALL reject a measurement with a negative or nonfinite value with 400, apply no event of that batch, and keep the previous snapshot. A new session id or launch generation SHALL clear stored categories.

#### Scenario: Missing versus zero
- **WHEN** a measurement reports `memory: 0` and omits `agents`
- **THEN** the stored snapshot holds memory 0 and agents unknown

#### Scenario: Invalid measurement
- **WHEN** a measurement carries a negative category
- **THEN** the response is 400 and the previous snapshot is unchanged
