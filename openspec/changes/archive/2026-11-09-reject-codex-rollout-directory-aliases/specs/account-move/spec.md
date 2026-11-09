## MODIFIED Requirements

### Requirement: A task can be moved to another account

The source sessions anchor and every component beneath it SHALL be non-symlink entries. Opened directories and the source rollout SHALL match the identities checked before opening. A source sessions symlink to the home itself, an inside sibling or an outside directory SHALL be rejected with the account error, preserving account settings, source storage and target storage. Destination publication SHALL remain bound to a checked real parent directory and reject symlinked sessions anchors too. Account-home aliases remain supported when they identify distinct physical homes.

#### Scenario: Source sessions aliases credentials
- **WHEN** sessions points to . and the indexed rollout is sessions/auth.json
- **THEN** the move is rejected without copying credentials or changing either index or the selected account
