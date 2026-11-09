## ADDED Requirements

### Requirement: Web Settings displays provider usage windows

The web Settings view SHALL display current cached usage for configured Claude accounts and the default Codex account. For Claude it SHALL display each available five-hour and weekly window; for Codex it SHALL display its available weekly window. Each available window SHALL show its usage percentage and, when known, its reset time. A valid reading with no known reset SHALL retain its percentage and identify the reset time as unavailable. Missing or expired windows SHALL be shown as unavailable, and the view SHALL expire readings at their reset time and refresh readings while Settings is open without initiating provider probes or sign-in lookups.

#### Scenario: Claude has both windows

- **WHEN** a configured Claude account has valid five-hour and weekly readings
- **THEN** Settings shows both percentages and reset times for that account

#### Scenario: Codex has a valid weekly reading

- **WHEN** the daemon has a valid cached Codex weekly reading
- **THEN** Settings shows its percentage and reset time when known

#### Scenario: Codex depleted hold has no reset timestamp

- **WHEN** a valid cached Codex depleted reading has no known reset time
- **THEN** Settings shows its percentage and identifies the reset time as unavailable

#### Scenario: Reading is missing or expired

- **WHEN** a provider window has no current valid cached reading
- **THEN** Settings identifies that window as unavailable instead of displaying stale data as current

#### Scenario: Settings refreshes usage

- **WHEN** Settings remains open while readings change
- **THEN** the displayed readings refresh using the existing cached snapshots and no extra provider probe is started
