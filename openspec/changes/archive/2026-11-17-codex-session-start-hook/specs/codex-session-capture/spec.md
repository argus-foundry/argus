## ADDED Requirements

### Requirement: Record the Codex session ID at launch

A fresh Codex launch without a stored session ID SHALL register an inline `SessionStart` hook through `-c`, together with `--dangerously-bypass-hook-trust`, that reports the hook payload's `session_id` for `$ARGUS_TASK_ID` to the daemon, only for a startup SessionStart (a resume or clear start SHALL NOT be reported). The daemon SHALL accept the report only from the master token and store it only when the task is a Codex task, the ID is a UUID, and the task's session ID is empty. Resumed launches SHALL NOT register the hook. A hook or daemon failure SHALL NOT prevent the agent from starting.

#### Scenario: Fresh launch

- **WHEN** a Codex task starts without a session ID
- **THEN** its command carries the SessionStart hook flag before the prompt
- **AND** the task's session ID is stored when the hook reports it

#### Scenario: Repeated or invalid report

- **WHEN** a report arrives for a task that already has a session ID, is malformed, or targets a non-Codex task
- **THEN** the stored session ID is unchanged

### Requirement: Recover a missing session ID before resuming

Before a resume decision, Argus SHALL fill an empty session ID for capture-once backends (Codex, Pi, opencode) from the backend's own state when a conversation exists for the task's worktree, and SHALL leave it empty when none exists. It SHALL NOT replace an existing ID for these backends, SHALL adopt an ID recorded concurrently by the launch hook instead of overwriting it, and SHALL NOT fill during an account or backend move (a backend move clears the ID on purpose).

#### Scenario: Session killed by a supervisor restart

- **WHEN** a Codex session was killed without a clean exit and its session ID is empty but Codex has a thread for the worktree
- **THEN** resuming stores that thread's ID and resumes it instead of relaunching with the original prompt
