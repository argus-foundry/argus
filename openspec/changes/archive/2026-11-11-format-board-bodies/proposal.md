## Why

Coordinator board bodies often arrive as one unbroken paragraph, which is hard to scan. The renderer already honors newlines, bullets and bold, but coordinators are not told to use them, and nested bullets and numbered lists render flat.

## What Changes

- Tell coordinators how to format a board body (lead sentence, blank line, `- **Label:** detail` bullets, sub-bullets, option bullets plus a recommendation bullet) in the `hera_board_*` tool description, the builtin hera skill and the coordinator orientation.
- TUI board bodies render nested bullets (one level per two leading spaces) and numbered items (`1. `) with a hanging indent.

## Impact

- `internal/tui/hera/board.go`, `internal/mcp/hera_board.go`, `internal/agent/hera_spawn.go`, `internal/skills/builtin/hera/SKILL.md` (supervisor spawn digest re-recorded, no surface bump).
