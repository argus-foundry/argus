## Why

REST clients (web workspace, future Mac app) cannot start an orchestration team: `POST /api/tasks` creates an ordinary task with no coordinator binding. The daemon already has a transactional primitive, `SpawnHeraCoordinator`; it only needs a REST contract, a per-spawn profile input and retry-safe idempotency.

## What Changes

- `GET /api/hera/coordinator-options?project=<name>` returns the valid branch, profile and archetype choices plus defaults.
- `POST /api/hera/orchestrators` creates a bound coordinator (orchestrator, role, binding, task) through `agent.SpawnHeraCoordinator`, with a persisted `request_id` receipt for idempotent retries.
- `HeraCoordinatorSpawnInput` gains `Profile`, passed to `CreateAndStart`.
- New SSE event `hera.orchestrator_created`.
- New table `hera_coordinator_requests`.

## Impact

- Affected specs: rest-api, hera-coordination, diligence-profiles.
- Affected code: `internal/api`, `internal/agent/hera_spawn.go`, `internal/db`, `internal/gitutil`, `internal/model/event.go`.

## Non-Goals

- Web adoption of the route: ENG-1097 (Web: Start a coordinator with branch and profile choices).
- TUI profile/archetype choice propagation: ENG-1119 (TUI: Preserve coordinator profile and archetype choices). The TUI form is untouched here.
- macOS native coordinator creation: named parity follow-up (macOS Hera tab remains read-only).
- Caller-supplied command templates or worktree paths: never accepted.
