## Why

A worker that reports `done` keeps its `meta:hera.ready_to_close` mark when it later reports `working`. Once it rests again, the roster shows unfinished work as ready to close. The TUI's manual status step already clears the mark; the shared MCP status path did not.

## What changes

- `applyRoleStatus` (shared by `hera_status` and `hera_send`) clears the bound task's `ready_to_close` mark when a worker-kind role applies `working`, before the role status is written.
- A failed clear returns the existing MCP error and leaves the role status unchanged; `hera_send` soft-fails the status apply and still delivers the message.
- Coordinator and freelance status changes never touch the mark. The task status is not changed.

## Capabilities

### Modified capabilities

- `hera-coordination`: worker `working` clears readiness.

## Non-Goals

- No new route, payload or event; the roster already projects the persisted marker. No web, TUI or macOS change.

## Impact

- `internal/mcp/hera.go`; tests in `internal/mcp` and `internal/api`.
