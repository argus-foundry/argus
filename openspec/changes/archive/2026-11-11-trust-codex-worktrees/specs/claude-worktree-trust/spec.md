## MODIFIED Requirements

### Requirement: Trust new Claude worktrees before launch

Argus SHALL seed hasTrustDialogAccepted as true in the canonical absolute worktree project entry before starting a newly created Claude-family task. All creation callers SHALL use the shared creation path. Trust SHALL default on and `[agents] trust_worktrees = false` SHALL disable it. Other backend families SHALL not write Claude trust.

#### Scenario: Shared creation callers

- **WHEN** a plain task, Hera worker, coordinator or sub-coordinator creates a Claude worktree
- **THEN** its trust entry exists before runner.Start

#### Scenario: Disabled or unrelated backend

- **WHEN** trust_worktrees is false or the task uses a non-Claude family
- **THEN** creation does not mutate Claude trust
