# Claude worktree trust

## Purpose

Prepare each new Claude worktree with account-specific folder trust before session launch, while preserving existing configuration and allowing task creation to continue on failure.
## Requirements
### Requirement: Trust new Claude worktrees before launch

Argus SHALL seed hasTrustDialogAccepted as true in the canonical absolute worktree project entry before starting a newly created Claude-family task. All creation callers SHALL use the shared creation path. Trust SHALL default on and `[agents] trust_worktrees = false` SHALL disable it. Other backend families SHALL not write Claude trust.

#### Scenario: Shared creation callers

- **WHEN** a plain task, Hera worker, coordinator or sub-coordinator creates a Claude worktree
- **THEN** its trust entry exists before runner.Start

#### Scenario: Disabled or unrelated backend

- **WHEN** trust_worktrees is false or the task uses a non-Claude family
- **THEN** creation does not mutate Claude trust

### Requirement: Respect the launched account and preserve config

Argus SHALL use the task's effective Claude config location, including backend environment overrides, read it fresh, change only the target project's trust flag, and atomically replace the file while preserving all other JSON values. Missing files SHALL be created and malformed or structurally incompatible files SHALL remain untouched. Argus MUST acquire Claude's `<config>.lock` directory before the fresh read, hold and refresh its lease through publication, and wait no more than 500ms for acquisition. It MUST NOT steal another writer's lock, publish after detecting lease loss, or remove another writer's lock on release.

#### Scenario: Named account

- **WHEN** the task runs with an explicit CLAUDE_CONFIG_DIR
- **THEN** trust is written to that directory's .claude.json and the default config is untouched

#### Scenario: Existing data

- **WHEN** the config has unrelated projects, project fields or global fields
- **THEN** all values survive, including numbers beyond floating-point precision

#### Scenario: Trust failure

- **WHEN** reading, merging, canonicalizing or writing trust fails
- **THEN** Argus logs the error with a [trust] prefix and still creates and starts the task

#### Scenario: Concurrent Claude writer

- **WHEN** another writer holds the config lock and changes a login or setting before releasing it
- **THEN** Argus reads that updated value only after acquiring the lock and preserves it when adding trust

#### Scenario: Contention or ownership loss

- **WHEN** the lock remains held beyond the acquisition deadline or Argus detects its lease was replaced
- **THEN** Argus leaves the config and other writer's lock untouched, logs [trust], and continues creation

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

