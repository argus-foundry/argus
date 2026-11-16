## Why

`hera_archive` only retires a role once its work has MERGED. In batch flows (e.g. Sketch) a worker is finished the moment its PR is open and green: the PR and the batch own the work from then on, and merging can take hours. Until then dozens of finished workers stay on the rail. Separately, archiving a hera-bound task through argus (`task_archive`, REST, TUI, web) never touches its hera role, so a task can be archived while its role stays live on the rail.

## What Changes

- **Handed-off archive.** `hera_archive` additionally archives a worker/freelance role whose PR is open and not failing CI, whose branch is pushed with local tip == remote tip, whose worktree is clean, and whose session is stopped (or gets stopped by the archive), provided every existing non-git check still passes. It is recorded as a distinct `handed_off` archive record, separate from merged done-done.
- **Watch after handoff.** The PR poller keeps polling handed-off archived tasks. On merge the record settles (and the task may complete). On a close without merge the role is un-archived and revived to `in_progress`, and an item is posted on the coordinator's board. A coordinator-callable `hera_unarchive` covers "the batch hands it back".
- **Forced archive.** `hera_archive(role_name, force=true, reason=...)` overrides the done-done/handoff checks, stores the reason durably in the archive record (shown in the role history and inbox viewer), and stays coordinator-only. Coordinators, pinned roles, resident roles, and standing-team members stay hard refusals under force.
- **Unread mail no longer blocks.** In the archive transaction the role's unread hera mail is marked read and one digest message (sender + first line per message) is forwarded to the coordinator's inbox. The outbox check stays.
- **Argus archive and hera archive agree.** Archiving a hera-bound task from argus (MCP `task_archive`, REST archive endpoint, TUI, web, macOS) runs the same hera verdict and archives the role in the same step, or refuses with the same reason. A DB-level backstop in `SetArchived`/`Update` refuses an archive of a task with a live worker/freelance binding, so no side door remains.
- Docs: hera skill, coordinator orientation, README `hera_archive` row (+ `hera_unarchive`), gotchas.

## Capabilities

### Modified Capabilities

- `hera-auto-archive`: done-done predicate (inbox clause), `hera_archive` tool (`force`, `reason`, handoff), atomicity (mail forward in the transaction); adds handed-off archive, handoff watch, forced archive, mail forwarding, task/role archive agreement.
- `hera-coordination`: tool inventory gains `hera_unarchive`; coordinator guidance covers handed-off and forced archive.

## Impact

- `internal/hera/archive.go` (modes, `HandoffCheck`), `internal/db/hera_archive.go` (+ new `hera_archive_records` table, mail forward, task-archive guard), `internal/daemon` (handoff check, poller eligibility + merge/close handling), `internal/gitutil` (CI rollup in the batch PR query), `internal/mcp/hera.go` + `server.go` (tool params, `hera_unarchive`, `task_archive` guard), `internal/api/handlers.go` (archive endpoint guard), `internal/tui` (refusal notice + force confirm), skills/orientation/README/gotchas.
- REST: the archive endpoint gains an optional `{"force":true,"reason":"..."}` body and a 409 with the refusal reason. See Frontend Parity in design.md.
