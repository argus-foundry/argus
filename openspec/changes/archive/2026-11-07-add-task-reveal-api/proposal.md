## Why

ENG-1068: Web and the TUI cannot open a task's worktree or a changed file in Finder; users copy paths instead.

## What changes

- Add `POST /api/tasks/{id}/reveal`, a master-only, strictly contained Finder reveal on the daemon host with an injectable launcher.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `rest-api`
- `git-integration`

## Non-Goals

- ENG-1115 (Web reveal in Finder) and ENG-1123 (TUI reveal) are the named follow-ups for Web/TUI; macOS may consume the route later. No UI changes here.

## Impact

- **Code:** `internal/api`, plus daemon/skew wiring where noted in design.
- **Frontend parity:** API-only by design; see Non-Goals.
