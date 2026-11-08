## ADDED Requirements

### Requirement: Task context categories in the composer
Theia SHALL prefer GET /api/tasks/{id}/context over bridge/meta measurements when the route is supported. It SHALL render measured category token counts for messages, memory, tools, agents, skills, system and compaction. Null categories SHALL be absent, never coerced to zero. Historical role totals SHALL NOT supply categories. A supported response with context=null SHALL show unavailable context, without substituting older bridge/meta data.

#### Scenario: Measured counts and age
- **WHEN** the task context response contains category measurements
- **THEN** the popover shows each measured count, including measured zero, and omits null categories
- **AND** measured_at appears as measurement age and a timestamp
- **AND** live=false is labeled last recorded while live=true remains measured regardless of age

#### Scenario: Old daemon
- **WHEN** the context route returns 404 or HTML through a successful GET fallback
- **THEN** the composer quietly retains its existing bridge/meta context behavior
- **AND** other context read failures clear optional measurements rather than substituting old data

#### Scenario: Context refresh and selection fencing
- **WHEN** the selected task emits task.context_changed on the daemon event stream
- **THEN** Theia refetches its context, also retaining popover-open and foreground 15 second polling refreshes
- **AND** task/session switches and disconnect cancel or ignore superseded reads and clear prior measurements
- **AND** events buffered during a task snapshot also invalidate selected context

## MODIFIED Requirements

### Requirement: Measured context popover
The context control SHALL open an accessible dismissible popover, show only sourced existing REST values with their correct scope, and indicate unavailable context without fabricating a fill or percent. The popover SHALL retain Claude plan, email, organization and usage limit windows when available, while Account and historical role token totals SHALL remain in task Info and SHALL NOT appear in the composer popover. The composer SHALL NOT fetch the Hera roster for context readings. Unsupported backend fields SHALL be omitted. Switching tasks and disconnecting SHALL cancel reads and reject late results. Bridge and Hera metadata context readings SHALL be fallback only when the task context route is unsupported.

#### Scenario: Backend measurements are partial
- **WHEN** REST exposes account usage but no context-window measurement
- **THEN** usage is shown only for the task's matching account and backend
- **AND** the context ring remains unknown rather than displaying quota as context
#### Scenario: Legacy bridge and recorded stamp availability
- **WHEN** the daemon lacks the task context route and the selected task has a live bridge context with a measured percent or measured tokens/window
- **THEN** the ring displays that context percentage and the popover shows available token/window values
- **WHEN** that older daemon has only Hera metadata
- **THEN** the popover labels the values as recorded, shows record time and pairs percent with tokens only for equal timestamps
- **AND** a configured recycle budget and historical role token totals never supply an inferred window

#### Scenario: Shared account across backends
- **WHEN** a Codex task uses an account that also supports Claude
- **THEN** the composer omits the Account row and task Info retains the raw account identity
- **AND** Claude plan, email and account quotas SHALL NOT be attributed to Codex
- **AND** unsupported Codex fields remain absent as documented in web-workspace/docs/context-data.md

#### Scenario: Historical token totals
- **WHEN** a task has exactly one live bound role with measured REST token totals
- **THEN** available counts remain in task Info under its cost display rules and SHALL NOT appear in the composer popover
- **AND** ambiguous multiple role bindings are not summed or arbitrarily selected

#### Scenario: Composer keeps distinct Claude account details
- **WHEN** the matching Claude account exposes plan, email, organization and valid usage limit windows
- **THEN** those values remain in the composer with measured context and reset times
- **AND** the composer omits Account and historical role token totals without reading the Hera roster
