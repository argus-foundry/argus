## ADDED Requirements

### Requirement: Per-spawn profile on coordinator spawn

The coordinator spawn input SHALL carry an optional profile override stored on the task (`task.Profile`), with an empty value inheriting the project binding, alongside the existing archetype and base branch.

#### Scenario: Explicit profile reaches model resolution
- **WHEN** a coordinator is spawned with profile `lean`
- **THEN** the task stores `lean` and model resolution uses that profile's archetype model
