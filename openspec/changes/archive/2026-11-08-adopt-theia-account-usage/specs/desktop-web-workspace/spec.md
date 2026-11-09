## ADDED Requirements

### Requirement: Claude account usage in settings
Agents settings SHALL use the existing authenticated accounts array to display each account's valid five-hour and weekly Claude usage with reset time. Missing, malformed or expired readings SHALL remain unavailable, never inferred as zero used. Usage SHALL be labeled Claude and SHALL NOT describe Codex quota. Refresh SHALL remain read-only and leave account editing to its named future CRUD scope.

#### Scenario: Mixed account readings
- **WHEN** one account has valid usage and another has expired or absent usage
- **THEN** only the matching account shows the valid usage windows and reset times, with unavailable readings labeled clearly
