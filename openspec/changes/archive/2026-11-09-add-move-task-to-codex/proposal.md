## Why

When every Claude account is out of budget, a stuck task has nowhere to go. Aaron wants to move it to Codex and keep the work going. A Claude conversation cannot be resumed by Codex (the transcript formats differ), so this is an explicit, honestly labelled restart in place on Codex, seeded with a handoff. It never claims the conversation came along.

## What Changes

- New `agent.MoveTaskBackend`, reached through the existing account-move entry points by passing a `backend`: persists the target `Backend` and a Codex-capable `Account`, clears `Model` and `SessionID`, and restarts in place through the runner's recycle path with a seed prompt.
- The seed (`internal/lastexchange` generalized) states that the task moved from one CLI to another and the prior conversation did NOT carry over, then gives the worktree, branch, `git status`, the last human exchange when a Claude transcript is readable, and the original task prompt. Everything fails open.
- Moving back (Codex to Claude) uses the same primitive; the seed omits the exchange (no Codex transcript reader).
- REST `POST /api/tasks/{id}/account` and MCP `task_move_account` take an optional `backend`; results add `from_backend`, `to_backend`, `handoff`, `conversation_carried` (always false).
- TUI `>` picker lists handoff rows for the other CLI, labelled as a restart with handoff, with Codex usage where a reading exists.
- Documented losses on Codex: bridge mod and context indicator, accurate needs-input detection (scraper fallback), `hera_new_orchestrator`.

## Impact

Capability `account-move`. Hera bindings key on the task id and are untouched. Web and macOS have no move UI today (named parity gap, unchanged).
