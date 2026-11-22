## ADDED Requirements

### Requirement: hera_new_orchestrator delivers the coordinator orientation

On success, `hera_new_orchestrator` SHALL include a "Coordinator orientation" section in its tool result containing the same orientation a recycle of the new coordinator role would carry: `HeraCoordinatorOrientation` for the new orchestrator when the caller holds no live worker binding in another orchestrator, and `HeraSubCoordinatorOrientation` (naming the new orchestrator and the caller's parent orchestrator) when it does. Rejected calls SHALL NOT include it.

#### Scenario: A plain task bootstrapping an orchestrator receives the full orientation

- **WHEN** a task with no hera binding calls hera_new_orchestrator(name=X) successfully
- **THEN** the tool result contains `HeraCoordinatorOrientation("X")`, including the context-discipline habits

#### Scenario: A self-promoting worker receives the sub-coordinator orientation

- **WHEN** a task holding a live worker binding in orchestrator P calls hera_new_orchestrator(name=B) successfully
- **THEN** the tool result contains `HeraSubCoordinatorOrientation` naming B and P, and not the top-level coordinator orientation

### Requirement: hera_new_orchestrator keeps the caller's prompt when none is given

When `hera_new_orchestrator` is called without a `prompt`, the new coordinator role's stored mission SHALL be the calling task's current prompt, so a later recycle seed's original-prompt section shows what the session was doing before it promoted itself. An explicit `prompt` SHALL still be stored as given.

#### Scenario: Omitted prompt falls back to the task prompt

- **WHEN** a task whose prompt is "build the importer" calls hera_new_orchestrator without `prompt`
- **THEN** the new coordinator role's prompt is "build the importer"

#### Scenario: Explicit prompt wins

- **WHEN** hera_new_orchestrator is called with `prompt` set
- **THEN** the new coordinator role's prompt is that value, not the task prompt
