## MODIFIED Requirements

### Requirement: A task can be moved to another account

Codex moves SHALL discover an empty saved conversation ID through account-scoped capture before transferring, or refuse uncertain discovery. They SHALL validate target thread storage before any missing-source fallback. Missing source row/rollout SHALL select a fresh process with no stale resume ID, on both stopped and live tasks. Source rollout reads SHALL be confined to regular files in source sessions storage. Destination path components SHALL refuse symlinks and writes SHALL remain rooted in the canonical target home. Physical home aliases SHALL be rejected. Failed target index writes SHALL preserve any previous destination rollout and row. Source credentials, rollout and row SHALL remain unchanged; non-allowlisted target metadata SHALL retain its own values. Canceled HTTP requests SHALL abandon queued task locks and perform no later mutation.

#### Scenario: Failed kick keeps the target
- **WHEN** a kick returns an error even though the source handle looks alive
- **THEN** the target stays persisted, restart_unknown is returned, and no second launch is attempted

#### Scenario: First live Codex move
- **WHEN** a live task has an empty saved ID and its account index identifies its thread
- **THEN** that thread is copied and its captured ID is persisted with the target account

#### Scenario: Unready target with missing source data
- **WHEN** the source ID, row or rollout is missing and the target index is missing or unusable
- **THEN** the move is rejected without account or session changes

#### Scenario: Missing conversation starts fresh
- **WHEN** a ready target accepts a task whose saved conversation is absent
- **THEN** stopped and live launch paths start fresh without resuming the missing ID

#### Scenario: Unsafe paths or physical account aliases
- **WHEN** the source rollout escapes sessions storage, a destination component is a symlink, or distinct account names share physical home identity
- **THEN** the transfer is refused and source storage remains unchanged

#### Scenario: SQLite write rejects the transfer
- **WHEN** a real target insert or update fails, including a trigger rejecting after a row change
- **THEN** the previous target row and rollout are restored, or no new row or rollout remains

#### Scenario: Canceled move waiting for its task lock
- **WHEN** an HTTP move is canceled before obtaining the task lock
- **THEN** it returns without waiting for the previous operation to finish and performs no later mutation
