## ADDED Requirements

### Requirement: Trust Codex worktrees in the launched process

Argus SHALL add a process-scoped Codex config override marking the canonical absolute worktree path trusted on both fresh and resumed launches, using a backend family registry hook. It SHALL pass a single inline projects table as one `-c` argument and preserve the path as literal data. It MUST NOT edit the account config.toml or the shared CODEX_HOME overlay config to seed trust. `[agents] trust_worktrees = false` SHALL disable both Codex launch trust and Claude creation trust.

#### Scenario: Fresh or resumed session

- **WHEN** a Codex task starts or resumes with an existing worktree
- **THEN** its argv includes `-c` followed by a single inline projects table trusting that physical worktree path
- **AND** existing user project entries remain in the effective configuration
- **AND** the trust override is not persisted to the account config

#### Scenario: Ordinary Codex configuration change

- **WHEN** a session with injected trust saves an unrelated model setting
- **THEN** Codex saves the requested setting while retaining existing user project entries and omitting the injected trust entry

#### Scenario: Dots and shell syntax in paths

- **WHEN** a worktree path contains dots, quotes, spaces or shell metacharacters
- **THEN** its complete path remains one TOML key and no part is executed as shell syntax

#### Scenario: Opt-out or unrelated backend

- **WHEN** trust_worktrees is false or the family has no trust launch hook
- **THEN** Argus does not add a Codex trust override

#### Scenario: Path resolution fails

- **WHEN** Argus cannot canonicalize the worktree
- **THEN** it logs the error with a [trust] prefix and continues normal session launch without the override
