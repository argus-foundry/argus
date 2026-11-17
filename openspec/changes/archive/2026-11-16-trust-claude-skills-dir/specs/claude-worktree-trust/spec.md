## ADDED Requirements

### Requirement: Trust the builtin skills directory

When Argus seeds Claude trust for a new task, it SHALL also seed hasTrustDialogAccepted as true for the canonical absolute path of the Argus-managed builtin skills workspace that Claude launches receive through `--add-dir`, in the same account config and under the same lock, opt-out and fail-open rules as worktree trust. A failure to trust one directory SHALL NOT prevent trusting the other.

#### Scenario: New Claude task

- **WHEN** a Claude-family task is created with trust_worktrees enabled
- **THEN** both the worktree and the builtin skills workspace have trust entries in the task account's config before runner.Start

#### Scenario: Disabled or unrelated backend

- **WHEN** trust_worktrees is false or the task uses a non-Claude family
- **THEN** no skills trust entry is written

#### Scenario: Skills workspace unavailable

- **WHEN** the skills workspace cannot be materialized or resolves to no path
- **THEN** the worktree is still trusted and creation continues
