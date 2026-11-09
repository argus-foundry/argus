## MODIFIED Requirements

### Requirement: The resident tab is first and named after the agent

When the resident is enabled the TUI header SHALL show the resident tab first, labelled with `resident.name`, for example `Solo | Tasks | Projects | Settings`. `1`, `2`, `3` SHALL continue to select Tasks, Projects and Settings; a new rebindable global action `global.tab_resident` (default `0`) SHALL select the resident tab. The tab SHALL be the Hera view with exactly one coordinator, the resident: a rail of the resident's workers, the resident's conversation pane bound to the resident session with input going straight to the PTY, and the resident's inbox. Selecting the resident row SHALL show the resident pane; selecting a worker row SHALL show that worker in the existing worker pane. The inbox SHALL be the existing read-only inbox viewer opened with `i`, and the rail SHALL show the unread count and the existing needs-input and ready-to-close markers per worker so a stuck agent is visible. The rail SHALL be navigation-only: every creation or structure-changing action (new coordinator, spawn, move, join, detach, hide, pin, rename, role status, kanban step, nuke, bounce) SHALL be inert in this tab and absent from its `?` overlay and ctrl+k palette. The resident tab SHALL NOT be restored by last-tab persistence; a launch whose persisted tab would be the resident SHALL land on Tasks. When the resident is disabled the tab and its marker SHALL NOT appear.

#### Scenario: Tab order and digits

- **WHEN** the resident is enabled and named "Solo"
- **THEN** the header reads `Solo | Tasks | Projects | Settings` and `1/2/3` still select Tasks/Projects/Settings

#### Scenario: Disabled hides everything

- **WHEN** the resident is disabled
- **THEN** no resident tab, marker or rail box is shown

#### Scenario: Not restored on launch

- **WHEN** the TUI exits on the resident tab and is relaunched
- **THEN** it lands on the Tasks tab

#### Scenario: Rail lists the team

- **WHEN** the resident has three workers, one flagged needs-input
- **THEN** the rail shows the three workers, the flagged one with its needs-input marker, beside the resident pane

#### Scenario: Worker selection

- **WHEN** the user selects a worker row
- **THEN** that worker's pane or details are shown and the resident session is unaffected

#### Scenario: Inbox viewer

- **WHEN** the user presses `i` on the resident tab
- **THEN** the read-only inbox viewer opens and does not acknowledge any message

#### Scenario: No creation actions

- **WHEN** the user presses any spawn, new-coordinator, move, join, detach, hide, pin, rename, status, kanban, nuke or bounce key on the resident rail
- **THEN** nothing happens and none of them is listed in the help overlay or palette for this tab

#### Scenario: Resident pane lifecycle unchanged

- **WHEN** a live session is newly bound to the resident pane
- **THEN** the existing first-attach resync applies and the Hera size-drift kick is not armed
