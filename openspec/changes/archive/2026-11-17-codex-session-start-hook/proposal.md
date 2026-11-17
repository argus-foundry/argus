## Why

Codex mints its own session ID and Argus only recorded it after a clean exit. A supervisor restart kills the session as a lost stream, which skips that capture, so the task's session ID stayed empty and the next start relaunched with the original prompt, losing the conversation.

## What Changes

- Resume paths (REST resume, TUI start, revive, bounce recovery) now fill an empty Codex/Pi/opencode session ID from the backend's own state before deciding whether to resume.
- Fresh Codex launches register an inline `SessionStart` hook (`argus codex-session-hook`) that reports the thread ID to the daemon at launch (`POST /api/tasks/{id}/session-id`, fill-only, Codex-only).
- The hook needs `--dangerously-bypass-hook-trust`: Codex skips a hook with no recorded trusted hash, and the hash cannot be written without editing the user's symlinked config. Argus already launches Codex with `--dangerously-bypass-approvals-and-sandbox`.
- Supervisor spawn surface bumped to 54.

## Capabilities

### New Capabilities
- `codex-session-capture`: how a Codex task's session ID is learned and recovered.
