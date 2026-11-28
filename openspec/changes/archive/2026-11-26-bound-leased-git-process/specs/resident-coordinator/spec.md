## ADDED Requirements

### Requirement: Bound leased Git subprocesses

A leased remote push and its optional origin probe SHALL end within their command budget even if a transport child inherits the command's output pipes. The repository ownership lock SHALL be released when those commands time out.

#### Scenario: A transport child keeps the pipe open
- **WHEN** the leased delete reaches its deadline while a transport child still holds a Git output pipe
- **THEN** the command stops waiting and repository ownership becomes available again.

#### Scenario: An earlier stack plan is stale
- **WHEN** a stacked task disappears, changes branches, or a different task takes its branch before cleanup runs
- **THEN** the branch is left alone. Cleanup verifies the current task and repository under Git ownership before deleting the reference.

#### Scenario: Git refuses to remove a linked worktree
- **WHEN** a managed worktree is locked or is not registered with the verified repository
- **THEN** cleanup keeps the directory and reports pending local cleanup. A known live linked checkout identifies its own repository when configuration points elsewhere.
