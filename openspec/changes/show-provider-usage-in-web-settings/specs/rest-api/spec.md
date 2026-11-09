## ADDED Requirements

### Requirement: Cached provider usage is available to web Settings

The authenticated REST API SHALL expose the current valid cached Codex weekly usage reading needed by web Settings, including its percentage and reset timestamp. Reading this data SHALL NOT start a provider probe. Missing or expired readings SHALL be omitted or explicitly represented as unavailable.

#### Scenario: Codex snapshot is available

- **WHEN** web Settings requests usage and the daemon has a valid Codex snapshot
- **THEN** the response includes the usage percentage and reset timestamp

#### Scenario: No current Codex snapshot exists

- **WHEN** the cache is empty or its reading has expired
- **THEN** the response represents Codex usage as unavailable
