## ADDED Requirements

### Requirement: Cached provider usage is available to web Settings

The authenticated REST API SHALL expose current valid cached provider usage needed by web Settings: Claude five-hour and weekly windows for each configured Claude account, and the default Codex weekly reading. Each available window SHALL include its percentage and a reset timestamp when known. The endpoint SHALL read caches only; it SHALL NOT start provider probes or sign-in status lookups. Missing or expired windows SHALL be represented as unavailable.

#### Scenario: Codex snapshot is available

- **WHEN** web Settings requests usage and the daemon has a valid Codex snapshot
- **THEN** the response includes the usage percentage and reset timestamp when known

#### Scenario: Valid depleted Codex reading has no reset time

- **WHEN** a valid cached Codex depleted reading has no reset timestamp
- **THEN** the response includes its percentage and omits the reset timestamp

#### Scenario: No current Codex snapshot exists

- **WHEN** the cache is empty or its reading has expired
- **THEN** the response represents Codex usage as unavailable
