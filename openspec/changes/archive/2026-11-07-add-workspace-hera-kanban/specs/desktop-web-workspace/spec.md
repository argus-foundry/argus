## Non-Goals amendment

The native `add-web-hera-mutations` follow-up remains separate. Spawn, new coordinator, move, join, detach, inbox and merge-safety Cleanup retain their standing TUI-only disposition. The workspace rail supports coordinator kanban and orchestrator pin mutations through existing endpoints. One unified project rail replaces separate Tasks/Hera modes, the Hera visibility filter and a separate Freelance section. Task status remains editable in Settings rather than through quick workflow transitions. Archive/restore remains in the rail. Session selection remains beside Stop/Resume. Web Push is the desktop-alert channel; in-page attention preferences remain separate.

## ADDED Requirements

### Requirement: Coordinator triage in the unified project rail

Within each project the workspace SHALL group root coordinator trees in Active, Backlog, Blocked and Done order, defaulting missing kanban state to Active and hiding empty groups. Done SHALL start collapsed. Group disclosure state SHALL persist through guarded workspace preferences. Plain non-Hera tasks SHALL retain their project placement. Nested coordinator trees SHALL remain below their parent. Pinned orchestrators SHALL sort first within their group. Task row geometry, indentation and status icons SHALL remain unchanged.

#### Scenario: Grouping and pinned ordering
- **WHEN** a project contains ordinary tasks and coordinator trees in multiple kanban states
- **THEN** populated groups SHALL appear in Active, Backlog, Blocked and Done order with pinned coordinators first in each group
- **AND** ordinary tasks SHALL remain reachable in their project without a separate Freelance section or Projects tab

#### Scenario: Remembered disclosure and navigation
- **WHEN** a user expands Done or collapses another group and reloads the workspace
- **THEN** the group SHALL retain that preference
- **AND** search and explicit task navigation SHALL reveal matching trees without changing row indentation

### Requirement: Coordinator menu triage actions

The shared task menu SHALL offer explicit orchestrator Pin/Unpin for live coordinators and the four kanban destinations for root coordinators. Nested coordinators SHALL NOT offer kanban changes. Ordinary task pin actions SHALL retain their existing behavior. Mutations SHALL use existing authenticated REST, optimistically update the rail, log success and failure, and restore authoritative state with existing error feedback on failure. Menu keyboard access and focus restoration SHALL remain available, including when a move hides its source row.

#### Scenario: Coordinator triage mutation
- **WHEN** a root coordinator menu changes kanban state or pin
- **THEN** the client SHALL POST to that orchestrator's existing kanban, pin or unpin endpoint
- **AND** the tree SHALL move or reorder without changing its task selection

#### Scenario: Failed mutation and focus
- **WHEN** a coordinator mutation fails
- **THEN** the optimistic change SHALL roll back and existing error feedback SHALL appear
- **AND** focus SHALL remain reachable on the equivalent row action or destination disclosure

## MODIFIED Requirements

### Requirement: Task rail quick actions

The workspace SHALL expose task settings, explicit Pin/Unpin and copies through task row menus, reachable by context click and keyboard. Workflow status SHALL be edited only through the task settings select. Ordinary task Pin SHALL consume supplied boolean task state without another selection raw read, lazily resolve unknown menu state, preserve the fresh raw record when mutating and reject archived tasks. Coordinator Pin/Unpin SHALL instead use the orchestrator's pin state and targeted authenticated endpoints without full task-record writes. All live Hera-bound tasks, including freelance bindings, SHALL remain available without a separate visibility filter.

#### Scenario: Captured row action

- **WHEN** a row action targets a task other than the current main task
- **THEN** only the captured row's task is changed
- **AND** failure leaves the main task and draft available

#### Scenario: Existing task-menu keyboard entry

- **WHEN** ArrowDown opens a task's actions menu
- **THEN** Task settings retains initial focus before the additional quick actions
