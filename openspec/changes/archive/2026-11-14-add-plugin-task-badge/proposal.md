## Why

Plugins (e.g. iris, which stamps a dogfood-lease summary into task metadata) need to mark a task visibly in the TUI without argus knowing anything about that plugin. Plugins can already write `task_meta`, but nothing renders it.

## What Changes

- A `task_meta` row with key `badge` in any namespace, whose value is a JSON object `{"icon","tone","expires_at"}`, renders as one tone-tinted single-cell glyph next to the task in the TUI task list (after the PR cell) and in the Hera rail (right-aligned tag slot, next to PR; the name truncates first).
- Icon-only: no label. Invalid or expired badges are ignored. One badge per task; the alphabetically first namespace with a valid badge wins.
- New `db.ListMetaByKey` read on the existing tick; no new endpoint or polling.

## Capabilities

### New Capabilities

- `plugin-task-badge`: the generic plugin badge contract and its TUI rendering.

### Modified Capabilities

(none)

## Impact

- `internal/tui/badge` (parser), `internal/db/task_meta.go`, `internal/tui/taskview/tasklist.go`, `internal/tui/hera/{rail,page,asyncrefresh}.go`, `internal/tui/app.go`, `docs/plugins.md`.

## Non-Goals

- **Frontend parity exception (approved by the user, who is the only user):** the web SPA and macOS app ignore badges. Named follow-ups if wanted.
- The remote (`--remote`) TUI shows no badges: the REST task DTO does not carry arbitrary meta and no endpoint is added.
- The jump switcher's private task list and rail do not draw badges yet.
