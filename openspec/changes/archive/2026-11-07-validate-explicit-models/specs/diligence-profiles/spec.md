## MODIFIED Requirements

### Requirement: Profile-aware model resolution

The system SHALL resolve a task's effective model with the precedence: a valid per-task model override; otherwise, the resolved profile's model for the task's selected backend and archetype; otherwise the selected backend's configured default; otherwise no injected model flag. Task creation SHALL reject an explicit model the resolved backend does not allow (see rest-api: Explicit task model is validated before creation); for an already-persisted task, an invalid explicit override or missing/invalid backend-specific profile entry SHALL fall through to the selected backend's configured default rather than reaching the backend CLI.

The profile consulted SHALL be the task's per-spawn override when present, otherwise its project binding, otherwise `default`; a task without an archetype SHALL not consult a profile.

#### Scenario: Backend-specific profile model applied

- **WHEN** a Codex task has no valid override, carries `code_slice`, and its valid profile declares `codex = "gpt-5-codex"`
- **THEN** the effective model is `gpt-5-codex`

#### Scenario: Unsupported persisted explicit override falls through

- **WHEN** an already-persisted Codex task has `Model = "opus"`
- **THEN** the system does not inject `opus` and instead uses the Codex backend default or no model flag

#### Scenario: Profile selection and no-archetype behavior remain unchanged

- **WHEN** a task has a per-spawn profile override
- **THEN** it takes precedence over the project binding

- **WHEN** a task carries no archetype
- **THEN** it does not consult a profile
