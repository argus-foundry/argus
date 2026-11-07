## MODIFIED Requirements

### Requirement: Project rows and task creation
Project headers SHALL show a letter avatar and project name, an always-visible New task (+) button and a hover/focus Configuration gear. Project headers SHALL omit task counts. Empty projects SHALL have no disclosure control, expanded state or empty child area, including when previously expanded projects become empty after refresh or archive filters. Non-empty groups SHALL retain existing pointer, keyboard and in-memory disclosure behavior. The Configuration gear SHALL open the project editor directly; no single-item project menu or duplicate New task action SHALL remain.

#### Scenario: Start a task from a project
- **WHEN** New task is chosen from a project's plus button
- **THEN** the main pane SHALL open a task-creation form with that project preselected, a native prompt field and optional name
- **AND** POST /api/tasks SHALL use existing JSON fields and omit backend and account overrides unless explicitly selected, omit branch overrides, and omit model and sandbox overrides when their defaults apply
- **AND** successful creation SHALL select the new task and open its agent terminal
- **AND** prompt paste SHALL preserve multiline text without submitting; ordinary Enter SHALL submit and Shift+Enter SHALL insert a newline, retaining composition guards
- **AND** errors SHALL show inline while keeping the prompt and name for correction or retry
- **AND** Escape or Cancel SHALL return to the previous workspace view without sending agent input
- **AND** canceling an in-flight request SHALL ignore late results and explain that creation may have completed
- **AND** creation drafts SHALL stay in tab memory and clear on disconnect; pre-creation attachments remain outside scope, with task attachments available after creation

## ADDED Requirements

### Requirement: Task creation model and sandbox
The optional workspace SHALL expose model and sandbox choices only during task creation, using existing backend metadata and POST /api/tasks fields.

#### Scenario: Choose creation model and sandbox
- **WHEN** the task creation form opens
- **THEN** Model SHALL default to the resolved backend default and suggest that backend's models, accepting custom identifiers
- **AND** changing the backend SHALL refresh model suggestions and reset Model to default
- **AND** Sandbox SHALL offer Project default, Enabled and Disabled with Project default selected initially
- **AND** POST /api/tasks SHALL include model only for a nonempty value differing from the backend default, and sandbox_override only as enabled or disabled when explicitly selected
- **AND** each project draft SHALL preserve both choices across cancellation and failed creation, clearing on successful creation or disconnect
- **AND** both controls SHALL have accessible names, visible keyboard focus and be disabled while creation is pending
