# Support account moves for Codex tasks

## Why

A Codex task can be created on a named account but can never be moved to another compatible account, because the move operation was Claude-only. Moving a Codex task needs its own session transfer, since Codex keeps conversations in a per-account thread index and rollout files rather than in Claude's `projects/` tree.

## What Changes

- A backend family declares how its saved conversation moves between accounts (`Family.TransferSession`). Claude wraps the existing transcript copy; Codex copies the thread's rollout file and its row in the target account's `state_5.sqlite`.
- `POST /api/tasks/{id}/account` works for any family that declares a transfer, serializes on the per-task lock, refuses a closed-out Hera task like `/restart`, and reports a failed restart through the shared partial-outcome response.
- `GET /api/backends` adds `account_move {supported, preserves_conversation}`.
- New `task.account_changed` event `{from,to,resumed,started}`.

## Impact

`internal/agent` (registry field, `codexmove.go`), `internal/api/accountmove.go`, `internal/model/event.go`, `/api/backends` DTO. No schema change.

## Non-Goals

Web and TUI consumption: Web follow-up is ENG-1082 and TUI follow-up is ENG-1093 (the TUI account-move picker still filters to Claude). No Mac UI work.
