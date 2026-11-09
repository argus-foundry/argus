## ADDED Requirements

### Requirement: Hera plan snapshot endpoint

`GET /api/hera/orchestrators/{id}/plan` SHALL return `{orchestrator_id, authored, nodes, edges}`. Nodes are identified `role:<id>` (stable across materialization) with `role_id`, nullable `task_id`, `name`, `state` (planned|pending|in_progress|in_review|done|failed|cancelled), `planned`, `cancelled`, `prompt`, `archetype` and nullable `child_orchestrator_id`. Edges are `{blocker_id, dependent_id}`. Archived roles and coordinators SHALL be excluded, cancelled planned nodes kept, and only edges between present nodes returned. A roster with no planned node and no edge SHALL return `authored:false` with empty arrays. Malformed ids SHALL return 400, unauthenticated requests 401, a missing orchestrator 404, a graph over 2000 nodes or 5000 edges 413 (never truncated), and store errors 500. The endpoint SHALL NOT mutate state.

#### Scenario: Wire id survives materialization
- **WHEN** a planned node is bound to a task
- **THEN** its `id` is unchanged and `task_id` changes from null to the task id

#### Scenario: Oversized graph
- **WHEN** an orchestrator has more than 2000 plan nodes
- **THEN** the response is 413

### Requirement: Plan change event

The daemon SHALL emit `hera.plan_changed` with payload `{"orchestrator_id":N}` on `/api/events/stream` after any committed plan, node, block, binding, role-status or bound-task status change, from every mutation path (MCP, gater, TUI). A failed mutation SHALL emit nothing, and emission SHALL happen outside DB locks.

#### Scenario: Block added
- **WHEN** a blocking edge is committed
- **THEN** one `hera.plan_changed` for that orchestrator is emitted after the commit

#### Scenario: Rejected edge
- **WHEN** an edge is rejected as a cycle
- **THEN** no event is emitted
