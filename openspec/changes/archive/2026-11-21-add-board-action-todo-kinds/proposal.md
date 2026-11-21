## Why

Human-only steps and future team work currently masquerade as decisions or FYIs. Aaron's review of 632 board items identified about 80 human actions and 17 future team tasks that need their own completion and attention behavior.

## What changes

- Add coordinator-board kinds `action` (Aaron performs a step) and `todo` (the team performs future work), preserving existing decision and FYI behavior.
- Persist an action's blocking flag, optional trigger and copyable step, plus explicit trigger readiness for deferred work. Blocking actions lead the board; pending triggers do not demand attention.
- Add durable Done for actions and Done/Dropped for board todos across MCP, REST, the TUI, SPA board, shared Decide rail and standalone `/decide`.
- Update coordinator orientation, builtin Hera guidance and `/hera-remind-me` capture rules.
- Widen the SQLite kind CHECK without reclassifying existing rows; retain dependent records and stable IDs.
- Keep the board kind `todo` distinct from the Things 3 `internal/todo` and `todo_*` backend.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `coordinator-board`: new kinds, typed metadata, attention/order, terminal outcomes and coordinator guidance.
- `board-decision-page`: expose and complete the new kinds in the shared web response surfaces.
- `hera-auto-archive`: prevent archiving from stranding open actions or board todos.

## Impact

Touches SQLite migration/store, Hera summaries and attention counts, MCP schemas/handlers, REST mutation and Decide receipts, TUI board/keymap/help, static web assets/service-worker version, skills and Reference docs. macOS currently has no board view; adding a native board is a named parity follow-up. No new dependencies, automatic trigger detector, Things sync or historical reclassification.
