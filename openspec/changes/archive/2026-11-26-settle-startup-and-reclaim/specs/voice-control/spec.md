## ADDED Requirements

### Requirement: Startup terminal and reclaim handoffs

Startup reattachment SHALL use authoritative owner observations to settle pending managed launches. Terminal clients SHALL preserve process identity through reconnect. Rebasing an archived task into active use SHALL coordinate with Hera reclaim.

#### Scenario: A launch settles during startup attachment
- **WHEN** a daemon sees an owner-managed launch or a live task during startup and that process settles before the stream attaches
- **THEN** a retained matching terminal receipt is finalized without restoring that task as a live worker.

#### Scenario: Restore races Hera reclaim
- **WHEN** a task is being stopped or its worktree is being reclaimed
- **THEN** restore, pin, and new role binding cannot race that operation.

#### Scenario: Theia terminal resumes across a replacement
- **WHEN** the Theia terminal sees a different process ID or a stale snapshot
- **THEN** it refreshes terminal state and uses only the replacement process’s cursor.

#### Scenario: Remote TUI discovers a replacement stream
- **WHEN** a remote TUI reader receives a stale-session conflict or a rerendering exit
- **THEN** it listens for the replacement with a fresh cursor and the pane reattaches without a task exit callback.
