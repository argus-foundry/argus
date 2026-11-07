## MODIFIED Requirements

### Requirement: Coordinator triage in the unified project rail

Within each project the workspace SHALL show root trees classified active directly below the project without a triage header or divider, using In progress wherever that classification needs a display label. Missing state SHALL default to active. Blocked trees SHALL remain visible and expanded with a secondary Blocked label. Ordinary tasks SHALL precede deferred footers. Nonempty Backlog and Done groups SHALL appear as project footers in that order, collapsed by default, counting root trees rather than descendants. Footer disclosure SHALL persist through guarded browser preferences and remain usable without storage. Nested trees SHALL remain below their parent and pins SHALL sort first within each triage class. Task row geometry, status icons, activity chips, 12px nesting and 220px rail support SHALL remain intact, including reduced motion. Global Active/Archived labels SHALL retain lifetime semantics.

#### Scenario: Grouping and pinned ordering
- **WHEN** a project contains ordinary tasks and root trees in multiple triage states
- **THEN** In progress trees SHALL have no group header and Blocked trees SHALL remain expanded with their classification visible
- **AND** ordinary tasks SHALL remain reachable before nonempty Backlog and Done footers, without a separate team mode
- **AND** pinned roots SHALL remain first within their triage class, including projects containing only Done

#### Scenario: Remembered disclosure and navigation
- **WHEN** a user changes a Backlog or Done disclosure and reloads the workspace
- **THEN** that project and state SHALL retain the preference through guarded storage
- **AND** unavailable or malformed storage SHALL retain usable default disclosures
- **AND** search SHALL temporarily reveal matches without saving that temporary opening, while explicit navigation SHALL reveal the selected tree without changing indentation

#### Scenario: Deferred attention and filtered descendants
- **WHEN** a collapsed footer contains tasks needing input or attention
- **THEN** its visible summary and accessible name SHALL expose those counts and project attention SHALL remain visible
- **WHEN** search finds a descendant whose parent is filtered out
- **THEN** its accessible task name SHALL retain the root tree's triage context

### Requirement: Coordinator menu triage actions

The shared task menu SHALL offer explicit task Pin/Unpin for live lead agents and the four kanban destinations for root trees. Active SHALL display as In progress. Nested trees SHALL NOT offer kanban changes. Ordinary task pins SHALL retain their behavior. Mutations SHALL use existing authenticated REST targets and values, optimistically update the rail, log success and failure, and restore authoritative state with error feedback on failure. Menu keyboard access and focus restoration SHALL remain available when a move hides its source row. Workspace product copy, tooltips and accessible labels SHALL use task, team tasks, agent and unavailable rather than orchestration implementation terms; browser service worker terminology SHALL remain unchanged.

#### Scenario: Coordinator triage mutation
- **WHEN** a root task menu changes triage or pin
- **THEN** the client SHALL POST to the existing orchestrator kanban, pin or unpin endpoint with unchanged values
- **AND** the tree SHALL move or reorder without changing task selection, with Move to In progress as the active destination label

#### Scenario: Failed mutation and focus
- **WHEN** a triage or pin mutation fails
- **THEN** the optimistic change SHALL roll back and existing error feedback SHALL appear
- **AND** focus SHALL remain on the equivalent row action or destination disclosure, returning to the source action if rollback removes the footer
