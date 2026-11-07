## Why

REST clients cannot invoke the TUI's worker bounce or forced coordinator recycle; generic stop and resume do not carry their lifecycle semantics. The web workspace needs both, with a durable receipt it can poll.

## What changes

- A shared `hera.LifecycleService` implements worker/freelance bounce (one system instruction, no kill, completes when the role's own self-service recycle is observed) and coordinator forced recycle. The TUI rail actions and the REST routes both call it.
- New routes: `POST /api/hera/roles/{id}/bounce`, `POST /api/hera/roles/{id}/recycle` (`force:true`), `GET /api/hera/roles/{id}/lifecycle-operations/{operation_id}`.
- A generic `operations` table stores receipts keyed by client `request_id`, so retries are idempotent and one active operation per role is enforced atomically.
- New SSE event `hera.lifecycle_changed` after each persisted transition.
- `RecycleWatcher` gains an outcome hook so a bounce advances when the role's self-service recycle restarts or fails.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `rest-api`: bounce, recycle and operation-read routes.
- `events-bus`: `hera.lifecycle_changed`.
- `coordinator-context-management`: shared lifecycle service semantics.

## Impact

- **Code:** `internal/hera/lifecycle.go`, `internal/hera/recycle_watcher.go`, `internal/db/operations.go` + `schema.go`, `internal/api/hera_lifecycle.go`, `internal/daemon/lifecycle.go` + wiring, `internal/tui/heraactions.go`.
- **Unchanged:** supervisor wire surface (no `ProtocolVersion` or digest change), `ForceRecycleCoordinator` RPC, MCP tools.
- **Schema:** additive `CREATE TABLE IF NOT EXISTS operations`; no schema version bump.

## Non-Goals

- No new Web or TUI controls. Web adoption is the follow-up ENG-1102 (Web: Add worker bounce and coordinator recycle actions); the macOS app consumes the REST contract later.
- No timeout kill for a bounce that never receives a handoff.
- The generic table is shaped so later receipt-style operations (ENG-1048, ENG-1066) can reuse it; they are not implemented here.
