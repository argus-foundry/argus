## ADDED Requirements

### Requirement: Host Finder reveal
The workspace SHALL offer worktree and file Finder actions explaining before activation that Finder opens on the daemon host. It SHALL POST {} or the exact relative path, disable actions after master-only 403 for the credential lifetime, and distinguish unsupported host, missing path/task, invalid path and launch failure without automatic retries.

#### Scenario: Adopt the merged contract
- **WHEN** Task and file menus send captured targets including whitespace; device-token rejection disables further reveal controls until reconnect.
- **THEN** the client follows the merged daemon contract, retaining captured-task cancellation and accessible feedback
