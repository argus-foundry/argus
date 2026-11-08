## MODIFIED Requirements

### Requirement: Project rows and task creation
Project headers SHALL show a letter avatar and project name, an always-visible New task (+) button and a hover/focus Configuration gear. Project headers SHALL omit task counts. Empty projects SHALL have no disclosure control, expanded state or empty child area, including when previously expanded projects become empty after refresh or archive filters. Non-empty groups SHALL retain existing pointer, keyboard and in-memory disclosure behavior. The Configuration gear SHALL open the project editor directly; no single-item project menu or duplicate New task action SHALL remain.

#### Scenario: Start a task from a project
- **WHEN** New task is chosen from a project's plus button
- **THEN** the main pane SHALL open a task-creation form with that project preselected, a native prompt field and optional name
- **AND** POST /api/tasks SHALL use existing JSON fields and omit backend and account overrides unless explicitly selected, omit branch overrides, and omit blank model and inherited sandbox overrides
- **AND** successful creation SHALL select the new task and open its agent terminal
- **AND** prompt paste SHALL preserve multiline text without submitting; ordinary Enter SHALL submit and Shift+Enter SHALL insert a newline, retaining composition guards
- **AND** errors SHALL show inline while keeping the prompt and name for correction or retry
- **AND** Escape or Cancel SHALL return to the previous workspace view without sending agent input
- **AND** canceling an in-flight request SHALL ignore late results and explain that creation may have completed
- **AND** creation drafts SHALL stay in tab memory and clear on disconnect; pre-creation attachments remain outside scope, with task attachments available after creation


### Requirement: Task creation model and sandbox
The optional workspace SHALL expose model and sandbox choices only during task creation, using existing backend metadata and POST /api/tasks fields.

#### Scenario: Choose creation model and sandbox
- **WHEN** the task creation form opens
- **THEN** Model SHALL show only Daemon default and remain disabled while Backend is inherited, regardless of project or global backend metadata, because routing can select another backend
- **AND** choosing an explicit backend with a nonempty models list SHALL offer only Backend default and listed identifiers, rejecting unlisted identifiers
- **AND** choosing an explicit backend without a models list SHALL allow trimmed free text with a Backend default placeholder
- **AND** changing Backend SHALL reset Model to default and refresh its control and choices
- **AND** Sandbox SHALL offer Project default, Enabled and Disabled with Project default selected initially
- **AND** POST /api/tasks SHALL include every explicitly selected nonempty model, including one equal to the backend default, omit blank model, and include sandbox_override only as enabled or disabled when explicitly selected
- **AND** each project draft SHALL preserve both choices across cancellation and failed creation, clearing on successful creation or disconnect
- **AND** both controls SHALL have accessible names, visible keyboard focus when enabled and be disabled while creation is pending
