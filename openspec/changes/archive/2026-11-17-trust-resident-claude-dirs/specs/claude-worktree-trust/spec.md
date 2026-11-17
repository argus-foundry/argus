## ADDED Requirements

### Requirement: Trust resident launch directories

On every Claude-family resident launch with trust_worktrees enabled, Argus SHALL seed hasTrustDialogAccepted as true, in the launched account's Claude config, for the canonical resident working directory, the builtin skills workspace and the resident user-skills `--add-dir` root when present, under the same lock and config-preservation rules as worktree trust. Failures SHALL be logged with a [trust] prefix and SHALL NOT block the launch.

#### Scenario: Resident launch on an account

- **WHEN** the resident launches on a Claude account
- **THEN** that account's config trusts the resident working directory and every directory passed to it with `--add-dir`

#### Scenario: Disabled

- **WHEN** trust_worktrees is false
- **THEN** the resident launch writes no trust entries

#### Scenario: Trust write fails

- **WHEN** the account config is malformed or locked
- **THEN** the config is left untouched, Argus logs [trust] and the resident command is still built
