## ADDED Requirements

### Requirement: Settings shortcut transitions
Global shortcuts opening workspace surfaces SHALL leave full-window Settings before opening their target. Settings departure SHALL refuse while a settings save is in flight, matching Back. Creation drafts and compact drawer dismissal SHALL be preserved. Actions that do not change the workspace surface MAY remain in Settings.

#### Scenario: Open another surface from Settings
- **WHEN** Settings Back has focus and a shortcut opens New Task, task settings, a task action dialog, a task detail tab, split view or the task filter
- **THEN** Settings SHALL close and release its inert boundary before the target becomes visible and interactive

#### Scenario: Shortcut during save
- **WHEN** a Settings save is pending and a shortcut would leave Settings
- **THEN** Settings SHALL remain active and no target surface SHALL open
- **AND** the save SHALL finish normally before a later shortcut may leave

#### Scenario: Unsaved edits follow Back behavior
- **WHEN** a user edits Settings without starting a save and invokes a surface shortcut
- **THEN** departure SHALL follow the existing Back behavior without silently saving edits
- **AND** unsaved-edit leave confirmation remains deferred to `workspace-settings-unsaved-edit-confirmation` in the canonical follow-up register above

## MODIFIED Requirements

### Requirement: Settings home and project management
The footer SHALL replace its original UI link with an accessible settings gear beside connection status. Settings SHALL retain the workspace's task draft and terminal attachment, support General/repository section navigation and return through Escape, Back, task selection or surface shortcuts. Departure SHALL refuse while a settings save is in flight, matching Back. Unsaved edits SHALL be discarded without saving on departure. Leave confirmation remains deferred to `workspace-settings-unsaved-edit-confirmation` in the canonical follow-up register above. Closing or disconnecting SHALL cancel stale settings reads and guard late results.

#### Scenario: Edit General settings
- **WHEN** a user opens General
- **THEN** the client SHALL show existing KB, default backend/share project and HTTP API controls, with the API port read-only
- **AND** explicit Save SHALL send only the owned partial settings fields, preserve form data on failure and show restart-required notices without restarting the daemon

#### Scenario: Manage projects
- **WHEN** a user explicitly adds, edits or removes project configuration
- **THEN** the client SHALL use existing project CRUD endpoints, validate required fields and check duplicate names before create
- **AND** editing SHALL preserve all returned sandbox overrides and clearly disclose the existing profile-reset API limitation
- **AND** removing SHALL require confirmation that existing tasks and worktrees remain
- **AND** successful writes SHALL refresh project choices and the task rail while errors retain editable input
